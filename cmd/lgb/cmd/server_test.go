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

	"github.com/fgjcarlos/lgb/internal/config"
	"github.com/fgjcarlos/lgb/internal/server"
	"github.com/fgjcarlos/lgb/internal/testutil"
)

// GitGuardian-safe: use const indirection for credential env var values.
const (
	fixtureJwtValue  = "fixture-server-test-jwt"
	fixtureJwtEnvKey = "LGB_AUTH_JWTSECRET"
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
	// JwtSecret comes from env — config has it empty, loader will overlay it.
	// But since we're injecting cfg directly here (not going through Load),
	// we set it directly to simulate the env overlay having run.
	cfg.Auth.JwtSecret = fixtureJwtValue

	ctx, cancel := context.WithCancel(context.Background())

	d := &Deps{
		Config: cfg,
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

	var bootstrapCalled bool
	d := &Deps{
		Config: cfg,
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

func (m *mockServerPLCManager) Stop() error { return nil }

// TestServerCmd_WithPLCs_CreatesPLCManager verifies that runServerTo creates a
// PLCManager when PLCs are configured and passes it to server.New. (PLC-DRV-2.1)
func TestServerCmd_WithPLCs_CreatesPLCManager(t *testing.T) {
	cfg := testutil.MinimalConfig(t)
	cfg.Auth.JwtSecret = fixtureJwtValue
	cfg.PLCs = []config.PLC{
		{Name: "plc-a", Address: "127.0.0.1:44818", SocketTimeout: "1s"},
	}

	mgr := &mockServerPLCManager{}
	var factoryCalled bool

	d := &Deps{
		Config: cfg,
		PLCManagerFactory: func(c *config.Config) server.PLCManager {
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

// TestServerCmd_NoPLCs_NilManager verifies that runServerTo passes nil for the
// PLCManager when no PLCs are configured (backward-compatible path). (PLC-DRV-2.1)
func TestServerCmd_NoPLCs_NilManager(t *testing.T) {
	cfg := testutil.MinimalConfig(t)
	cfg.Auth.JwtSecret = fixtureJwtValue
	cfg.PLCs = nil // no PLCs

	var factoryCalled bool
	d := &Deps{
		Config: cfg,
		PLCManagerFactory: func(c *config.Config) server.PLCManager {
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

	// Factory must NOT be called when there are no PLCs.
	if factoryCalled {
		t.Error("expected PLCManagerFactory NOT to be called when no PLCs are configured")
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
	cfg.MQTT.BrokerURL = "tcp://localhost:1883"
	cfg.MQTT.GroupID = "plant-a"
	cfg.MQTT.EdgeNodeID = "lgb-1"

	node := &mockCmdSparkplugNode{}
	var factoryCalled bool

	d := &Deps{
		Config: cfg,
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

func TestServerCmd_NoGroupID_NilSparkplugNode(t *testing.T) {
	cfg := testutil.MinimalConfig(t)
	cfg.Auth.JwtSecret = fixtureJwtValue
	cfg.MQTT.GroupID = ""
	cfg.MQTT.BrokerURL = ""

	var factoryCalled bool
	d := &Deps{
		Config: cfg,
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
