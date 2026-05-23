// Package config provides the typed configuration schema, loader, validator,
// hot-reload watcher, and secret redaction for the LGB gateway.
//
// # Secret Convention
//
// Fields tagged with `secret:"true"` contain sensitive values that MUST NOT
// appear in logs. The canonical source for these values is environment
// variables following the pattern LGB_{SECTION_UPPER}_{FIELD_UPPER}:
//
//   - auth.jwtSecret  → LGB_AUTH_JWTSECRET
//   - mqtt.password   → LGB_MQTT_PASSWORD
//   - mqtt.passwordFile → LGB_MQTT_PASSWORDFILE
//
// Use (*Config).Redacted() when logging config objects. Per MVP-FND-3.1 and ADR-0002.
//
// Requirements: MVP-FND-2.1–2.6, MVP-FND-3.1, MVP-FND-3.2. Design: §4.1, §5.1–5.4.
package config

import (
	"reflect"
	"time"

	errs "github.com/fgjcarlos/lgb/internal/errors"
)

// Re-export sentinels for ergonomic local use (design §8, decision #9).
var (
	ErrConfigInvalid    = errs.ErrConfigInvalid
	ErrConfigMissing    = errs.ErrConfigMissing
	ErrConfigPermission = errs.ErrConfigPermission
)

// Config is the canonical typed view of lgb.yaml.
// It is the single source of truth for all configuration field names and types.
// Per MVP-FND-2.6, callers MUST NOT access koanf internals directly.
type Config struct {
	Gateway   GatewaySection   `koanf:"gateway"`
	Server    ServerSection    `koanf:"server"`
	Auth      AuthSection      `koanf:"auth"`
	MQTT      MQTTSection      `koanf:"mqtt"`
	Historian HistorianSection `koanf:"historian"`
	Backup    BackupSection    `koanf:"backup"`
	PLCs      []PLC            `koanf:"plcs"`
	PLCSim    PLCSimSection    `koanf:"plcsim"`
}

// PLCSimSection holds configuration for the in-process PLC simulator probe.
// The gateway performs a TCP dial to Addr on startup and logs the result
// (informational only — does not fail startup). Requirements: MVP-FND-9.3.
type PLCSimSection struct {
	// Addr is the TCP address of the plcsim service to probe.
	// Default: "plcsim:44818" (the Docker Compose service name + EtherNet/IP port).
	// Override via LGB_PLCSIM_ADDR or the plcsim.addr YAML field.
	Addr string `koanf:"addr"`
}

// GatewaySection holds gateway-level settings.
type GatewaySection struct {
	ID        string `koanf:"id"`
	LogLevel  string `koanf:"logLevel"`
	LogFormat string `koanf:"logFormat"`
	DataDir   string `koanf:"dataDir"`
}

// ServerSection holds HTTP server settings.
type ServerSection struct {
	HTTPAddr        string `koanf:"httpAddr"`
	TLSEnabled      bool   `koanf:"tlsEnabled"`
	ShutdownTimeout string `koanf:"shutdownTimeout"`
}

// AuthSection holds authentication settings.
type AuthSection struct {
	JwtSecret  string `koanf:"jwtSecret"  secret:"true"`
	SessionTTL string `koanf:"sessionTTL"`
}

// MQTTSection holds MQTT broker settings.
type MQTTSection struct {
	BrokerURL    string `koanf:"brokerURL"`
	ClientID     string `koanf:"clientID"`
	Password     string `koanf:"password"     secret:"true"`
	PasswordFile string `koanf:"passwordFile" secret:"true"`
}

// HistorianSection holds historian/SQLite settings.
type HistorianSection struct {
	RetentionDays int `koanf:"retentionDays"`
}

// BackupSection holds backup/restic settings.
type BackupSection struct {
	Repos []BackupRepo `koanf:"repos"`
}

// BackupRepo holds a single restic repository configuration.
type BackupRepo struct {
	URL      string `koanf:"url"`
	Password string `koanf:"password" secret:"true"`
}

// PLC holds CIP/gologix PLC settings.
type PLC struct {
	Name    string `koanf:"name"`
	Address string `koanf:"address"`
}

// Validate checks the loaded config against schema constraints.
//
// It uses errors.Join so ALL violations are reported at once (MVP-FND-2.3).
// Each violation wraps ErrConfigInvalid so errors.Is works on the result.
func (c *Config) Validate() error {
	var violations []error

	// gateway.logLevel must be one of debug|info|warn|error.
	switch c.Gateway.LogLevel {
	case "debug", "info", "warn", "error":
		// valid
	default:
		violations = append(violations, errorf("gateway.logLevel: %q is not one of debug|info|warn|error: %w", c.Gateway.LogLevel, ErrConfigInvalid))
	}

	// gateway.logFormat must be one of text|json.
	switch c.Gateway.LogFormat {
	case "text", "json":
		// valid
	default:
		violations = append(violations, errorf("gateway.logFormat: %q is not one of text|json: %w", c.Gateway.LogFormat, ErrConfigInvalid))
	}

	// server.httpAddr must be non-empty.
	if c.Server.HTTPAddr == "" {
		violations = append(violations, errorf("server.httpAddr: must not be empty: %w", ErrConfigInvalid))
	}

	// auth.sessionTTL must be a valid Go duration string when non-empty.
	if c.Auth.SessionTTL != "" {
		if _, err := time.ParseDuration(c.Auth.SessionTTL); err != nil {
			violations = append(violations, errorf("auth.sessionTTL: %q is not a valid Go duration: %w", c.Auth.SessionTTL, ErrConfigInvalid))
		}
	}

	// historian.retentionDays must be positive when non-zero.
	if c.Historian.RetentionDays < 0 {
		violations = append(violations, errorf("historian.retentionDays: must be a positive integer, got %d: %w", c.Historian.RetentionDays, ErrConfigInvalid))
	}

	return errs.Join(violations...)
}

// Redacted returns a deep copy of the config where every field tagged
// `secret:"true"` is replaced with the literal "[redacted]".
//
// Use this method whenever logging or displaying the config to prevent secret
// leakage (MVP-FND-3.2, design §5.4).
func (c *Config) Redacted() *Config {
	copy := *c
	redactStructFields(reflect.ValueOf(&copy))
	return &copy
}

// redactStructFields recursively replaces string fields tagged `secret:"true"`
// with "[redacted]" using reflection.
func redactStructFields(v reflect.Value) {
	// Dereference pointers.
	for v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return
		}
		v = v.Elem()
	}

	switch v.Kind() {
	case reflect.Struct:
		t := v.Type()
		for i := 0; i < v.NumField(); i++ {
			field := v.Field(i)
			ft := t.Field(i)
			if ft.Tag.Get("secret") == "true" && field.Kind() == reflect.String && field.CanSet() {
				field.SetString("[redacted]")
			} else {
				redactStructFields(field)
			}
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			redactStructFields(v.Index(i))
		}
	}
}
