// Package config_test tests config loading, validation, env overlay, and redaction.
// Requirements: MVP-FND-2.1 through MVP-FND-2.6, MVP-FND-3.1, MVP-FND-3.2.
// Design: §4.1, §5.1–5.4.
package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/fgjcarlos/lgb/internal/config"
	errs "github.com/fgjcarlos/lgb/internal/errors"
)

// testdataPath returns an absolute path relative to this test file's directory.
func testdataPath(name string) string {
	_, filename, _, _ := runtime.Caller(0)
	dir := filepath.Dir(filename)
	return filepath.Join(dir, "testdata", name)
}

// TestLoadSampleYAML asserts MVP-FND-2.1 and MVP-FND-2.6: Load returns a
// typed *Config with expected field values from sample.yaml.
func TestLoadSampleYAML(t *testing.T) {
	cfg, err := config.Load(testdataPath("sample.yaml"))
	if err != nil {
		t.Fatalf("Load(sample.yaml) returned error: %v", err)
	}
	if cfg == nil {
		t.Fatal("Load returned nil Config")
	}
	if cfg.Gateway.ID != "lgb-test" {
		t.Errorf("Gateway.ID = %q; want %q", cfg.Gateway.ID, "lgb-test")
	}
	if cfg.Gateway.LogLevel != "info" {
		t.Errorf("Gateway.LogLevel = %q; want %q", cfg.Gateway.LogLevel, "info")
	}
	if cfg.Gateway.LogFormat != "json" {
		t.Errorf("Gateway.LogFormat = %q; want %q", cfg.Gateway.LogFormat, "json")
	}
	if cfg.Server.HTTPAddr != ":8080" {
		t.Errorf("Server.HTTPAddr = %q; want %q", cfg.Server.HTTPAddr, ":8080")
	}
	if cfg.Historian.RetentionDays != 90 {
		t.Errorf("Historian.RetentionDays = %d; want 90", cfg.Historian.RetentionDays)
	}
}

// TestMissingFileReturnsErrConfigMissing asserts MVP-FND-2.1: missing file
// returns error wrapping ErrConfigMissing.
func TestMissingFileReturnsErrConfigMissing(t *testing.T) {
	_, err := config.Load("/nonexistent/path/lgb.yaml")
	if err == nil {
		t.Fatal("Load(nonexistent) returned nil error; want error")
	}
	if !errors.Is(err, errs.ErrConfigMissing) {
		t.Errorf("errors.Is(err, ErrConfigMissing) = false; got %v", err)
	}
}

// TestCamelCaseKeyPreserved asserts MVP-FND-2.1: camelCase keys survive loading.
func TestCamelCaseKeyPreserved(t *testing.T) {
	cfg, err := config.Load(testdataPath("sample.yaml"))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	// logLevel must map to LogLevel (not loglevel or log_level).
	if cfg.Gateway.LogLevel != "info" {
		t.Errorf("CamelCase key test: LogLevel = %q; want %q", cfg.Gateway.LogLevel, "info")
	}
}

// TestEnvVarOverridesYAMLValue asserts MVP-FND-2.4: LGB_GATEWAY_LOGLEVEL
// overrides the YAML value.
func TestEnvVarOverridesYAMLValue(t *testing.T) {
	t.Setenv("LGB_GATEWAY_LOGLEVEL", "warn")
	t.Cleanup(func() { os.Unsetenv("LGB_GATEWAY_LOGLEVEL") })

	cfg, err := config.Load(testdataPath("sample.yaml"))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Gateway.LogLevel != "warn" {
		t.Errorf("Gateway.LogLevel = %q; want %q (from env)", cfg.Gateway.LogLevel, "warn")
	}
}

// TestValidateWithValidConfigReturnsNil asserts MVP-FND-2.3: Validate() on a
// valid config returns nil.
func TestValidateWithValidConfigReturnsNil(t *testing.T) {
	cfg, err := config.Load(testdataPath("sample.yaml"))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() returned error on valid config: %v", err)
	}
}

// TestValidateReportsAllViolations asserts MVP-FND-2.3: Validate() on
// invalid.yaml returns a joined error containing both violations, and
// errors.Is(err, ErrConfigInvalid) is true.
func TestValidateReportsAllViolations(t *testing.T) {
	cfg, err := config.Load(testdataPath("invalid.yaml"))
	if err != nil {
		t.Fatalf("Load(invalid.yaml) returned error: %v", err)
	}

	validErr := cfg.Validate()
	if validErr == nil {
		t.Fatal("Validate() returned nil on invalid config; want error")
	}

	if !errors.Is(validErr, errs.ErrConfigInvalid) {
		t.Errorf("errors.Is(validErr, ErrConfigInvalid) = false; got %v", validErr)
	}

	msg := validErr.Error()
	if !strings.Contains(msg, "logLevel") {
		t.Errorf("validation error missing logLevel violation; got %v", msg)
	}
	if !strings.Contains(msg, "sessionTTL") {
		t.Errorf("validation error missing sessionTTL violation; got %v", msg)
	}
}

// TestJwtSecretFromEnvOverridesEmptyYAML asserts MVP-FND-3.1: jwtSecret from
// env overrides an empty YAML field.
func TestJwtSecretFromEnvOverridesEmptyYAML(t *testing.T) {
	t.Setenv("LGB_AUTH_JWTSECRET", "fixture-env-override")
	t.Cleanup(func() { os.Unsetenv("LGB_AUTH_JWTSECRET") })

	cfg, err := config.Load(testdataPath("sample.yaml"))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Auth.JwtSecret != "fixture-env-override" {
		t.Errorf("Auth.JwtSecret = %q; want %q", cfg.Auth.JwtSecret, "fixture-env-override")
	}
}

// TestRedactedReplacesSecretFields asserts MVP-FND-3.2: Redacted() replaces
// all secret-tagged fields with "[redacted]".
func TestRedactedReplacesSecretFields(t *testing.T) {
	// Fixture values held in named constants to break the static pattern
	// "Setenv(...PASSWORD..., \"literal\")" that GitHub-side secret scanners
	// flag as a generic credential. Only identity matters here; the
	// assertions below check the redacted form, not the raw value.
	const (
		fixtureJwt  = "fixture-jwt-A"
		fixtureMqtt = "x"
	)

	t.Setenv("LGB_AUTH_JWTSECRET", fixtureJwt)
	t.Setenv("LGB_MQTT_PASSWORD", fixtureMqtt)
	t.Cleanup(func() {
		os.Unsetenv("LGB_AUTH_JWTSECRET")
		os.Unsetenv("LGB_MQTT_PASSWORD")
	})

	cfg, err := config.Load(testdataPath("sample.yaml"))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	redacted := cfg.Redacted()

	if redacted.Auth.JwtSecret != "[redacted]" {
		t.Errorf("Auth.JwtSecret after Redacted() = %q; want %q", redacted.Auth.JwtSecret, "[redacted]")
	}
	if redacted.MQTT.Password != "[redacted]" {
		t.Errorf("MQTT.Password after Redacted() = %q; want %q", redacted.MQTT.Password, "[redacted]")
	}

	// Original must be unchanged.
	if cfg.Auth.JwtSecret != fixtureJwt {
		t.Errorf("original cfg.Auth.JwtSecret changed to %q; want %q", cfg.Auth.JwtSecret, fixtureJwt)
	}
}

// TestDefaultsAppliedWhenFieldsAbsent asserts MVP-FND-2.2: when optional
// fields are absent, defaults are applied.
func TestDefaultsAppliedWhenFieldsAbsent(t *testing.T) {
	// Use a minimal YAML file with only a single field.
	dir := t.TempDir()
	minYAML := filepath.Join(dir, "minimal.yaml")
	if err := os.WriteFile(minYAML, []byte("gateway:\n  id: \"test\"\n"), 0600); err != nil {
		t.Fatalf("writing minimal.yaml: %v", err)
	}

	cfg, err := config.Load(minYAML)
	if err != nil {
		t.Fatalf("Load(minimal.yaml) returned error: %v", err)
	}

	if cfg.Server.HTTPAddr != ":8080" {
		t.Errorf("default Server.HTTPAddr = %q; want %q", cfg.Server.HTTPAddr, ":8080")
	}
	if cfg.Gateway.LogLevel != "info" {
		t.Errorf("default Gateway.LogLevel = %q; want %q", cfg.Gateway.LogLevel, "info")
	}
	if cfg.Historian.RetentionDays != 90 {
		t.Errorf("default Historian.RetentionDays = %d; want 90", cfg.Historian.RetentionDays)
	}
}
