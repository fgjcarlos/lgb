// loader.go — koanf-backed configuration loader.
//
// Provider stack (merge order, later wins):
//  1. confmap defaults       — compiled-in defaults for every field
//  2. file + yaml.Parser()   — user-edited YAML at the --config path
//  3. env (prefix LGB_, _)   — secret + override env vars
//
// CLI overrides (--data-dir, --log-level, --log-format) are applied by
// cmd/lgb/cmd/root.go after Load returns, by mutating *Config fields directly.
//
// Secret convention: fields tagged `secret:"true"` on Config get their runtime
// values from environment variables following LGB_{SECTION_UPPER}_{FIELD_UPPER}.
// See ADR-0002 and MVP-FND-3.1.
package config

import (
	"fmt"
	"os"
	"reflect"
	"strings"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

// defaults contains compiled-in default values for every field.
// These are applied first so later providers can selectively override.
var defaults = map[string]interface{}{
	"gateway.id":              "lgb-1",
	"gateway.logLevel":        "info",
	"gateway.logFormat":       "text",
	"server.httpAddr":         ":8080",
	"server.tlsEnabled":       false,
	"server.shutdownTimeout":  "10s",
	"auth.sessionTTL":         "8h",
	"historian.retentionDays": 90,
	// PLCSim probe defaults: Docker Compose service name + EtherNet/IP port.
	// Override via LGB_PLCSIM_ADDR or plcsim.addr YAML field.
	"plcsim.addr": "plcsim:44818",
}

// envKeyMap maps uppercase env var suffixes (after stripping LGB_) to
// canonical koanf dot-notation keys (preserving camelCase).
// Generated once from Config struct tags at package init.
var envKeyMap map[string]string

func init() {
	envKeyMap = buildEnvKeyMap(reflect.TypeOf(Config{}), "")
}

// buildEnvKeyMap recursively inspects struct tags to build the
// uppercase-env → camelCase-koanf mapping.
func buildEnvKeyMap(t reflect.Type, prefix string) map[string]string {
	m := make(map[string]string)
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		koanfKey := f.Tag.Get("koanf")
		if koanfKey == "" || koanfKey == "-" {
			continue
		}
		fullKey := koanfKey
		if prefix != "" {
			fullKey = prefix + "." + koanfKey
		}
		envSuffix := strings.ToUpper(strings.ReplaceAll(fullKey, ".", "_"))
		m[envSuffix] = fullKey

		// Recurse into nested structs (not slices).
		ft := f.Type
		if ft.Kind() == reflect.Struct {
			for k, v := range buildEnvKeyMap(ft, fullKey) {
				m[k] = v
			}
		}
	}
	return m
}

// Load reads the YAML at path, overlays LGB_* env vars, applies defaults,
// and returns a fully populated *Config.
//
// On failure it returns an error wrapping one of:
//   - ErrConfigMissing    — file not found
//   - ErrConfigPermission — file not readable
//
// Note: Load does NOT call Validate. Callers should call cfg.Validate() after
// Load if strict validation is required (e.g. lgb server startup). This allows
// loading a config for inspection purposes (lgb config validate) even when it
// has violations.
func Load(path string) (*Config, error) {
	k := koanf.New(".")

	// Layer 1: compiled-in defaults via confmap provider.
	if err := k.Load(confmap.Provider(defaults, "."), nil); err != nil {
		return nil, fmt.Errorf("config: loading defaults: %w", err)
	}

	// Layer 2: YAML file.
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, fmt.Errorf("config: file not found %q: %w", path, ErrConfigMissing)
	}
	fp := file.Provider(path)
	if err := k.Load(fp, yaml.Parser()); err != nil {
		// Distinguish permission errors from parse errors.
		if os.IsPermission(err) {
			return nil, fmt.Errorf("config: reading %q: %w", path, ErrConfigPermission)
		}
		return nil, fmt.Errorf("config: parsing %q: %w", path, err)
	}

	// Layer 3: env vars with LGB_ prefix.
	// The callback maps LGB_{SUFFIX} to the canonical camelCase koanf key
	// using the pre-built envKeyMap (generated from struct tags at init).
	// e.g. LGB_GATEWAY_LOGLEVEL → gateway.logLevel (preserving camelCase).
	envProvider := env.Provider("LGB_", ".", func(s string) string {
		suffix := strings.TrimPrefix(s, "LGB_")
		if canonical, ok := envKeyMap[suffix]; ok {
			return canonical
		}
		// Fallback: lowercase with dots (handles unknown/future fields).
		parts := strings.SplitN(suffix, "_", 2)
		if len(parts) != 2 {
			return strings.ToLower(suffix)
		}
		return strings.ToLower(parts[0]) + "." + strings.ToLower(parts[1])
	})
	if err := k.Load(envProvider, nil); err != nil {
		return nil, fmt.Errorf("config: loading env vars: %w", err)
	}

	// Unmarshal into typed struct.
	var cfg Config
	if err := k.Unmarshal("", &cfg); err != nil {
		return nil, fmt.Errorf("config: unmarshalling: %w", err)
	}

	return &cfg, nil
}
