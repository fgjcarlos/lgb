//go:build e2e

// Package e2e contains end-to-end tests for the LGB gateway binary.
//
// These tests spawn the pre-built binary and assert exit codes and output shapes.
// They require that `make build` has been run first.
//
// Run with: go test -tags=e2e ./cmd/lgb/e2e/...
//
// Requirements: MVP-FND-1.2–1.6. Design: §17.
package e2e

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// lgbBin returns the path to the pre-built binary.
// Defaults to ../../bin/lgb (relative to this file's package).
func lgbBin(t *testing.T) string {
	t.Helper()
	// Prefer LGB_BIN env var for CI flexibility.
	if bin := os.Getenv("LGB_BIN"); bin != "" {
		return bin
	}
	// Default: repo root bin/lgb.
	// t's test binary is in cmd/lgb/e2e/ so root is ../../..
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatalf("cannot resolve repo root: %v", err)
	}
	return filepath.Join(root, "bin", "lgb")
}

// sampleYAML returns the path to the canonical test config.
func sampleYAML(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatalf("cannot resolve repo root: %v", err)
	}
	return filepath.Join(root, "cmd", "lgb", "testdata", "sample.yaml")
}

// TestE2E_VersionJSON verifies `lgb version --json` emits valid JSON + exit 0.
// (MVP-FND-1.2)
func TestE2E_VersionJSON(t *testing.T) {
	bin := lgbBin(t)
	cfg := sampleYAML(t)

	out, err := exec.Command(bin, "--config", cfg, "--json", "version").CombinedOutput()
	if err != nil {
		t.Fatalf("lgb version --json failed: %v\noutput: %s", err, out)
	}

	var result map[string]string
	if jsonErr := json.Unmarshal(out, &result); jsonErr != nil {
		t.Fatalf("output is not valid JSON: %v — got: %q", jsonErr, string(out))
	}
	for _, key := range []string{"version", "commit", "date"} {
		if result[key] == "" {
			t.Errorf("expected non-empty JSON field %q", key)
		}
	}
}

// TestE2E_Status verifies `lgb status` emits JSON with status:"ok" + exit 0.
// (MVP-FND-1.5)
func TestE2E_Status(t *testing.T) {
	bin := lgbBin(t)
	cfg := sampleYAML(t)

	out, err := exec.Command(bin, "--config", cfg, "status").CombinedOutput()
	if err != nil {
		t.Fatalf("lgb status failed: %v\noutput: %s", err, out)
	}

	var result map[string]interface{}
	if jsonErr := json.Unmarshal(out, &result); jsonErr != nil {
		t.Fatalf("output is not valid JSON: %v — got: %q", jsonErr, string(out))
	}
	if result["status"] != "ok" {
		t.Errorf("expected status=%q, got %v", "ok", result["status"])
	}
}

// TestE2E_ConfigValidate verifies `lgb config validate` exits 0 for sample.yaml.
// (MVP-FND-1.6)
func TestE2E_ConfigValidate(t *testing.T) {
	bin := lgbBin(t)
	cfg := sampleYAML(t)

	out, err := exec.Command(bin, "--config", cfg, "config", "validate").CombinedOutput()
	if err != nil {
		t.Fatalf("lgb config validate failed: %v\noutput: %s", err, out)
	}
	if !strings.Contains(string(out), "config OK") {
		t.Errorf("expected output to contain %q, got: %q", "config OK", string(out))
	}
}
