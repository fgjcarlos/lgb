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
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/fgjcarlos/lgb/internal/auth"
	"github.com/fgjcarlos/lgb/internal/backup"
	"github.com/fgjcarlos/lgb/internal/config"
	"github.com/fgjcarlos/lgb/internal/doctor"
	"github.com/fgjcarlos/lgb/internal/health"
	"github.com/fgjcarlos/lgb/internal/historian"
	"github.com/fgjcarlos/lgb/internal/httpx"
	"github.com/fgjcarlos/lgb/internal/plc"
)

// PLCManager is the interface that the PLC manager must satisfy for server
// lifecycle integration. (design §10)
type PLCManager interface {
	Start(ctx context.Context) error
	Stop() error
	Reload(ctx context.Context, cfg *config.Config) error
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
	userStore *auth.UserStore
	auditLog  *auth.AuditLogger
	histStore *historian.Store
	bkpMgr    *backup.Manager

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
	UserStore *auth.UserStore
	AuditLog  *auth.AuditLogger
	HistStore *historian.Store
	BkpMgr    *backup.Manager

	// Checks is the list of doctor.Check instances run by GET /api/doctor.
	// When nil, the server starts with no checks registered.
	Checks []doctor.Check
}

// New creates a new Server. All optional dependencies in opts may be nil;
// Run handles the nil cases without panicking.
//
// Checks from both the positional parameter and opts.Checks are merged; opts.Checks
// is appended after the positional slice so callers can inject test-only checks
// via Opts without altering production call sites.
func New(cfg *config.Config, log *slog.Logger, checks []doctor.Check, opts Opts) *Server {
	allChecks := checks
	if len(opts.Checks) > 0 {
		allChecks = append(allChecks, opts.Checks...)
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

// Run binds the configured address, mounts routes, serves until ctx is
// cancelled, then calls httpx.Shutdown. Returns nil on clean shutdown.
//
// Per design §4.5 and §20.1, Run does NOT handle OS signals — the caller
// (cmd/lgb/cmd/server.go) wires signal.NotifyContext before calling Run.
func (s *Server) Run(ctx context.Context) error {
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

	shutdownTimeout := 10 * time.Second
	if s.cfg.Server.ShutdownTimeout != "" {
		if d, err := time.ParseDuration(s.cfg.Server.ShutdownTimeout); err == nil {
			shutdownTimeout = d
		}
	}

	srv := &http.Server{Handler: mux}

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
	serveErr := make(chan error, 1)
	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			serveErr <- err
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
