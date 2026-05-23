// server.go — lgb server subcommand.
//
// Validates jwtSecret, bootstraps dataDir, and starts the HTTP server.
// Signal handling via signal.NotifyContext happens here — internal/server
// is kept signal-free for testability. Design §6.3, §20.1.
// Requirements: MVP-FND-1.3, MVP-FND-1.9, MVP-FND-7.5.
package cmd

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/fgjcarlos/lgb/internal/datadir"
	"github.com/fgjcarlos/lgb/internal/doctor"
	"github.com/fgjcarlos/lgb/internal/log"
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

	// Build doctor checks for the server.
	checks := doctor.Default(cfg).Checks()

	// Start the server.
	srv := server.New(cfg, logger, checks)

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
