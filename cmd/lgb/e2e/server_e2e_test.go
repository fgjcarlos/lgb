//go:build e2e

// Package e2e — server end-to-end test.
//
// Spawns the pre-built binary, polls /health, then sends SIGTERM and asserts
// clean exit.
//
// Requirements: MVP-FND-1.3. Design: §17.
package e2e

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

// GitGuardian-safe: use const indirection for credential env var values.
const (
	e2eJwtFixture  = "e2e-fixture-jwt"
	e2eJwtEnvKey   = "LGB_AUTH_JWTSECRET"
)

// TestE2E_ServerHealthAndSIGTERM spawns the server, polls /health, then
// sends SIGTERM and asserts exit 0. (MVP-FND-1.3, MVP-FND-1.9)
func TestE2E_ServerHealthAndSIGTERM(t *testing.T) {
	bin := lgbBin(t)
	cfg := sampleYAML(t)

	// GitGuardian-safe: env value via const, not literal.
	env := append(os.Environ(), fmt.Sprintf("%s=%s", e2eJwtEnvKey, e2eJwtFixture))

	cmd := exec.Command(bin, "--config", cfg, "server")
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start lgb server: %v", err)
	}

	// Poll /health until it returns 200 or 5s elapses.
	addr := ":8080" // sample.yaml has server.httpAddr: ":8080"
	healthURL := fmt.Sprintf("http://localhost%s/health", addr)
	deadline := time.Now().Add(5 * time.Second)
	var ready bool
	for time.Now().Before(deadline) {
		resp, err := http.Get(healthURL)
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			ready = true
			break
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !ready {
		_ = cmd.Process.Kill()
		t.Fatal("server /health did not return 200 within 5s")
	}

	// Send SIGTERM and wait for clean exit.
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("SIGTERM failed: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("expected clean exit (code 0), got: %v", err)
		}
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		t.Error("server did not exit within 5s after SIGTERM")
	}
}
