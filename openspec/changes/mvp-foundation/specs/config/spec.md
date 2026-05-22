---
change: mvp-foundation
domain: config
phase: spec
date: 2026-05-22
status: draft
---

# Config Specification

## Purpose

Configuration loading, schema validation, environment-variable secret overlay, hot-reload, and key-case preservation for the LGB gateway. Also defines the secret management convention for all present and future sensitive fields.

## Requirements

### [MVP-FND-2.1] YAML loading via koanf v2

The config loader MUST use `github.com/knadh/koanf/v2` with its `yaml` file provider. It MUST load a YAML file at the path passed to it. It MUST preserve key case — camelCase fields such as `scanRateMs` and `brokerURL` MUST NOT be lowercased or snake_cased during load. `CGO_ENABLED=0` MUST be satisfied; koanf v2 is pure-Go.

#### Scenario: camelCase key is preserved

- GIVEN a config YAML with `gateway.logLevel: "debug"`
- WHEN the loader reads the file
- THEN `k.String("gateway.logLevel")` returns `"debug"` (not `"loglevel"` or `"log_level"`)

#### Scenario: Missing config file returns error

- GIVEN no file exists at the specified path
- WHEN `Load(path)` is called
- THEN it returns a non-nil error wrapping `ErrConfigMissing`

---

### [MVP-FND-2.2] Required fields and defaults

The following fields MUST be present after load+validation or MUST resolve to their default:

| Field | Required | Default | Notes |
|-------|----------|---------|-------|
| `gateway.id` | SHOULD | `"lgb-1"` | Unique gateway instance name |
| `gateway.logLevel` | NO | `"info"` | One of `debug\|info\|warn\|error` |
| `gateway.logFormat` | NO | `"text"` | One of `text\|json` |
| `gateway.dataDir` | NO | platform default | See `data-dir` spec |
| `server.httpAddr` | NO | `":8080"` | TCP listen address |
| `server.tlsEnabled` | NO | `false` | TLS on the HTTP server |
| `auth.jwtSecret` | conditional | — | MUST be non-empty if `lgb server` is started; see §MVP-FND-3.x |
| `auth.sessionTTL` | NO | `"8h"` | Valid Go duration string |
| `historian.retentionDays` | NO | `90` | Positive integer |

#### Scenario: Default values are applied when fields are absent

- GIVEN a minimal YAML with only `gateway.id: "test"`
- WHEN the loader runs
- THEN `server.httpAddr` resolves to `":8080"`
- AND `gateway.logLevel` resolves to `"info"`

---

### [MVP-FND-2.3] Schema validation

The loader MUST validate the loaded config against the schema after merging all providers. Validation MUST use `errors.Join` to aggregate all violations before returning, so a single call surfaces every error simultaneously. The returned error MUST be (or wrap) `ErrConfigInvalid`. Validation MUST check:

- `gateway.logLevel` is one of `debug|info|warn|error`
- `gateway.logFormat` is one of `text|json`
- `server.httpAddr` is a non-empty string
- `auth.sessionTTL` is a valid Go duration string if non-empty
- `historian.retentionDays` is a positive integer if non-zero

#### Scenario: All violations reported at once

- GIVEN a config with both `gateway.logLevel: "verbose"` and `auth.sessionTTL: "not-a-duration"`
- WHEN `Validate()` is called
- THEN the returned error contains messages for BOTH violations
- AND `errors.Is(err, ErrConfigInvalid)` returns true

#### Scenario: Valid config passes validation

- GIVEN `testdata/sample.yaml` with all valid values
- WHEN `Validate()` is called
- THEN it returns nil

---

### [MVP-FND-2.4] Environment-variable overlay — `LGB_{SECTION}_{FIELD}` pattern

The loader MUST merge environment variables using the prefix `LGB_` and the naming convention `LGB_{SECTION_UPPER}_{FIELD_UPPER}`. Environment variable values MUST override YAML values. Keys with nested structures follow the pattern: `LGB_GATEWAY_DATADIR` overrides `gateway.dataDir`, `LGB_SERVER_HTTPADDR` overrides `server.httpAddr`. The koanf env provider MUST be registered after the YAML provider so env wins.

#### Scenario: Env var overrides YAML value

- GIVEN a YAML config with `gateway.logLevel: "info"`
- AND env var `LGB_GATEWAY_LOGLEVEL=debug` is set
- WHEN the loader runs
- THEN `gateway.logLevel` resolves to `"debug"`

#### Scenario: Env var absent — YAML value used

- GIVEN a YAML config with `server.httpAddr: ":9090"`
- AND no `LGB_SERVER_HTTPADDR` env var
- WHEN the loader runs
- THEN `server.httpAddr` resolves to `":9090"`

---

### [MVP-FND-2.5] Hot-reload via `file.Provider.Watch()`

The config package MUST expose a `Watch(ctx context.Context, onChange func(cfg *Config)) error` function (or equivalent). It MUST use koanf's `file.Provider.Watch()` to detect file changes. It MUST debounce multiple rapid writes and emit a single reload event per logical change (debounce window SHOULD be ≥ 200 ms). The watcher goroutine MUST stop when `ctx` is cancelled.

Platform note: on Linux `inotify` is used; on macOS `kqueue`/`FSEvents`; on Windows `ReadDirectoryChangesW`. All three are supported by koanf's file watcher without code changes.

#### Scenario: File change triggers reload callback

- GIVEN the watcher is running against a valid config file
- WHEN the file is modified on disk
- THEN `onChange` is called exactly once with the updated config (within 1 s)

#### Scenario: Rapid successive writes trigger one callback

- GIVEN the watcher is running
- WHEN the config file is written 5 times within 100 ms
- THEN `onChange` is called exactly once
- AND it is called with the final file state

#### Scenario: Context cancel stops watcher

- GIVEN the watcher is running with a cancellable context
- WHEN the context is cancelled
- THEN `Watch()` returns `ctx.Err()`
- AND no further callbacks are invoked

---

### [MVP-FND-2.6] Config struct is the canonical output type

The loader MUST return a typed `*Config` struct (not a raw map). Callers MUST NOT access koanf internals directly outside `internal/config/`. The `Config` struct MUST be defined in `internal/config/config.go` and MUST be the single source of truth for field names and types.

#### Scenario: Loader returns typed struct

- GIVEN a valid YAML config
- WHEN `Load(path)` is called
- THEN it returns a `*Config` value with all fields populated
- AND callers can access `cfg.Gateway.LogLevel` without type assertions

---

### [MVP-FND-3.1] Secret fields — env-var-only convention

The following fields are designated secret fields. They MAY appear in YAML as empty strings (as documentation placeholders) but their runtime values MUST come from environment variables. The loader MUST document this in its package-level comment and in ADR-0002.

| YAML path | Env var override |
|-----------|-----------------|
| `auth.jwtSecret` | `LGB_AUTH_JWT_SECRET` |
| `mqtt.password` | `LGB_MQTT_PASSWORD` |
| `mqtt.passwordFile` | `LGB_MQTT_PASSWORDFILE` |
| `backup.repos[N].password` | `LGB_BACKUP_REPOS_{N}_PASSWORD` |

Future secret fields added to the schema MUST follow the same `LGB_{SECTION_UPPER}_{FIELD_UPPER}` pattern. The rule MUST be documented in `internal/config/loader.go` doc comment.

#### Scenario: jwtSecret from env overrides empty YAML

- GIVEN YAML has `auth.jwtSecret: ""`
- AND env var `LGB_AUTH_JWT_SECRET=supersecret` is set
- WHEN the loader runs
- THEN `cfg.Auth.JwtSecret` is `"supersecret"`

#### Scenario: Missing required secret blocks server start

- GIVEN YAML has `auth.jwtSecret: ""`
- AND `LGB_AUTH_JWT_SECRET` is not set
- WHEN `lgb server` is invoked
- THEN the process exits with code 1
- AND stderr contains `auth.jwtSecret is required`

---

### [MVP-FND-3.2] Secrets MUST NOT appear in logs

The logger MUST NOT print any value of a secret field. The config struct MUST implement `fmt.Stringer` or a `Redacted()` method that replaces secret field values with `"[redacted]"`. Any `slog` log call that logs the config MUST use the redacted form.

#### Scenario: Config log output does not leak secrets

- GIVEN `auth.jwtSecret` is set to `"my-secret"`
- WHEN the loaded config is logged at DEBUG level
- THEN the log output does not contain `"my-secret"`
- AND it contains `"[redacted]"` in place of the secret
