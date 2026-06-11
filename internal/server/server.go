// Package server provides the HTTP server stub for the LGB gateway.
//
// The server mounts /health, /metrics (stub), and /readyz. Graceful shutdown
// is handled via httpx.Shutdown with a configurable deadline.
// Signal handling lives in cmd/lgb/cmd/server.go — this package is test-friendly.
//
// Requirements: MVP-FND-1.3, MVP-FND-1.8, MVP-FND-1.9. Design: §11, §4.3–4.5, §10.
package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/fgjcarlos/lgb/internal/aclstore"
	"github.com/fgjcarlos/lgb/internal/auth"
	"github.com/fgjcarlos/lgb/internal/backup"
	"github.com/fgjcarlos/lgb/internal/config"
	"github.com/fgjcarlos/lgb/internal/doctor"
	"github.com/fgjcarlos/lgb/internal/health"
	"github.com/fgjcarlos/lgb/internal/historian"
	"github.com/fgjcarlos/lgb/internal/httpx"
	"github.com/fgjcarlos/lgb/internal/plc"
	"github.com/fgjcarlos/lgb/internal/plcstore"
	"github.com/fgjcarlos/lgb/internal/writeguard"
)

// PLCManager is the interface that the PLC manager must satisfy for server
// lifecycle integration. (design §10)
type PLCManager interface {
	Start(ctx context.Context) error
	Stop() error
	Reload(ctx context.Context, cfg *config.Config) error
	// WriteTag writes val to the named tag on the named PLC.
	// Returns plc.ErrPLCNotFound if the PLC is not registered.
	// Added in PR2 (Design §2).
	WriteTag(plcName, tag string, val any) error
}

// tagUpdateHook is implemented by PLC managers that can fan out scanned tag
// updates to additional consumers such as the realtime WebSocket API.
type tagUpdateHook interface {
	AddTagCallback(func(plc.TagUpdate))
}

// SparkplugNode is the interface the Sparkplug edge node must satisfy
// for server lifecycle integration. Same pattern as PLCManager. (design §9)
type SparkplugNode interface {
	Start(ctx context.Context) error
	Stop() error
}

// HistorianWriter is the interface the historian async writer must satisfy
// for server lifecycle integration.
type HistorianWriter interface {
	Start(ctx context.Context)
	Stop(ctx context.Context) error
}

// BackupScheduler is the interface the backup scheduler must satisfy
// for server lifecycle integration.
type BackupScheduler interface {
	Start(ctx context.Context)
	Stop() error
}

// OPCUAServer is the interface the OPC UA server must satisfy
// for server lifecycle integration.
type OPCUAServer interface {
	Start(ctx context.Context) error
	Stop() error
}

// Server is the LGB HTTP server stub.
type Server struct {
	cfg        *config.Config
	log        *slog.Logger
	checks     []doctor.Check
	plcMgr     PLCManager         // nil when no PLCs are configured
	spNode     SparkplugNode      // nil when MQTT/Sparkplug is not configured
	histW      HistorianWriter    // nil when historian is not configured
	bkpSch     BackupScheduler    // nil when backup is not configured
	opcuaSrv   OPCUAServer        // nil when OPC UA is not configured
	authTokens *auth.TokenService // nil disables API auth, used by tests only
	tagHub     *tagHub            // realtime API fanout for PLC tag updates

	// Domain store dependencies (all nil-safe).
	userStore  *auth.UserStore
	auditLog   *auth.AuditLogger
	histStore  *historian.Store
	bkpMgr     *backup.Manager
	plcStore   *plcstore.Store
	aclStore   *aclstore.Store   // nil until PR4 wires the admin CRUD API
	writeGuard *writeguard.Guard // nil-safe; operative as soon as set (PR2+)

	// tlsConfig is the test seam injected via Opts.TLSConfig. When non-nil and
	// cfg.Server.TLSEnabled is true, Run uses tls.NewListener instead of ServeTLS.
	tlsConfig *tls.Config

	bkpStatus backupStatus // mutex-guarded backup status cell

	mu   sync.Mutex
	addr string // resolved bound address (host:port)
}

// Opts groups optional server dependencies. All fields may be nil.
type Opts struct {
	PLCMgr     PLCManager
	SpNode     SparkplugNode
	HistW      HistorianWriter
	BkpSch     BackupScheduler
	OPCUASrv   OPCUAServer
	AuthTokens *auth.TokenService

	// Domain store dependencies (all optional).
	UserStore  *auth.UserStore
	AuditLog   *auth.AuditLogger
	HistStore  *historian.Store
	BkpMgr     *backup.Manager
	PLCStore   *plcstore.Store
	ACLStore   *aclstore.Store   // nil until PR4 wires the admin CRUD API
	WriteGuard *writeguard.Guard // nil-safe; operative as soon as set

	// Checks is the list of doctor.Check instances run by GET /api/doctor.
	// When nil, the server starts with no checks registered.
	Checks []doctor.Check

	// TLSConfig is the test seam for TLS. When non-nil and cfg.Server.TLSEnabled
	// is true, Run wraps the listener with tls.NewListener(ln, TLSConfig) instead
	// of calling srv.ServeTLS with cert/key files. This allows tests to inject an
	// in-memory self-signed cert without disk access (R72 seam).
	//
	// In production (TLSConfig == nil, TLSEnabled == true): Run calls
	// srv.ServeTLS(ln, cfg.Server.TLSCertFile, cfg.Server.TLSKeyFile).
	TLSConfig *tls.Config
}

// New creates a new Server. All optional dependencies in opts may be nil;
// Run handles the nil cases without panicking.
//
// Checks from both the positional parameter and opts.Checks are merged; opts.Checks
// is appended after the positional slice so callers can inject test-only checks
// via Opts without altering production call sites.
//
// Guard ownership: if opts.WriteGuard is nil but both opts.PLCStore and
// opts.ACLStore are non-nil, New builds a writeguard.Guard automatically.
// This activates the HTTP write endpoint (registerWriteRoutes) — safe because an
// empty ACL store denies all HTTP writes by default (deny-by-default). An
// injected opts.WriteGuard (for tests) takes precedence over auto-construction.
func New(cfg *config.Config, log *slog.Logger, checks []doctor.Check, opts Opts) *Server {
	allChecks := checks
	if len(opts.Checks) > 0 {
		allChecks = append(allChecks, opts.Checks...)
	}

	// Auto-build the write guard when the stores are available and no guard was
	// injected. Injected guard wins (used by tests that want full control).
	guard := opts.WriteGuard
	if guard == nil && opts.PLCStore != nil && opts.ACLStore != nil {
		guard = writeguard.NewGuard(&plcstoreTagReader{store: opts.PLCStore}, opts.ACLStore)
	}

	return &Server{
		cfg:        cfg,
		log:        log,
		checks:     allChecks,
		plcMgr:     opts.PLCMgr,
		spNode:     opts.SpNode,
		histW:      opts.HistW,
		bkpSch:     opts.BkpSch,
		opcuaSrv:   opts.OPCUASrv,
		authTokens: opts.AuthTokens,
		tagHub:     newTagHub(),
		userStore:  opts.UserStore,
		auditLog:   opts.AuditLog,
		histStore:  opts.HistStore,
		bkpMgr:     opts.BkpMgr,
		plcStore:   opts.PLCStore,
		aclStore:   opts.ACLStore,
		writeGuard: guard,
		tlsConfig:  opts.TLSConfig,
		bkpStatus:  backupStatus{status: "idle"},
	}
}

// Addr returns the bound address (host:port) after Run has started, or empty
// string if the server has not yet bound. Tests poll this to discover the port.
func (s *Server) Addr() string {
	// Poll briefly to allow the goroutine to bind.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		a := s.addr
		s.mu.Unlock()
		if a != "" {
			return a
		}
		time.Sleep(5 * time.Millisecond)
	}
	return ""
}

// Default HTTP server timeout values (R75-1). These apply when the
// corresponding config field is empty string (e.g. MinimalConfig in tests).
const (
	defaultReadHeaderTimeout = 5 * time.Second
	defaultReadTimeout       = 30 * time.Second
	// defaultWriteTimeout is intentionally longer than a typical HTTP response
	// because WebSocket connections are hijacked before WriteTimeout fires —
	// once hijacked, http.Server no longer enforces WriteTimeout on that
	// connection (Go stdlib guarantee, R75-5). So this only bounds non-WS
	// responses which should always complete well within 60 s.
	defaultWriteTimeout = 60 * time.Second
	defaultIdleTimeout  = 120 * time.Second
)

// buildHTTPServer constructs the *http.Server with all four timeout fields set
// from config (R75-1). When a field is empty string, the compiled-in default
// constant is used — this is LOAD-BEARING: MinimalConfig leaves these fields
// empty, so the fallback prevents zero timeouts in all existing server tests.
//
// The returned server's Handler is securityHeadersMiddleware(mux) so that
// every response carries the required security headers (R75-2).
func (s *Server) buildHTTPServer(mux *http.ServeMux) *http.Server {
	parseDur := func(val string, def time.Duration) time.Duration {
		if val == "" {
			return def
		}
		if d, err := time.ParseDuration(val); err == nil && d > 0 {
			return d
		}
		return def
	}

	return &http.Server{
		Handler:           securityHeadersMiddleware(mux),
		ReadHeaderTimeout: parseDur(s.cfg.Server.ReadHeaderTimeout, defaultReadHeaderTimeout),
		ReadTimeout:       parseDur(s.cfg.Server.ReadTimeout, defaultReadTimeout),
		// WriteTimeout does NOT close hijacked WebSocket connections — once the
		// HTTP layer calls conn.Hijack(), http.Server stops enforcing timeouts on
		// that connection entirely. (R75-5 guarantee, Go stdlib net/http design.)
		WriteTimeout: parseDur(s.cfg.Server.WriteTimeout, defaultWriteTimeout),
		IdleTimeout:  parseDur(s.cfg.Server.IdleTimeout, defaultIdleTimeout),
	}
}

// Run binds the configured address, mounts routes, serves until ctx is
// cancelled, then calls httpx.Shutdown. Returns nil on clean shutdown.
//
// Per design §4.5 and §20.1, Run does NOT handle OS signals — the caller
// (cmd/lgb/cmd/server.go) wires signal.NotifyContext before calling Run.
func (s *Server) Run(ctx context.Context) error {
	// Fail fast: TLS is enabled but cert/key paths are missing (R72 guard).
	// This check is independent of Validate() so that callers that bypass
	// config validation cannot reach ServeTLS with empty paths and receive a
	// cryptic stdlib error.
	if s.cfg.Server.TLSEnabled && s.tlsConfig == nil &&
		(s.cfg.Server.TLSCertFile == "" || s.cfg.Server.TLSKeyFile == "") {
		return fmt.Errorf("server: TLS is enabled but TLSCertFile or TLSKeyFile is not configured")
	}

	ln, err := net.Listen("tcp", s.cfg.Server.HTTPAddr)
	if err != nil {
		return fmt.Errorf("server: listen %q: %w", s.cfg.Server.HTTPAddr, err)
	}

	// Store the actual bound address for Addr().
	s.mu.Lock()
	s.addr = ln.Addr().String()
	s.mu.Unlock()

	mux := httpx.NewMux()

	// /health — always 200 {"status":"ok"}
	mux.Handle("/health", health.Handler())

	// /metrics — stub 200 with empty Prometheus text exposition
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "# empty\n")
	})

	// /readyz — returns 200 once the server is bound (we're already bound here).
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"status":"ok"}`)
	})

	s.registerAPIRoutes(mux)
	s.mountSPA(mux)

	shutdownTimeout := 10 * time.Second
	if s.cfg.Server.ShutdownTimeout != "" {
		if d, err := time.ParseDuration(s.cfg.Server.ShutdownTimeout); err == nil {
			shutdownTimeout = d
		}
	}

	// Warn only on the non-TLS path (R72): when TLSEnabled is true the warning
	// is suppressed — the server is about to serve TLS (PR2 WARN gating).
	if !s.cfg.Server.TLSEnabled {
		s.log.Warn("server running WITHOUT TLS — plaintext HTTP", "addr", s.cfg.Server.HTTPAddr)
	}

	srv := s.buildHTTPServer(mux)

	// Start Sparkplug node FIRST (connects MQTT, registers Will). (design §9)
	if s.spNode != nil {
		if err := s.spNode.Start(ctx); err != nil {
			s.log.Warn("sparkplug node: Start returned error", slog.String("error", err.Error()))
		}
	}

	// Start historian writer SECOND (must be ready before PLC scans produce data).
	if s.histW != nil {
		s.histW.Start(ctx)
	}

	// Start PLC manager THIRD (scan loop emits TagUpdates to Sparkplug, Historian, and API).
	if s.plcMgr != nil {
		if hook, ok := s.plcMgr.(tagUpdateHook); ok {
			hook.AddTagCallback(s.PublishTagUpdate)
		}
		if err := s.plcMgr.Start(ctx); err != nil {
			s.log.Warn("plc manager: Start returned error", slog.String("error", err.Error()))
		}
	}

	// Start OPC UA server FOURTH (exposes tag values, needs PLC manager running).
	if s.opcuaSrv != nil {
		go func() {
			if err := s.opcuaSrv.Start(ctx); err != nil && ctx.Err() == nil {
				s.log.Warn("opcua server: Start returned error", slog.String("error", err.Error()))
			}
		}()
	}

	// Start backup scheduler LAST (periodic backups of historian snapshots).
	if s.bkpSch != nil {
		s.bkpSch.Start(ctx)
	}

	s.log.Info("server listening", "addr", ln.Addr().String())

	// Serve in a goroutine; wait for ctx to cancel then gracefully shut down.
	//
	// TLS dispatch (R72):
	//   - TLSEnabled=true + Opts.TLSConfig (test seam): wrap ln with tls.NewListener
	//     and call srv.Serve — no disk access, no cert/key files needed.
	//   - TLSEnabled=true + no seam (production): call srv.ServeTLS with the
	//     cert/key files from config (Validate already confirmed they are non-empty).
	//   - TLSEnabled=false: plaintext srv.Serve (existing path).
	serveErr := make(chan error, 1)
	go func() {
		var serveErrInner error
		switch {
		case s.cfg.Server.TLSEnabled && s.tlsConfig != nil:
			// Test seam: in-memory TLS config, no disk I/O.
			tlsLn := tls.NewListener(ln, s.tlsConfig)
			serveErrInner = srv.Serve(tlsLn)
		case s.cfg.Server.TLSEnabled:
			// Production: cert/key from config (already validated non-empty).
			serveErrInner = srv.ServeTLS(ln, s.cfg.Server.TLSCertFile, s.cfg.Server.TLSKeyFile)
		default:
			serveErrInner = srv.Serve(ln)
		}
		if serveErrInner != nil && serveErrInner != http.ErrServerClosed {
			serveErr <- serveErrInner
		}
		close(serveErr)
	}()

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
	}

	s.log.Info("shutdown initiated")

	// Stop backup scheduler FIRST (no new backup runs).
	if s.bkpSch != nil {
		if err := s.bkpSch.Stop(); err != nil {
			s.log.Warn("backup scheduler: Stop returned error", slog.String("error", err.Error()))
		}
	}

	// Stop OPC UA server SECOND (stops serving tag values).
	if s.opcuaSrv != nil {
		if err := s.opcuaSrv.Stop(); err != nil {
			s.log.Warn("opcua server: Stop returned error", slog.String("error", err.Error()))
		}
	}

	// Stop PLC manager THIRD (stops tag reads).
	if s.plcMgr != nil {
		if err := s.plcMgr.Stop(); err != nil {
			s.log.Warn("plc manager: Stop returned error", slog.String("error", err.Error()))
		}
	}

	// Stop historian writer THIRD (flushes pending samples).
	if s.histW != nil {
		if err := s.histW.Stop(ctx); err != nil {
			s.log.Warn("historian writer: Stop returned error", slog.String("error", err.Error()))
		}
	}

	// Stop Sparkplug node LAST (publishes DDEATH, disconnects MQTT).
	if s.spNode != nil {
		if err := s.spNode.Stop(); err != nil {
			s.log.Warn("sparkplug node: Stop returned error", slog.String("error", err.Error()))
		}
	}

	if err := httpx.Shutdown(ctx, srv, shutdownTimeout); err != nil {
		return fmt.Errorf("server: shutdown: %w", err)
	}
	s.log.Info("shutdown complete")
	return nil
}
