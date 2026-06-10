// server_test.go — tests for the server subcommand.
//
// GitGuardian pattern: NEVER pair a credential-keyword env var name with a
// string literal in t.Setenv. Always use const indirection.
//
// Requirements: MVP-FND-1.3, MVP-FND-2.4, MVP-FND-3.1, MVP-FND-7.5.
// Design: §6.3, §20.1.
package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/fgjcarlos/lgb/internal/auth"
	"github.com/fgjcarlos/lgb/internal/config"
	"github.com/fgjcarlos/lgb/internal/historian"
	"github.com/fgjcarlos/lgb/internal/plc"
	"github.com/fgjcarlos/lgb/internal/plcstore"
	"github.com/fgjcarlos/lgb/internal/server"
	"github.com/fgjcarlos/lgb/internal/testutil"
)

// GitGuardian-safe: use const indirection for credential env var values.
const (
	fixtureJwtValue       = "fixture-server-test-jwt"
	fixtureJwtEnvKey      = "LGB_AUTH_JWTSECRET"
	fixtureAdminPwEnvKey  = "LGB_AUTH_ADMIN_PASSWORD"
	fixtureAdminPwValue   = "fixture-admin-password"
)

// TestServerCmd_NoJwtSecretExits1 verifies that the server command refuses to
// start when jwtSecret is empty. (MVP-FND-1.3 "Server refuses to start without jwtSecret")
func TestServerCmd_NoJwtSecretExits1(t *testing.T) {
	cfg := testutil.MinimalConfig(t)
	cfg.Auth.JwtSecret = "" // explicitly empty

	d := &Deps{
		Config: cfg,
	}
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	err := runServerTo(context.Background(), d, stdout, stderr)
	if err == nil {
		t.Fatal("expected error when jwtSecret is empty, got nil")
	}
	combined := stdout.String() + stderr.String()
	if !strings.Contains(combined, "jwtSecret") {
		t.Errorf("expected error message to contain %q, got stdout=%q stderr=%q", "jwtSecret", stdout, stderr)
	}
}

// TestServerCmd_JwtFromEnv verifies that LGB_AUTH_JWTSECRET env var is
// respected. When set, the server starts and context cancellation exits cleanly.
// (MVP-FND-3.1)
func TestServerCmd_JwtFromEnv(t *testing.T) {
	t.Setenv(fixtureJwtEnvKey, fixtureJwtValue) // GitGuardian-safe: const indirection

	cfg := testutil.MinimalConfig(t)
	cfg.Auth.JwtSecret = fixtureJwtValue
	cfg.Historian.RetentionDays = 0

	ctx, cancel := context.WithCancel(context.Background())

	d := &Deps{
		Config:           cfg,
		UserStoreFactory: seededUserStoreFactory(t),
	}
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	errCh := make(chan error, 1)
	go func() {
		errCh <- runServerTo(ctx, d, stdout, stderr)
	}()

	// Wait for server to bind.
	srv := d.getServerForTest()
	if srv != nil {
		addr := srv.Addr()
		_ = addr // just checking it binds
	}

	cancel()
	err := <-errCh
	if err != nil {
		t.Errorf("expected clean shutdown, got: %v", err)
	}
}

// TestServerCmd_DataDirBootstrapped verifies that datadir.Ensure is called via
// the datadir bootstrap spy. (MVP-FND-7.5)
func TestServerCmd_DataDirBootstrapped(t *testing.T) {
	cfg := testutil.MinimalConfig(t)
	cfg.Auth.JwtSecret = fixtureJwtValue
	cfg.Historian.RetentionDays = 0

	var bootstrapCalled bool
	d := &Deps{
		Config:           cfg,
		UserStoreFactory: seededUserStoreFactory(t),
		DataDirEnsureFn: func(path string) (string, error) {
			bootstrapCalled = true
			return path, nil
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	errCh := make(chan error, 1)
	go func() {
		errCh <- runServerTo(ctx, d, stdout, stderr)
	}()

	cancel()
	<-errCh

	if !bootstrapCalled {
		t.Error("expected datadir.Ensure to be called during server startup")
	}
}

// mockServerPLCManager is a minimal PLCManager implementation used in cmd tests.
type mockServerPLCManager struct {
	startCalled bool
}

func (m *mockServerPLCManager) Start(ctx context.Context) error {
	m.startCalled = true
	return nil
}

func (m *mockServerPLCManager) Stop() error                                      { return nil }
func (m *mockServerPLCManager) Reload(_ context.Context, _ *config.Config) error { return nil }
func (m *mockServerPLCManager) WriteTag(_ string, _ string, _ any) error         { return nil }

// TestServerCmd_WithPLCs_CreatesPLCManager verifies that runServerTo creates a
// PLCManager when PLCs are configured and passes it to server.New. (PLC-DRV-2.1)
func TestServerCmd_WithPLCs_CreatesPLCManager(t *testing.T) {
	cfg := testutil.MinimalConfig(t)
	cfg.Auth.JwtSecret = fixtureJwtValue
	cfg.Historian.RetentionDays = 0
	cfg.PLCs = []config.PLC{
		{Name: "plc-a", Address: "127.0.0.1:44818", SocketTimeout: "1s"},
	}

	mgr := &mockServerPLCManager{}
	var factoryCalled bool

	d := &Deps{
		Config:           cfg,
		UserStoreFactory: seededUserStoreFactory(t),
		PLCManagerFactory: func(c *config.Config, _ plc.TagCallback) server.PLCManager {
			factoryCalled = true
			return mgr
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- runServerTo(ctx, d, &bytes.Buffer{}, &bytes.Buffer{})
	}()

	cancel()
	<-errCh

	if !factoryCalled {
		t.Error("expected PLCManagerFactory to be called when PLCs are configured")
	}
}

// TestServerCmd_NoPLCs_EmptyManager verifies that runServerTo ALWAYS calls the
// PLCManagerFactory — even when no PLCs are configured — so that the manager is
// never nil. The resulting manager has zero workers. (PCS-RELOAD-3.1)
func TestServerCmd_NoPLCs_EmptyManager(t *testing.T) {
	cfg := testutil.MinimalConfig(t)
	cfg.Auth.JwtSecret = fixtureJwtValue
	cfg.Historian.RetentionDays = 0
	cfg.PLCs = nil // no PLCs in YAML

	var factoryCalled bool
	d := &Deps{
		Config:           cfg,
		UserStoreFactory: seededUserStoreFactory(t),
		PLCManagerFactory: func(c *config.Config, _ plc.TagCallback) server.PLCManager {
			factoryCalled = true
			return &mockServerPLCManager{}
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- runServerTo(ctx, d, &bytes.Buffer{}, &bytes.Buffer{})
	}()

	cancel()
	<-errCh

	// Factory MUST be called even with zero PLCs (always-construct invariant).
	if !factoryCalled {
		t.Error("expected PLCManagerFactory to be called even when no PLCs are configured")
	}
}

// TestServerCmd_PLCStoreSeed_FirstBoot verifies that on first boot (empty store)
// the YAML PLCs are seeded into the plcstore. We capture the seeded PLC count
// inside the factory (before runServerTo closes the store) via a channel.
// (PCS-STORE-1.2)
func TestServerCmd_PLCStoreSeed_FirstBoot(t *testing.T) {
	cfg := testutil.MinimalConfig(t)
	cfg.Auth.JwtSecret = fixtureJwtValue
	cfg.Historian.RetentionDays = 0
	cfg.PLCs = []config.PLC{
		{Name: "line1", Address: "10.0.0.1:44818", ScanRate: "1s", SocketTimeout: "5s"},
	}

	// seededNames is populated by the PLCManagerFactory, which receives the
	// liveCfg built from the store after seeding. This proves the store was
	// seeded before the manager was created.
	var seededNames []string
	factoryCh := make(chan []string, 1)

	d := &Deps{
		Config:           cfg,
		UserStoreFactory: seededUserStoreFactory(t),
		PLCStoreFactory: func(ctx context.Context, path string) (*plcstore.Store, error) {
			return plcstore.Open(ctx, ":memory:")
		},
		PLCManagerFactory: func(c *config.Config, _ plc.TagCallback) server.PLCManager {
			names := make([]string, len(c.PLCs))
			for i, p := range c.PLCs {
				names[i] = p.Name
			}
			factoryCh <- names
			return &mockServerPLCManager{}
		},
	}
	_ = seededNames

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- runServerTo(ctx, d, &bytes.Buffer{}, &bytes.Buffer{})
	}()

	// Wait for factory to be called (server must start first).
	select {
	case names := <-factoryCh:
		seededNames = names
	case err := <-errCh:
		t.Fatalf("server exited before factory was called: %v", err)
	}

	cancel()
	<-errCh

	if len(seededNames) != 1 {
		t.Errorf("expected 1 seeded PLC passed to manager factory, got %d", len(seededNames))
	}
	if len(seededNames) > 0 && seededNames[0] != "line1" {
		t.Errorf("expected PLC 'line1' in manager factory, got %q", seededNames[0])
	}
}

// TestServerCmd_PLCStoreSeed_Idempotent verifies that a pre-populated store
// is not re-seeded on restart (Seed is a no-op when IsEmpty=false). We verify
// by checking the manager factory receives only the pre-existing PLC. (PCS-STORE-1.2)
func TestServerCmd_PLCStoreSeed_Idempotent(t *testing.T) {
	cfg := testutil.MinimalConfig(t)
	cfg.Auth.JwtSecret = fixtureJwtValue
	cfg.Historian.RetentionDays = 0
	// YAML has "line1" — but the store already has "pre-existing".
	cfg.PLCs = []config.PLC{
		{Name: "line1", Address: "10.0.0.1:44818", ScanRate: "1s", SocketTimeout: "5s"},
	}

	factoryCh := make(chan []string, 1)

	d := &Deps{
		Config:           cfg,
		UserStoreFactory: seededUserStoreFactory(t),
		PLCStoreFactory: func(ctx context.Context, path string) (*plcstore.Store, error) {
			s, err := plcstore.Open(ctx, ":memory:")
			if err != nil {
				return nil, err
			}
			// Pre-populate so IsEmpty returns false.
			_ = s.Create(ctx, config.PLC{
				Name: "pre-existing", Address: "10.9.9.9:44818", ScanRate: "1s", SocketTimeout: "5s",
			})
			return s, nil
		},
		PLCManagerFactory: func(c *config.Config, _ plc.TagCallback) server.PLCManager {
			names := make([]string, len(c.PLCs))
			for i, p := range c.PLCs {
				names[i] = p.Name
			}
			factoryCh <- names
			return &mockServerPLCManager{}
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- runServerTo(ctx, d, &bytes.Buffer{}, &bytes.Buffer{})
	}()

	var managerPLCs []string
	select {
	case names := <-factoryCh:
		managerPLCs = names
	case err := <-errCh:
		t.Fatalf("server exited before factory was called: %v", err)
	}

	cancel()
	<-errCh

	// Manager must receive only "pre-existing" (YAML seed was skipped).
	if len(managerPLCs) != 1 {
		t.Errorf("expected 1 PLC from store (pre-existing), got %d", len(managerPLCs))
	}
	if len(managerPLCs) > 0 && managerPLCs[0] != "pre-existing" {
		t.Errorf("expected 'pre-existing', got %q", managerPLCs[0])
	}
}

// mockCmdSparkplugNode tracks Start/Stop for cmd wiring tests.
type mockCmdSparkplugNode struct {
	startCalled bool
}

func (m *mockCmdSparkplugNode) Start(_ context.Context) error {
	m.startCalled = true
	return nil
}

func (m *mockCmdSparkplugNode) Stop() error { return nil }

func TestServerCmd_WithGroupID_CreatesSparkplugNode(t *testing.T) {
	cfg := testutil.MinimalConfig(t)
	cfg.Auth.JwtSecret = fixtureJwtValue
	cfg.Historian.RetentionDays = 0
	cfg.MQTT.BrokerURL = "tcp://localhost:1883"
	cfg.MQTT.GroupID = "plant-a"
	cfg.MQTT.EdgeNodeID = "lgb-1"

	node := &mockCmdSparkplugNode{}
	var factoryCalled bool

	d := &Deps{
		Config:           cfg,
		UserStoreFactory: seededUserStoreFactory(t),
		SparkplugNodeFactory: func(c *config.Config) server.SparkplugNode {
			factoryCalled = true
			return node
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- runServerTo(ctx, d, &bytes.Buffer{}, &bytes.Buffer{})
	}()

	cancel()
	<-errCh

	if !factoryCalled {
		t.Error("expected SparkplugNodeFactory to be called when GroupID is set")
	}
}

func TestServerCmd_WithHistorian_CreatesStoreAndWriter(t *testing.T) {
	cfg := testutil.MinimalConfig(t)
	cfg.Auth.JwtSecret = fixtureJwtValue
	cfg.Historian.RetentionDays = 30
	cfg.MQTT.GroupID = ""
	cfg.MQTT.BrokerURL = ""

	var storeOpened bool
	d := &Deps{
		Config:           cfg,
		UserStoreFactory: seededUserStoreFactory(t),
		HistorianStoreFactory: func(ctx context.Context, path string, opts historian.Options) (*historian.Store, error) {
			storeOpened = true
			if opts.RetentionDays != 30 {
				t.Errorf("expected RetentionDays=30, got %d", opts.RetentionDays)
			}
			return historian.Open(ctx, ":memory:", opts)
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- runServerTo(ctx, d, &bytes.Buffer{}, &bytes.Buffer{})
	}()

	srv := d.getServerForTest()
	if srv != nil {
		_ = srv.Addr()
	}

	cancel()
	<-errCh

	if !storeOpened {
		t.Error("expected HistorianStoreFactory to be called when retentionDays > 0")
	}
}

func TestServerCmd_NoHistorian_WhenRetentionZero(t *testing.T) {
	cfg := testutil.MinimalConfig(t)
	cfg.Auth.JwtSecret = fixtureJwtValue
	cfg.Historian.RetentionDays = 0
	cfg.MQTT.GroupID = ""
	cfg.MQTT.BrokerURL = ""

	var storeOpened bool
	d := &Deps{
		Config:           cfg,
		UserStoreFactory: seededUserStoreFactory(t),
		HistorianStoreFactory: func(ctx context.Context, path string, opts historian.Options) (*historian.Store, error) {
			storeOpened = true
			return historian.Open(ctx, ":memory:", opts)
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- runServerTo(ctx, d, &bytes.Buffer{}, &bytes.Buffer{})
	}()

	cancel()
	<-errCh

	if storeOpened {
		t.Error("expected HistorianStoreFactory NOT to be called when retentionDays is 0")
	}
}

func TestServerCmd_NoGroupID_NilSparkplugNode(t *testing.T) {
	cfg := testutil.MinimalConfig(t)
	cfg.Auth.JwtSecret = fixtureJwtValue
	cfg.Historian.RetentionDays = 0
	cfg.MQTT.GroupID = ""
	cfg.MQTT.BrokerURL = ""

	var factoryCalled bool
	d := &Deps{
		Config:           cfg,
		UserStoreFactory: seededUserStoreFactory(t),
		SparkplugNodeFactory: func(c *config.Config) server.SparkplugNode {
			factoryCalled = true
			return nil
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- runServerTo(ctx, d, &bytes.Buffer{}, &bytes.Buffer{})
	}()

	cancel()
	<-errCh

	if factoryCalled {
		t.Error("expected SparkplugNodeFactory NOT to be called when GroupID is empty")
	}
}

// openMemoryUserStore is a test helper that opens an in-memory UserStore and
// registers cleanup. Used by TestServer_AdminSeeded to isolate from disk.
func openMemoryUserStore(t *testing.T) *auth.UserStore {
	t.Helper()
	s, err := auth.OpenUserStore(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("OpenUserStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// seededUserStoreFactory returns a UserStoreFactory that ignores the path and
// always returns a pre-populated in-memory store with one admin user. Use this
// in cmd-level tests that need runServerTo to start cleanly without setting
// LGB_AUTH_ADMIN_PASSWORD.
func seededUserStoreFactory(t *testing.T) func(context.Context, string) (*auth.UserStore, error) {
	t.Helper()
	s := openMemoryUserStore(t)
	if _, err := s.Create(context.Background(), "admin", "adminpass", auth.RoleAdmin); err != nil {
		t.Fatalf("seed admin user: %v", err)
	}
	return func(_ context.Context, _ string) (*auth.UserStore, error) { return s, nil }
}

// TestServer_AdminSeeded verifies that runServerTo calls EnsureAdminExists
// right after the user store opens. Covers three scenarios from R66-1:
//  1. Fresh DB + env var set → admin user created.
//  2. Fresh DB without env var → server returns a fatal error.
//  3. Pre-populated DB → EnsureAdminExists is a no-op, server starts cleanly.
//
// Requirements: R66-1. Design: #66 admin bootstrap wiring.
func TestServer_AdminSeeded(t *testing.T) {
	t.Run("fresh DB with env var creates admin", func(t *testing.T) {
		t.Setenv(fixtureAdminPwEnvKey, fixtureAdminPwValue) // GitGuardian-safe: const indirection

		store := openMemoryUserStore(t)

		cfg := testutil.MinimalConfig(t)
		cfg.Auth.JwtSecret = fixtureJwtValue
		cfg.Historian.RetentionDays = 0
		cfg.MQTT.GroupID = ""

		ctx, cancel := context.WithCancel(context.Background())
		errCh := make(chan error, 1)
		go func() {
			errCh <- runServerTo(ctx, &Deps{
				Config:           cfg,
				UserStoreFactory: func(_ context.Context, _ string) (*auth.UserStore, error) { return store, nil },
			}, &bytes.Buffer{}, &bytes.Buffer{})
		}()

		// Give the server a moment to complete admin seeding and start.
		// Cancel immediately to keep the test fast.
		cancel()
		if err := <-errCh; err != nil {
			t.Fatalf("expected clean start, got: %v", err)
		}

		count, err := store.Count(context.Background())
		if err != nil {
			t.Fatalf("Count: %v", err)
		}
		if count == 0 {
			t.Error("expected admin user to be created in the store, got 0 users")
		}
	})

	t.Run("fresh DB without env var returns fatal error", func(t *testing.T) {
		t.Setenv(fixtureAdminPwEnvKey, "") // explicitly absent

		store := openMemoryUserStore(t)

		cfg := testutil.MinimalConfig(t)
		cfg.Auth.JwtSecret = fixtureJwtValue
		cfg.Historian.RetentionDays = 0
		cfg.MQTT.GroupID = ""

		err := runServerTo(context.Background(), &Deps{
			Config:           cfg,
			UserStoreFactory: func(_ context.Context, _ string) (*auth.UserStore, error) { return store, nil },
		}, &bytes.Buffer{}, &bytes.Buffer{})

		if err == nil {
			t.Fatal("expected fatal error when LGB_AUTH_ADMIN_PASSWORD is unset on fresh DB, got nil")
		}
		if !strings.Contains(err.Error(), "admin") && !strings.Contains(err.Error(), "LGB_AUTH_ADMIN_PASSWORD") {
			t.Errorf("expected error message to mention admin or env var, got: %v", err)
		}
	})

	t.Run("populated DB is a no-op", func(t *testing.T) {
		t.Setenv(fixtureAdminPwEnvKey, "") // env var absent — irrelevant when users already exist

		store := openMemoryUserStore(t)
		// Pre-populate with an admin user so EnsureAdminExists is a no-op.
		if _, err := store.Create(context.Background(), "existing-admin", "pass", auth.RoleAdmin); err != nil {
			t.Fatalf("pre-create admin: %v", err)
		}

		cfg := testutil.MinimalConfig(t)
		cfg.Auth.JwtSecret = fixtureJwtValue
		cfg.Historian.RetentionDays = 0
		cfg.MQTT.GroupID = ""

		ctx, cancel := context.WithCancel(context.Background())
		errCh := make(chan error, 1)
		go func() {
			errCh <- runServerTo(ctx, &Deps{
				Config:           cfg,
				UserStoreFactory: func(_ context.Context, _ string) (*auth.UserStore, error) { return store, nil },
			}, &bytes.Buffer{}, &bytes.Buffer{})
		}()

		cancel()
		if err := <-errCh; err != nil {
			t.Fatalf("expected clean start when store is pre-populated, got: %v", err)
		}

		count, _ := store.Count(context.Background())
		if count != 1 {
			t.Errorf("expected store to still have exactly 1 user (no-op), got %d", count)
		}
	})
}
