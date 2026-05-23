// status_test.go — tests for the status subcommand.
//
// Requirements: MVP-FND-1.5. Design: §6.1.
package cmd

import (
	"bytes"
	"encoding/json"
	"testing"
)

// TestStatusCmd_JSONOutput verifies that `lgb status` prints valid JSON with
// "status":"ok" and exits 0. (MVP-FND-1.5 "Status prints stub JSON")
func TestStatusCmd_JSONOutput(t *testing.T) {
	d := &Deps{}
	buf := &bytes.Buffer{}

	if err := runStatusToWriter(d, buf); err != nil {
		t.Fatalf("status command returned error: %v", err)
	}

	var out statusOutput
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("output is not valid JSON: %v — got: %q", err, buf.String())
	}
	if out.Status != "ok" {
		t.Errorf("expected status=%q, got %q", "ok", out.Status)
	}
}

// TestStatusCmd_ExitCodeZero verifies the status command exits 0.
func TestStatusCmd_ExitCodeZero(t *testing.T) {
	d := &Deps{}
	buf := &bytes.Buffer{}
	if err := runStatusToWriter(d, buf); err != nil {
		t.Errorf("expected exit 0, got error: %v", err)
	}
}
