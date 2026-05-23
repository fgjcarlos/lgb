// checks_test.go — tests for the Phase-0 check implementations.
//
// Requirements: MVP-FND-8.2. Design: §10, §4.3, §20.4.
package doctor

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"

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
