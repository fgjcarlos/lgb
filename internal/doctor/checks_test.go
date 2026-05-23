// checks_test.go — tests for the Phase-0 check implementations.
//
// Requirements: MVP-FND-8.2. Design: §10, §4.3, §20.4.
package doctor

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fgjcarlos/lgb/internal/config"
	"github.com/fgjcarlos/lgb/internal/testutil"
)

// TestResticCheck_NeverReturnsFail verifies that restic-on-path returns WARN
// (not FAIL) when the binary is absent. (MVP-FND-8.2)
func TestResticCheck_NeverReturnsFail(t *testing.T) {
	c := &resticCheck{}
	if c.Name() != "restic-on-path" {
		t.Errorf("expected name %q, got %q", "restic-on-path", c.Name())
	}
	result := c.Run(context.Background())
	// Either PASS (found) or WARN (not found) — FAIL is forbidden.
	if result.Status == StatusFail {
		t.Error("restic check must not return FAIL — only WARN or PASS per spec MVP-FND-8.2")
	}
}

// TestDataDirCheck_FailWhenFileAtPath verifies data-dir-writable returns FAIL
// when a regular file exists at the target path (ErrDataDirInvalid path).
func TestDataDirCheck_FailWhenFileAtPath(t *testing.T) {
	dir := t.TempDir()
	targetFile := filepath.Join(dir, "not-a-dir")
	if err := os.WriteFile(targetFile, []byte("x"), 0600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	cfg := testutil.MinimalConfig(t)
	cfg.Gateway.DataDir = targetFile
	c := &dataDirCheck{cfg: cfg}

	result := c.Run(context.Background())
	if result.Status != StatusFail {
		t.Errorf("expected StatusFail for file-at-path, got %v (msg: %q)", result.Status, result.Message)
	}
}

// TestPortCheck_FailWhenPortBound verifies http-port-available returns FAIL
// when another listener holds the port. (MVP-FND-8.2)
func TestPortCheck_FailWhenPortBound(t *testing.T) {
	// Bind an ephemeral port to simulate a conflict.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer ln.Close()

	cfg := testutil.MinimalConfig(t)
	cfg.Server.HTTPAddr = ln.Addr().String()
	c := &portCheck{cfg: cfg}

	result := c.Run(context.Background())
	if result.Status != StatusFail {
		t.Errorf("expected StatusFail for bound port, got %v (msg: %q)", result.Status, result.Message)
	}
}

// TestGoRuntimeCheck_ReturnsInfo verifies go-runtime-version returns a
// non-empty message and a valid status. (MVP-FND-8.2)
func TestGoRuntimeCheck_ReturnsInfo(t *testing.T) {
	c := &goRuntimeCheck{}
	if c.Name() != "go-runtime-version" {
		t.Errorf("expected name %q, got %q", "go-runtime-version", c.Name())
	}
	result := c.Run(context.Background())
	if result.Message == "" {
		t.Error("expected non-empty message from go-runtime-version")
	}
	// Status must be INFO or PASS (not WARN or FAIL — this check is informational).
	if result.Status == StatusWarn || result.Status == StatusFail {
		t.Errorf("go-runtime-version must not return WARN or FAIL, got %v", result.Status)
	}
}

// TestConfigLoadedCheck_AlwaysPass verifies config-loaded always returns PASS.
// (MVP-FND-8.2)
func TestConfigLoadedCheck_AlwaysPass(t *testing.T) {
	c := &configLoadedCheck{}
	if c.Name() != "config-loaded" {
		t.Errorf("expected name %q, got %q", "config-loaded", c.Name())
	}
	result := c.Run(context.Background())
	if result.Status != StatusPass {
		t.Errorf("expected StatusPass, got %v", result.Status)
	}
}

// TestPLCReachableCheck_Pass verifies that the check returns StatusPass when a
// real TCP listener is bound at the configured address. (PLC-DOC-1.1)
func TestPLCReachableCheck_Pass(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer ln.Close()

	cfg := testutil.MinimalConfig(t)
	cfg.PLCs = []config.PLC{
		{Name: "test-plc", Address: ln.Addr().String(), SocketTimeout: "2s"},
	}

	c := &plcReachableCheck{plc: cfg.PLCs[0]}
	result := c.Run(context.Background())

	if result.Status != StatusPass {
		t.Errorf("expected StatusPass, got %v (msg: %q)", result.Status, result.Message)
	}
	if !strings.Contains(result.Message, ln.Addr().String()) {
		t.Errorf("expected message to contain address %q, got %q", ln.Addr().String(), result.Message)
	}
}

// TestPLCReachableCheck_Fail verifies that the check returns StatusFail when
// the configured address is not listening. (PLC-DOC-1.2)
func TestPLCReachableCheck_Fail(t *testing.T) {
	cfg := testutil.MinimalConfig(t)
	cfg.PLCs = []config.PLC{
		{Name: "unreachable-plc", Address: "127.0.0.1:19999", SocketTimeout: "500ms"},
	}

	c := &plcReachableCheck{plc: cfg.PLCs[0]}
	result := c.Run(context.Background())

	if result.Status != StatusFail {
		t.Errorf("expected StatusFail, got %v (msg: %q)", result.Status, result.Message)
	}
	if !strings.Contains(result.Message, "127.0.0.1:19999") {
		t.Errorf("expected message to contain address, got %q", result.Message)
	}
}

// TestPLCReachableCheck_NoPort_DefaultsTo44818 verifies that an address without
// a port is dialled on port 44818 (the EtherNet/IP default). (PLC-DOC-1.3)
func TestPLCReachableCheck_NoPort_DefaultsTo44818(t *testing.T) {
	// Start a listener on port 44818 on loopback; skip if port is in use.
	ln, err := net.Listen("tcp", "127.0.0.1:44818")
	if err != nil {
		t.Skipf("port 44818 unavailable (already bound): %v", err)
	}
	defer ln.Close()

	cfg := testutil.MinimalConfig(t)
	cfg.PLCs = []config.PLC{
		{Name: "no-port-plc", Address: "127.0.0.1", SocketTimeout: "2s"},
	}

	c := &plcReachableCheck{plc: cfg.PLCs[0]}
	result := c.Run(context.Background())

	// Should pass because we are listening on :44818.
	if result.Status != StatusPass {
		t.Errorf("expected StatusPass when :44818 is listening, got %v (msg: %q)", result.Status, result.Message)
	}
	// The resolved address used in the message should include :44818.
	if !strings.Contains(result.Message, "44818") {
		t.Errorf("expected message to reference port 44818, got %q", result.Message)
	}
}

// TestPLCReachableCheck_Timeout verifies that the check returns StatusFail
// within approximately 200 ms when a 50 ms socketTimeout is configured and the
// target is a black-hole address. (PLC-DOC-1.4)
func TestPLCReachableCheck_Timeout(t *testing.T) {
	// 192.0.2.0/24 is TEST-NET-1 (RFC 5737) — routable but never actually reachable.
	// It causes the dial to block until our timeout fires rather than rejecting
	// the connection immediately the way a closed loopback port would.
	cfg := testutil.MinimalConfig(t)
	cfg.PLCs = []config.PLC{
		{Name: "timeout-plc", Address: "192.0.2.1:44818", SocketTimeout: "50ms"},
	}

	c := &plcReachableCheck{plc: cfg.PLCs[0]}
	start := time.Now()
	result := c.Run(context.Background())
	elapsed := time.Since(start)

	if result.Status != StatusFail {
		t.Errorf("expected StatusFail for timeout case, got %v (msg: %q)", result.Status, result.Message)
	}
	if elapsed > 200*time.Millisecond {
		t.Errorf("check took too long: %v (expected < 200ms)", elapsed)
	}
}

// TestDefault_WithPLCs_RegistersCheck verifies that Default(cfg) with one PLC
// registers 6 checks total (5 baseline + 1 plc-reachable). (PLC-DOC-1.5)
func TestDefault_WithPLCs_RegistersCheck(t *testing.T) {
	cfg := testutil.MinimalConfig(t)
	cfg.PLCs = []config.PLC{
		{Name: "plc-a", Address: "192.168.1.10:44818", SocketTimeout: "1s"},
	}

	r := Default(cfg)
	checks := r.Checks()

	if len(checks) != 6 {
		t.Errorf("expected 6 checks with one PLC, got %d", len(checks))
	}
	// Last check must be the plc-reachable check for plc-a.
	last := checks[len(checks)-1]
	expectedName := "plc-reachable/plc-a"
	if last.Name() != expectedName {
		t.Errorf("expected last check name %q, got %q", expectedName, last.Name())
	}
}

// TestDefault_NoPLCs_NoCheckRegistered verifies that Default(cfg) with no PLCs
// registers exactly 5 checks. (PLC-DOC-1.5)
func TestDefault_NoPLCs_NoCheckRegistered(t *testing.T) {
	cfg := testutil.MinimalConfig(t)
	cfg.PLCs = nil

	r := Default(cfg)
	checks := r.Checks()

	if len(checks) != 5 {
		t.Errorf("expected 5 checks with no PLCs, got %d", len(checks))
	}
}
