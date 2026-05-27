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
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/fgjcarlos/lgb/internal/auth"
	"github.com/fgjcarlos/lgb/internal/backup"
	"github.com/fgjcarlos/lgb/internal/config"
	"github.com/fgjcarlos/lgb/internal/datadir"
	"github.com/fgjcarlos/lgb/internal/doctor"
	"github.com/fgjcarlos/lgb/internal/historian"
	"github.com/fgjcarlos/lgb/internal/log"
	"github.com/fgjcarlos/lgb/internal/mqtt"
	"github.com/fgjcarlos/lgb/internal/plc"
	"github.com/fgjcarlos/lgb/internal/server"
	"github.com/fgjcarlos/lgb/internal/sparkplug"
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

	// Create the Sparkplug Edge Node when MQTT + GroupID are configured.
	var spNode server.SparkplugNode
	if cfg.MQTT.GroupID != "" {
		factory := d.SparkplugNodeFactory
		if factory == nil {
			factory = defaultSparkplugNodeFactory
		}
		spNode = factory(cfg)
		logger.Info("sparkplug edge node created",
			slog.String("component", "sparkplug"),
			slog.String("group", cfg.MQTT.GroupID),
			slog.String("node", cfg.MQTT.EdgeNodeID))
	}

	// Create the Historian Store + Writer when retentionDays > 0.
	var histStore *historian.Store
	var histWriter *historian.Writer
	if cfg.Historian.RetentionDays > 0 {
		dbPath := filepath.Join(resolvedPath, "lgb.db")
		openFn := d.HistorianStoreFactory
		if openFn == nil {
			openFn = historian.Open
		}
		var storeErr error
		histStore, storeErr = openFn(ctx, dbPath, historian.Options{
			RetentionDays: cfg.Historian.RetentionDays,
		})
		if storeErr != nil {
			return fmt.Errorf("server: open historian: %w", storeErr)
		}
		defer histStore.Close()
		histWriter = historian.NewWriter(histStore, historian.WriterOptions{})
		logger.Info("historian created",
			slog.String("component", "historian"),
			slog.String("db", dbPath),
			slog.Int("retention_days", cfg.Historian.RetentionDays))

		go runRetentionLoop(ctx, histStore, logger)
	}

	// Build the fan-out TagCallback that feeds both historian and sparkplug.
	var tagCb plc.TagCallback
	if histWriter != nil || spNode != nil {
		var spHandler func(sparkplug.TagUpdate)
		if h, ok := spNode.(interface{ HandleTagUpdate(sparkplug.TagUpdate) }); ok {
			spHandler = h.HandleTagUpdate
		}
		tagCb = buildTagCallback(ctx, histWriter, spHandler, logger)
	}

	// Create the PLC Manager when PLCs are configured.
	var plcMgr server.PLCManager
	if len(cfg.PLCs) > 0 {
		factory := d.PLCManagerFactory
		if factory == nil {
			factory = defaultPLCManagerFactory
		}
		plcMgr = factory(cfg, tagCb)
		logger.Info("plc manager created", slog.String("component", "plc-manager"),
			slog.Int("plc_count", len(cfg.PLCs)))
	}

	var histW server.HistorianWriter
	if histWriter != nil {
		histW = histWriter
	}

	// Create the Backup Scheduler when repos are configured.
	var bkpSch server.BackupScheduler
	if len(cfg.Backup.Repos) > 0 {
		interval, _ := time.ParseDuration(cfg.Backup.Interval)
		if interval <= 0 {
			interval = 24 * time.Hour
		}

		repos := make([]backup.Repository, len(cfg.Backup.Repos))
		for i, r := range cfg.Backup.Repos {
			repos[i] = backup.Repository{URL: r.URL, Password: r.Password}
		}
		bkpMgr := backup.NewManager(nil, repos)
		snapshotDir := filepath.Join(resolvedPath, "backup-tmp")
		sched := backup.NewScheduler(bkpMgr, []string{snapshotDir}, interval)

		if histStore != nil {
			sched.PreBackup = func(ctx context.Context) error {
				_, err := histStore.VacuumInto(ctx, snapshotDir)
				return err
			}
		}

		bkpSch = sched
		logger.Info("backup scheduler created",
			slog.String("component", "backup"),
			slog.Int("repo_count", len(repos)),
			slog.String("interval", interval.String()))
	}

	sessionTTL := 8 * time.Hour
	if cfg.Auth.SessionTTL != "" {
		parsedTTL, ttlErr := time.ParseDuration(cfg.Auth.SessionTTL)
		if ttlErr != nil {
			return fmt.Errorf("server: auth.sessionTTL: %w", ttlErr)
		}
		sessionTTL = parsedTTL
	}
	tokenService := auth.NewTokenService(cfg.Auth.JwtSecret, sessionTTL)

	srv := server.New(cfg, logger, checks, server.Opts{
		PLCMgr:     plcMgr,
		SpNode:     spNode,
		HistW:      histW,
		BkpSch:     bkpSch,
		AuthTokens: tokenService,
	})

	// Store server reference for tests.
	if d.serverRef != nil {
		*d.serverRef = srv
	}

	// Start config file watcher for PLC hot-reload.
	if plcMgr != nil && d.ConfigPath != "" {
		watchCtx := ctx
		go func() {
			_ = config.Watch(watchCtx, d.ConfigPath, func(newCfg *config.Config) {
				logger.Info("config changed, reloading PLC manager", slog.String("component", "config-watch"))
				if err := plcMgr.Reload(watchCtx, newCfg); err != nil {
					logger.Warn("plc manager reload error",
						slog.String("component", "config-watch"),
						slog.String("err", err.Error()))
				}
			})
		}()
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
func defaultPLCManagerFactory(cfg *config.Config, tagCb plc.TagCallback) server.PLCManager {
	return plc.NewManager(cfg, slog.Default(), nil, tagCb)
}

// defaultSparkplugNodeFactory is the production SparkplugNodeFactory.
func defaultSparkplugNodeFactory(cfg *config.Config) server.SparkplugNode {
	// Build NDEATH payload for MQTT Will message per Sparkplug B spec.
	ndeathTopic := fmt.Sprintf("spBv1.0/%s/NDEATH/%s", cfg.MQTT.GroupID, cfg.MQTT.EdgeNodeID)
	ndeathPayload, _ := sparkplug.BuildNDEATH(0)

	keepAlive := 30 * time.Second
	if cfg.MQTT.KeepAlive != "" {
		if d, err := time.ParseDuration(cfg.MQTT.KeepAlive); err == nil && d > 0 {
			keepAlive = d
		}
	}

	mqttClient := mqtt.NewClient(mqtt.Options{
		BrokerURL:    cfg.MQTT.BrokerURL,
		ClientID:     cfg.MQTT.ClientID,
		Password:     cfg.MQTT.Password,
		QoS:          byte(cfg.MQTT.QoS),
		KeepAlive:    keepAlive,
		CleanSession: cfg.MQTT.CleanSession,
		WillTopic:    ndeathTopic,
		WillPayload:  ndeathPayload,
		WillQoS:      1,
		WillRetain:   false,
	})

	var devices []sparkplug.DeviceConfig
	for _, p := range cfg.PLCs {
		var tags []sparkplug.TagDef
		for _, t := range p.Tags {
			tags = append(tags, sparkplug.TagDef{Name: t.Name, SparkplugType: t.Type})
		}
		devices = append(devices, sparkplug.DeviceConfig{DeviceID: p.Name, Tags: tags})
	}

	return sparkplug.NewEdgeNode(sparkplug.EdgeNodeConfig{
		GroupID: cfg.MQTT.GroupID,
		NodeID:  cfg.MQTT.EdgeNodeID,
		Client:  mqttClient,
		Devices: devices,
		Log:     slog.Default(),
	})
}

// buildTagCallback creates a fan-out callback that sends tag updates to both
// the historian writer and the sparkplug edge node.
func buildTagCallback(ctx context.Context, hw *historian.Writer, spHandler func(sparkplug.TagUpdate), logger *slog.Logger) plc.TagCallback {
	return func(u plc.TagUpdate) {
		if hw != nil {
			if err := hw.Enqueue(ctx, historian.Sample{
				PLCName:   u.PLCName,
				Tag:       u.Tag,
				Value:     u.Value,
				Timestamp: u.Timestamp,
				Quality:   "good",
			}); err != nil {
				logger.Warn("historian enqueue error",
					slog.String("component", "historian"),
					slog.String("tag", u.Tag),
					slog.String("err", err.Error()))
			}
		}
		if spHandler != nil {
			spHandler(sparkplug.TagUpdate{
				PLCName:   u.PLCName,
				Tag:       u.Tag,
				Value:     u.Value,
				Timestamp: u.Timestamp,
			})
		}
	}
}

// runRetentionLoop runs EnforceRetention every hour until ctx is cancelled.
func runRetentionLoop(ctx context.Context, store *historian.Store, logger *slog.Logger) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := store.EnforceRetention(ctx, time.Now()); err != nil {
				logger.Warn("historian retention error",
					slog.String("component", "historian"),
					slog.String("err", err.Error()))
			}
		}
	}
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
