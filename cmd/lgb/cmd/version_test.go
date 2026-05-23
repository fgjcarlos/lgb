// version_test.go — tests for the version subcommand.
//
// Tests call runVersionToWriter directly with a pre-populated *Deps so
// PersistentPreRunE (which needs a config file) is bypassed.
// This is the canonical CLI smoke test pattern for this project. Design §6.3.
// Requirements: MVP-FND-1.2. Design: §6.1, §6.5.
package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// TestVersionCmd_PlainOutput verifies that plain (no --json) output contains
// the "dev" fallback string. (MVP-FND-1.2 "Development build fallback")
func TestVersionCmd_PlainOutput(t *testing.T) {
	d := &Deps{}
	buf := &bytes.Buffer{}

	if err := runVersionToWriter(d, buf); err != nil {
		t.Fatalf("version command returned error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "dev") {
		t.Errorf("expected output to contain %q, got: %q", "dev", out)
	}
}

// TestVersionCmd_JSONOutput verifies that --json emits valid JSON with the
// required keys. (MVP-FND-1.2 "Version output JSON")
func TestVersionCmd_JSONOutput(t *testing.T) {
	d := &Deps{JSON: true}
	buf := &bytes.Buffer{}

	if err := runVersionToWriter(d, buf); err != nil {
		t.Fatalf("version --json returned error: %v", err)
	}

	var out versionOutput
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("output is not valid JSON: %v — got: %q", err, buf.String())
	}
	if out.Version == "" {
		t.Error("JSON version field is empty")
	}
	if out.Commit == "" {
		t.Error("JSON commit field is empty")
	}
	if out.Date == "" {
		t.Error("JSON date field is empty")
	}
}

// TestVersionCmd_ExitCodeZero verifies the version command exits 0 in all cases.
func TestVersionCmd_ExitCodeZero(t *testing.T) {
	for _, tc := range []struct {
		name string
		json bool
	}{
		{"plain", false},
		{"json", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := &Deps{JSON: tc.json}
			buf := &bytes.Buffer{}
			if err := runVersionToWriter(d, buf); err != nil {
				t.Errorf("expected exit 0, got error: %v", err)
			}
		})
	}
}
