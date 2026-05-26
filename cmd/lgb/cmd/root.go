// root.go — Cobra root command and dependency injection container.
//
// No package-level globals. PersistentPreRunE populates Deps before any
// subcommand's RunE executes. Design §6.2–6.4, §20.1.
// Requirements: MVP-FND-1.1, MVP-FND-5.5.
package cmd

import (
	"context"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/fgjcarlos/lgb/internal/config"
	"github.com/fgjcarlos/lgb/internal/doctor"
	"github.com/fgjcarlos/lgb/internal/historian"
	"github.com/fgjcarlos/lgb/internal/log"
	"github.com/fgjcarlos/lgb/internal/plc"
	"github.com/fgjcarlos/lgb/internal/server"
)

// Deps is the dependency container shared across all subcommands.
// Early-bound fields are set by flag parsing; late-bound fields are populated
// by PersistentPreRunE before any subcommand's RunE executes.
type Deps struct {
	// Early-bound: set by root-level flags.
	ConfigPath string
	DataDir    string
	LogLevel   string
	LogFormat  string
	JSON       bool

	// Late-bound: populated by PersistentPreRunE.
	Config *config.Config
	Logger *slog.Logger

	// DoctorRegistry is the check registry used by the doctor subcommand.
	// When nil, the doctor command calls doctor.Default(d.Config) to build it.
	// Tests inject a pre-populated *doctor.Registry to avoid real side-effects.
	DoctorRegistry *doctor.Registry

	// DataDirEnsureFn is the datadir.Ensure function used by the server command.
	// When nil, the production datadir.Ensure is called.
	// Tests inject a spy to verify the call without real filesystem side-effects.
	DataDirEnsureFn func(path string) (string, error)

	// PLCManagerFactory creates a PLCManager from config. When nil, the
	// production plc.NewManager is used (when PLCs are configured).
	PLCManagerFactory func(cfg *config.Config, tagCb plc.TagCallback) server.PLCManager

	// SparkplugNodeFactory creates a SparkplugNode from config. When nil, the
	// production sparkplug.NewEdgeNode is used (when GroupID is configured).
	SparkplugNodeFactory func(cfg *config.Config) server.SparkplugNode

	// HistorianStoreFactory creates a historian Store. When nil, the
	// production historian.Open is used (when retentionDays > 0).
	HistorianStoreFactory func(ctx context.Context, path string, opts historian.Options) (*historian.Store, error)

	// serverRef is set by runServerTo so test helpers can retrieve the *server.Server.
	// Unexported — test access is via getServerForTest().
	serverRef **server.Server

	// Injectable exit function — defaults to os.Exit; replaceable in tests.
	Exit func(code int)
}

// NewRoot constructs the root Cobra command and returns it alongside the
// shared *Deps. Callers (including main.go) should call root.Execute().
//
// Subcommands are registered here and receive d by pointer so that
// PersistentPreRunE's late-binding of Config and Logger is visible to them.
func NewRoot() (*cobra.Command, *Deps) {
	d := &Deps{
		Exit: os.Exit,
	}

	root := &cobra.Command{
		Use:   "lgb",
		Short: "LGB — industrial IoT gateway",
		Long: `lgb is the LGB gateway binary.

It provides a Cobra command tree for starting the HTTP server,
running diagnostics, validating configuration, and reporting version.`,
		// SilenceUsage prevents Cobra from printing usage on every error.
		SilenceUsage: true,
		// SilenceErrors prevents Cobra from printing the error itself;
		// the root executor prints it explicitly.
		SilenceErrors: true,
	}

	// Persistent flags — inherited by every subcommand.
	root.PersistentFlags().StringVar(&d.ConfigPath, "config", "lgb.yaml", "path to YAML config file")
	root.PersistentFlags().StringVar(&d.DataDir, "data-dir", "", "override data directory (default: platform default)")
	root.PersistentFlags().StringVar(&d.LogLevel, "log-level", "", "override log level (debug|info|warn|error)")
	root.PersistentFlags().StringVar(&d.LogFormat, "log-format", "", "override log format (text|json)")
	root.PersistentFlags().BoolVar(&d.JSON, "json", false, "emit machine-readable JSON output")

	// PersistentPreRunE loads config and builds the logger before any
	// subcommand runs. It skips for subcommands that don't need full config
	// (currently none — all benefit from having a logger).
	root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		return persistentPreRun(cmd, d)
	}

	// Register subcommands.
	root.AddCommand(NewVersionCmd(d))
	root.AddCommand(NewStatusCmd(d))
	root.AddCommand(NewConfigCmd(d))
	root.AddCommand(NewDoctorCmd(d))
	root.AddCommand(NewServerCmd(d))

	return root, d
}

// persistentPreRun loads the config and builds the logger, populating d.Config
// and d.Logger. Called by PersistentPreRunE.
func persistentPreRun(_ *cobra.Command, d *Deps) error {
	cfg, err := config.Load(d.ConfigPath)
	if err != nil {
		return err
	}

	// Apply CLI overrides on top of the loaded config.
	if d.LogLevel != "" {
		cfg.Gateway.LogLevel = d.LogLevel
	}
	if d.LogFormat != "" {
		cfg.Gateway.LogFormat = d.LogFormat
	}
	if d.DataDir != "" {
		cfg.Gateway.DataDir = d.DataDir
	}

	d.Config = cfg

	// Build the logger from the (possibly overridden) config.
	logger, err := log.New(log.Options{
		Level:  cfg.Gateway.LogLevel,
		Format: cfg.Gateway.LogFormat,
		Out:    os.Stderr,
	})
	if err != nil {
		return err
	}
	d.Logger = logger

	// Set the global default logger (spec MVP-FND-4.6, design §7).
	slog.SetDefault(logger)

	return nil
}
