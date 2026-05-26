// server.go — lgb server subcommand.
//
// Validates jwtSecret, bootstraps dataDir, probes plcsim reachability, and
// starts the HTTP server. Signal handling via signal.NotifyContext happens
// here — internal/server is kept signal-free for testability.
// Design §6.3, §13.1, §20.1.
// Requirements: MVP-FND-1.3, MVP-FND-1.9, MVP-FND-7.5, MVP-FND-9.3.
package cmd

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/fgjcarlos/lgb/internal/config"
	"github.com/fgjcarlos/lgb/internal/datadir"
	"github.com/fgjcarlos/lgb/internal/doctor"
	"github.com/fgjcarlos/lgb/internal/log"
	"github.com/fgjcarlos/lgb/internal/plc"
	"github.com/fgjcarlos/lgb/internal/server"
)

// NewServerCmd returns the server subcommand.
func NewServerCmd(d *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "server",
		Short: "Start the HTTP server stub",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Wire OS signal context here — not inside internal/server (design §4.5).
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return runServerTo(ctx, d, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
}

// runServerTo starts the server using the given context and writes log/error
// output to stdout/stderr. Called by RunE and tests.
//
// d.Config must be non-nil (set by PersistentPreRunE or tests).
func runServerTo(ctx context.Context, d *Deps, stdout, stderr io.Writer) error {
	if stderr == nil {
		stderr = os.Stderr
	}
	if stdout == nil {
		stdout = os.Stdout
	}

	cfg := d.Config
	if cfg == nil {
		return fmt.Errorf("server: config not loaded")
	}

	// Validate jwtSecret — must not be empty. (MVP-FND-1.3)
	if cfg.Auth.JwtSecret == "" {
		fmt.Fprintf(stderr, "error: auth.jwtSecret is required — set via config or LGB_AUTH_JWTSECRET env\n")
		return fmt.Errorf("server: auth.jwtSecret is required")
	}

	// Print the configured listen address to stdout for scripting / health probes.
	fmt.Fprintf(stdout, "lgb server starting on %s\n", cfg.Server.HTTPAddr)

	// Bootstrap the data directory. (MVP-FND-7.5)
	ensureFn := d.DataDirEnsureFn
	if ensureFn == nil {
		ensureFn = datadir.Ensure
	}
	resolvedPath, err := datadir.Resolve(cfg, d.DataDir)
	if err != nil {
		return fmt.Errorf("server: resolve datadir: %w", err)
	}
	resolvedPath, err = ensureFn(resolvedPath)
	if err != nil {
		return fmt.Errorf("server: ensure datadir: %w", err)
	}

	// Build logger if not already set (tests may pre-populate d.Logger).
	logger := d.Logger
	if logger == nil {
		logger, err = log.New(log.Options{
			Level:  cfg.Gateway.LogLevel,
			Format: cfg.Gateway.LogFormat,
			Out:    os.Stderr,
		})
		if err != nil {
			return fmt.Errorf("server: build logger: %w", err)
		}
	}

	logger.Info("datadir resolved", "component", "datadir", "path", resolvedPath)

	// Probe plcsim reachability (MVP-FND-9.3). This is informational only —
	// a failed probe does NOT prevent the server from starting.
	plcsimAddr := cfg.PLCSim.Addr
	if plcsimAddr == "" {
		plcsimAddr = "plcsim:44818" // fallback default
	}
	probePlCSim(logger, plcsimAddr)

	// Build doctor checks for the server.
	checks := doctor.Default(cfg).Checks()

	// Create the PLC Manager when PLCs are configured.
	// When no PLCs are configured, plcMgr is nil and server.New handles it safely.
	var plcMgr server.PLCManager
	if len(cfg.PLCs) > 0 {
		factory := d.PLCManagerFactory
		if factory == nil {
			factory = defaultPLCManagerFactory
		}
		plcMgr = factory(cfg)
		logger.Info("plc manager created", slog.String("component", "plc-manager"),
			slog.Int("plc_count", len(cfg.PLCs)))
	}

	// Start the server.
	srv := server.New(cfg, logger, checks, plcMgr)

	// Store server reference for tests.
	if d.serverRef != nil {
		*d.serverRef = srv
	}

	if err := srv.Run(ctx); err != nil {
		logger.Error("server error", slog.String("error", err.Error()))
		return err
	}
	return nil
}

// getServerForTest is a test-only helper that returns the running *server.Server
// so tests can call srv.Addr(). It relies on d.serverRef being non-nil.
// This method exists solely for tests; it is unexported.
func (d *Deps) getServerForTest() *server.Server {
	if d.serverRef == nil {
		return nil
	}
	return *d.serverRef
}

// defaultPLCManagerFactory is the production PLCManagerFactory.
// It wraps plc.NewManager to match the server.PLCManager interface.
func defaultPLCManagerFactory(cfg *config.Config) server.PLCManager {
	return plc.NewManager(cfg, slog.Default(), nil, nil)
}

// probePlCSim performs a TCP dial to addr with a 5-second timeout.
// It logs the outcome at INFO level with component="startup". The probe is
// informational only — failures do not return an error (MVP-FND-9.3).
func probePlCSim(logger *slog.Logger, addr string) {
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		logger.Info("plcsim unreachable", "component", "startup", "addr", addr, "error", err.Error())
		return
	}
	conn.Close()
	logger.Info("plcsim reachable", "component", "startup", "addr", addr)
}
