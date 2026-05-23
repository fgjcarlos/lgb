---
change: mvp-foundation
domain: doctor
phase: spec
date: 2026-05-22
status: draft
---

# Doctor Specification

## Purpose

Diagnostic check system for the LGB gateway (`lgb doctor`). Defines the check registry, result types, check categories, exact exit codes, and output formats (human-readable and JSON).

## Requirements

### [MVP-FND-8.1] Check registry and result type

The `internal/doctor` package MUST define a `Check` interface and a `Result` type. The `Result` MUST carry at minimum: `Name string`, `Status CheckStatus`, `Message string`. `CheckStatus` MUST be an enum with values `Info`, `Warn`, `Fail`. The registry MUST allow checks to be registered and iterated.

#### Scenario: Check registry executes all registered checks

- GIVEN 3 checks are registered
- WHEN `doctor.Run(ctx)` is called
- THEN 3 `Result` values are returned, one per check

---

### [MVP-FND-8.2] Phase 0 checks

For Phase 0, `lgb doctor` MUST run the following checks:

| Check name | Pass condition | Status on failure |
|------------|---------------|-------------------|
| `data-dir-writable` | Data directory exists and is writable | FAIL |
| `restic-on-path` | `restic` binary found on `$PATH` | WARN (not FAIL — backup may be unused) |
| `go-runtime-version` | `runtime.Version()` returns Go 1.24+ | INFO (informational only) |
| `http-port-available` | `server.httpAddr` port is not already bound | FAIL |

Platform note: the TCP probe for `http-port-available` MUST use `net.Listen("tcp", addr)` followed by immediate close. On Windows, the same `net.Listen` API is available without platform-specific code.

#### Scenario: restic not on PATH is WARN not FAIL

- GIVEN `restic` is not in `$PATH`
- WHEN `lgb doctor` runs
- THEN the `restic-on-path` result has status `Warn`
- AND the exit code is 0 (no FAIL-level checks)

#### Scenario: Data dir not writable is FAIL

- GIVEN the data directory is not writable by the running user
- WHEN `lgb doctor` runs
- THEN `data-dir-writable` result has status `Fail`
- AND exit code is 1

#### Scenario: Port already in use is FAIL

- GIVEN another process is already bound to `server.httpAddr`
- WHEN `lgb doctor` runs
- THEN `http-port-available` result has status `Fail`
- AND exit code is 1

---

### [MVP-FND-8.3] Exit codes

The `lgb doctor` command MUST use the following exit code rules:

| Condition | Exit code |
|-----------|-----------|
| All checks are Info or pass | 0 |
| At least one Warn, no Fail | 0 |
| At least one Fail | 1 |
| Unexpected internal error | 2 |

The worst check status determines the exit code. WARN does NOT change the exit code from 0.

#### Scenario: Only warn results exits 0

- GIVEN doctor runs and all checks pass except `restic-on-path` (Warn)
- WHEN `lgb doctor` returns
- THEN exit code is 0

---

### [MVP-FND-8.4] Human-readable output format

Default (no `--json`) output MUST print one line per check in the format:

```
[PASS] data-dir-writable: /var/lib/lgb is writable
[WARN] restic-on-path: restic not found on $PATH — backup checks unavailable
[FAIL] http-port-available: :8080 is already in use
```

#### Scenario: Human output contains check names and messages

- GIVEN doctor runs with two results: PASS and WARN
- WHEN output is printed without `--json`
- THEN stdout contains `[PASS]` and `[WARN]` prefixes with check names and messages

---

### [MVP-FND-8.5] JSON output format

When `--json` is passed, `lgb doctor` MUST emit a JSON object:

```json
{
  "checks": [
    {"name":"data-dir-writable","status":"pass","message":"…"},
    {"name":"restic-on-path","status":"warn","message":"…"}
  ],
  "overall": "warn"
}
```

The `overall` field MUST be `"pass"`, `"warn"`, or `"fail"` based on the exit code logic in §MVP-FND-8.3.

#### Scenario: JSON output is valid and complete

- GIVEN doctor runs with `--json`
- WHEN the output is parsed
- THEN it is valid JSON with a `checks` array and an `overall` field
- AND `overall` reflects the worst check status

---

### [MVP-FND-8.6] Checks run in parallel

Doctor checks MUST be run concurrently (e.g. via goroutines with a `sync.WaitGroup` or `errgroup`). Individual check panics MUST be recovered and converted to a FAIL result with the panic message. A single panicking check MUST NOT prevent other checks from running.

#### Scenario: Panicking check does not crash doctor

- GIVEN a check that panics
- WHEN `lgb doctor` runs
- THEN the panicking check result has status `Fail` with the panic message
- AND all other checks complete normally
