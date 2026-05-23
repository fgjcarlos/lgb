// config_validate_test.go — tests for the config validate subcommand.
//
// Requirements: MVP-FND-1.6. Design: §6.1, §6.5.
package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// TestConfigValidate_ValidSampleYAML verifies that sample.yaml → "config OK" + exit 0.
// (MVP-FND-1.6 "Valid config exits 0")
func TestConfigValidate_ValidSampleYAML(t *testing.T) {
	d := &Deps{ConfigPath: "testdata/sample.yaml"}
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	err := runConfigValidateTo(d, stdout, stderr)
	if err != nil {
		t.Fatalf("expected nil error for valid config, got: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "config OK") {
		t.Errorf("expected stdout to contain %q, got: %q", "config OK", out)
	}
}

// TestConfigValidate_InvalidYAML verifies that invalid.yaml → exit 1 + both violations listed.
// (MVP-FND-1.6 "Invalid config exits 1 with all violations listed")
func TestConfigValidate_InvalidYAML(t *testing.T) {
	d := &Deps{ConfigPath: "testdata/invalid.yaml"}
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	err := runConfigValidateTo(d, stdout, stderr)
	if err == nil {
		t.Fatal("expected non-nil error for invalid config, got nil")
	}
	combined := stdout.String() + stderr.String()
	if !strings.Contains(combined, "logLevel") && !strings.Contains(combined, "sessionTTL") {
		// Check that we have two violation mentions
		t.Errorf("expected both violations to be listed in output, got stdout=%q stderr=%q", stdout, stderr)
	}
}

// TestConfigValidate_MissingFile verifies that a missing config → exit 1 + path referenced.
// (MVP-FND-1.6 "Missing config file exits 1")
func TestConfigValidate_MissingFile(t *testing.T) {
	d := &Deps{ConfigPath: "testdata/nonexistent.yaml"}
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	err := runConfigValidateTo(d, stdout, stderr)
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
	combined := stdout.String() + stderr.String()
	if !strings.Contains(combined, "nonexistent") {
		t.Errorf("expected error message to reference missing file path, got stdout=%q stderr=%q", stdout, stderr)
	}
}

// TestConfigValidate_JSONValid verifies that --json + valid config → {"valid":true}.
// (MVP-FND-1.6, design §6.5)
func TestConfigValidate_JSONValid(t *testing.T) {
	d := &Deps{ConfigPath: "testdata/sample.yaml", JSON: true}
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	if err := runConfigValidateTo(d, stdout, stderr); err != nil {
		t.Fatalf("expected nil error for valid config, got: %v", err)
	}

	var out configValidateOutput
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("output is not valid JSON: %v — got: %q", err, stdout.String())
	}
	if !out.Valid {
		t.Errorf("expected valid=true, got false; errors: %v", out.Errors)
	}
}

// TestConfigValidate_JSONInvalid verifies --json + invalid config → {"valid":false,"errors":[...]}.
func TestConfigValidate_JSONInvalid(t *testing.T) {
	d := &Deps{ConfigPath: "testdata/invalid.yaml", JSON: true}
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	err := runConfigValidateTo(d, stdout, stderr)
	if err == nil {
		t.Fatal("expected error for invalid config")
	}

	var out configValidateOutput
	if jsonErr := json.Unmarshal(stdout.Bytes(), &out); jsonErr != nil {
		t.Fatalf("output is not valid JSON: %v — got: %q", jsonErr, stdout.String())
	}
	if out.Valid {
		t.Error("expected valid=false")
	}
	if len(out.Errors) == 0 {
		t.Error("expected errors array to be non-empty")
	}
}
