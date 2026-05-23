---
change: mvp-foundation
domain: errors
phase: spec
date: 2026-05-22
status: draft
---

# Errors Specification

## Purpose

Project-wide error model for LGB. Defines sentinel errors per package, wrapping convention, boundary translation rules, panic policy, and multi-error aggregation.

## Requirements

### [MVP-FND-5.1] Sentinel errors per package

Every `internal/` package that produces domain-specific errors MUST export named sentinel error values using `errors.New`. Callers MUST compare errors using `errors.Is`, never by string comparison. Sentinel names MUST follow the pattern `Err{Domain}{Condition}`.

| Package | Sentinel | Meaning |
|---------|----------|---------|
| `internal/config` | `ErrConfigInvalid` | Schema or constraint violation |
| `internal/config` | `ErrConfigMissing` | Config file not found |
| `internal/config` | `ErrConfigPermission` | File not readable |
| `internal/datadir` | `ErrDataDirInvalid` | Path exists but is not a directory |
| `internal/datadir` | `ErrDataDirPermission` | Path not writable by running user |
| `internal/doctor` | `ErrCheckFailed` | A doctor check returned FAIL status |
| `internal/retry` | `ErrMaxAttempts` | Retry exhausted max attempts |

Future packages MUST add sentinels to this list in their own specs before apply.

#### Scenario: Sentinel error identified with errors.Is

- GIVEN `internal/config` returns a wrapped `ErrConfigInvalid`
- WHEN a caller checks `errors.Is(err, config.ErrConfigInvalid)`
- THEN it returns `true`
- AND `errors.Is(err, config.ErrConfigMissing)` returns `false`

---

### [MVP-FND-5.2] Error wrapping with `%w`

Every non-trivial error MUST be wrapped with `fmt.Errorf("context: %w", err)` to preserve the chain. Raw errors MUST NOT be returned without context unless the error message is already self-describing. The wrapping message MUST be in lowercase, without a trailing period.

#### Scenario: Error chain is preserved

- GIVEN the config loader wraps an OS error: `fmt.Errorf("reading config: %w", ioErr)`
- WHEN a caller uses `errors.As(err, &pathError)`
- THEN `pathError` is populated from the original OS error

---

### [MVP-FND-5.3] `errors.Join` for multiple validation failures

When multiple validation failures must be reported at once (e.g. config validation), the code MUST use `errors.Join` (Go 1.20+) to combine all errors before returning. The joined error MUST satisfy `errors.Is` for each constituent sentinel.

#### Scenario: Multiple config errors joined and reported

- GIVEN a config with two violations
- WHEN `Validate()` runs
- THEN the returned error contains both messages
- AND `errors.Is(err, ErrConfigInvalid)` returns true for the joined error

---

### [MVP-FND-5.4] No panics in library code

`internal/` packages MUST NOT call `panic()` for runtime or user-error conditions. Panics are reserved for programmer errors (e.g. nil pointer dereference on a mandatory dependency). All error conditions MUST be returned as `error` values. `main.go` MAY convert a top-level unrecoverable error to `os.Exit(1)` after logging.

#### Scenario: Invalid input returns error, not panic

- GIVEN `internal/config.Load("")` is called with an empty path
- WHEN the function executes
- THEN it returns a non-nil error
- AND it does NOT panic (test runs without recovery)

---

### [MVP-FND-5.5] Typed errors at API boundaries

CLI commands and HTTP handlers MUST translate internal errors into structured outputs. CLI commands MUST map error types to exit codes. HTTP handlers (when implemented in future phases) MUST map error types to HTTP status codes. The mapping MUST be defined in the `cmd/` layer, not inside `internal/` packages.

| Condition | CLI exit code | HTTP status (future) |
|-----------|--------------|----------------------|
| Success | 0 | 200 |
| User/validation error | 1 | 400 |
| Not found | 1 | 404 |
| Permission/auth error | 1 | 403/401 |
| Internal/unexpected | 2 | 500 |

#### Scenario: Validation error maps to exit code 1

- GIVEN `lgb config validate` encounters a schema violation
- WHEN the subcommand returns
- THEN exit code is 1
- AND the error message is human-readable (not a raw Go error string)
