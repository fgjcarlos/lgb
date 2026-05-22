---
change: mvp-foundation
domain: logging
phase: spec
date: 2026-05-22
status: draft
---

# Logging Specification

## Purpose

Structured logger setup for the LGB gateway using Go's stdlib `log/slog`. Defines level selection, format selection (text/JSON), required structured fields, and hygiene rules for all log sites.

## Requirements

### [MVP-FND-4.1] Logger uses `log/slog` stdlib

The logging package MUST be implemented using `log/slog` from the Go standard library (Go 1.24+). It MUST NOT introduce an external logging dependency. It MUST be pure-Go and `CGO_ENABLED=0` safe.

#### Scenario: Logger initialises without external dependency

- GIVEN the `internal/log` package is built with `CGO_ENABLED=0`
- WHEN `go build ./internal/log/...` is run
- THEN the build succeeds without CGo or external logging libraries in the dependency graph

---

### [MVP-FND-4.2] Configurable log level

The logger MUST support four levels: `debug`, `info`, `warn`, `error`. The level MUST be read from `gateway.logLevel` in the loaded config (defaulting to `"info"`). It MUST also be overridable via `--log-level` CLI flag, which takes precedence over config. Invalid level strings MUST cause a startup error.

#### Scenario: Log level from config

- GIVEN config `gateway.logLevel: "debug"`
- WHEN the logger is initialised
- THEN DEBUG-level messages are emitted

#### Scenario: Log level from CLI flag overrides config

- GIVEN config `gateway.logLevel: "info"` and CLI flag `--log-level=warn`
- WHEN the logger is initialised
- THEN only WARN and ERROR messages are emitted; INFO messages are suppressed

#### Scenario: Invalid log level causes startup error

- GIVEN CLI flag `--log-level=verbose`
- WHEN the binary starts
- THEN the process exits with code 1
- AND stderr contains a message about the invalid level

---

### [MVP-FND-4.3] Configurable log format

The logger MUST support two formats: `text` (human-readable key=value) and `json` (structured JSON object per line). The format MUST be read from `gateway.logFormat` in config (defaulting to `"text"`). It MUST also be overridable via `--log-format` CLI flag. Invalid format strings MUST cause a startup error.

Platform note: `text` format uses `slog.NewTextHandler`; `json` format uses `slog.NewJSONHandler`. Both are stdlib. Output MUST go to `os.Stderr` by default.

#### Scenario: JSON format produces valid JSON lines

- GIVEN `gateway.logFormat: "json"`
- WHEN the logger emits a message
- THEN each log line is a valid JSON object parseable with `encoding/json`

#### Scenario: Text format produces key=value lines

- GIVEN `gateway.logFormat: "text"`
- WHEN the logger emits a message
- THEN each log line is human-readable `time=… level=… msg=… key=value…` format

---

### [MVP-FND-4.4] Required structured fields

Every log site MUST include a `component` field identifying the package or subsystem emitting the log. Log sites handling a request or operation MUST include a `op` field identifying the operation name. When a request_id is available (future HTTP middleware), the `request_id` field SHOULD be included.

| Field | Type | Required | Example |
|-------|------|----------|---------|
| `component` | string | MUST | `"config"`, `"server"`, `"doctor"` |
| `op` | string | SHOULD | `"load"`, `"validate"`, `"shutdown"` |
| `request_id` | string | SHOULD (when available) | `"req-abc123"` |

#### Scenario: Log entry includes component field

- GIVEN the config loader logs a message
- WHEN the log output is inspected
- THEN the entry contains `component="config"` (text) or `"component":"config"` (JSON)

---

### [MVP-FND-4.5] Logger MUST NOT log secrets

The logger MUST NOT log any value designated as a secret field (see §MVP-FND-3.2). This is a hygiene requirement — callers MUST use the `Redacted()` representation when logging config objects. The `internal/log` package SHOULD provide a helper or document this expectation explicitly.

#### Scenario: Secret field not in log output

- GIVEN `auth.jwtSecret` is configured
- WHEN any component logs the config struct
- THEN the raw secret value does not appear in any log line

---

### [MVP-FND-4.6] Logger is initialised once and passed via context or explicit argument

The logger MUST NOT use a global variable that is mutated after program start. It SHOULD be initialised in `main.go` after config is loaded and passed to subsystems explicitly (or via `context.WithValue` if the design chooses). Package-level `slog.SetDefault` is acceptable for Phase 0 if an explicit logger is not threaded through all call sites yet, but this SHOULD be refactored in later phases.

#### Scenario: Logger level change does not race

- GIVEN the logger is initialised once at startup
- WHEN concurrent goroutines log messages
- THEN the Go race detector reports no data races on the logger
