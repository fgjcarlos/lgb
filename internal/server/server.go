// Package server provides the HTTP server stub for the LGB gateway.
//
// The server mounts /health, /metrics (stub), and /readyz. Graceful shutdown
// is handled via httpx.Shutdown with a configurable deadline.
// Signal handling lives in cmd/lgb/cmd/server.go — this package is test-friendly.
//
// Requirements: MVP-FND-1.3, MVP-FND-1.8, MVP-FND-1.9. Design: §11, §4.3–4.5.
package server

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/fgjcarlos/lgb/internal/config"
	"github.com/fgjcarlos/lgb/internal/doctor"
	"github.com/fgjcarlos/lgb/internal/health"
	"github.com/fgjcarlos/lgb/internal/httpx"
)

// Server is the LGB HTTP server stub.
type Server struct {
	cfg    *config.Config
	log    *slog.Logger
	checks []doctor.Check

	mu   sync.Mutex
	addr string // resolved bound address (host:port)
}

// New creates a new Server from the given config, logger, and optional checks.
// checks is currently unused in Phase 0 but wired for future readyz semantics.
func New(cfg *config.Config, log *slog.Logger, checks []doctor.Check) *Server {
	return &Server{
		cfg:    cfg,
		log:    log,
		checks: checks,
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

	shutdownTimeout := 10 * time.Second
	if s.cfg.Server.ShutdownTimeout != "" {
		if d, err := time.ParseDuration(s.cfg.Server.ShutdownTimeout); err == nil {
			shutdownTimeout = d
		}
	}

	srv := &http.Server{Handler: mux}

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
	if err := httpx.Shutdown(ctx, srv, shutdownTimeout); err != nil {
		return fmt.Errorf("server: shutdown: %w", err)
	}
	s.log.Info("shutdown complete")
	return nil
}
