---
change: plc-driver
phase: spec
domain: errors
date: 2026-05-23
status: draft
type: delta
---

# Errors Domain Delta Specification — plc-driver

## Purpose

Adds four PLC-domain sentinel errors to `internal/errors/errors.go`. All additions are purely additive. The base sentinel convention (MVP-FND-5.1) remains in force: `Err{Domain}{Condition}`, compared with `errors.Is`, never by string match.

Base spec: `openspec/specs/errors/spec.md` (mvp-foundation).

---

## Requirements

### [PLC-ERR-1.1] New PLC-domain sentinels

The following sentinels MUST be added to `internal/errors/errors.go` in a clearly labeled `// PLC-domain sentinels (PLC-DRV-1.*)` block:

| Sentinel | `errors.New` message | Meaning |
|----------|---------------------|---------|
| `ErrPLCConnect` | `"plc connect failed"` | Returned when the driver cannot establish a CIP session |
| `ErrPLCRead` | `"plc read failed"` | Returned when a tag read operation fails |
| `ErrPLCWrite` | `"plc write failed"` | Returned when a tag write operation fails |
| `ErrPLCTimeout` | `"plc operation timeout"` | Returned when an operation exceeds `SocketTimeout` |

All four MUST be exported `var` declarations using `errors.New`, consistent with the existing pattern in the file.

#### Scenario: Each sentinel is identifiable with errors.Is

- GIVEN `internal/plc` wraps `ErrPLCRead` via `fmt.Errorf("read SimInt: %w: %w", ErrPLCRead, underlying)`
- WHEN a caller checks `errors.Is(err, ErrPLCRead)`
- THEN it returns `true`
- AND `errors.Is(err, ErrPLCConnect)` returns `false`

#### Scenario: ErrPLCTimeout is distinct from ErrPLCRead

- GIVEN an operation exceeds `SocketTimeout` and returns an error wrapping `ErrPLCTimeout`
- WHEN a caller checks `errors.Is(err, ErrPLCTimeout)`
- THEN it returns `true`
- AND `errors.Is(err, ErrPLCRead)` returns `false`

---

### [PLC-ERR-1.2] Re-exports in internal/plc

The `internal/plc` package MUST re-export all four sentinels for ergonomic local use, following the pattern established in `internal/config/config.go`:

```go
var (
    ErrPLCConnect = errs.ErrPLCConnect
    ErrPLCRead    = errs.ErrPLCRead
    ErrPLCWrite   = errs.ErrPLCWrite
    ErrPLCTimeout = errs.ErrPLCTimeout
)
```

Callers of `internal/plc` MUST be able to use `plc.ErrPLCRead` without importing `internal/errors` directly.

#### Scenario: Caller uses plc package sentinel directly

- GIVEN a caller imports only `internal/plc`
- WHEN it checks `errors.Is(err, plc.ErrPLCRead)`
- THEN it returns the same result as `errors.Is(err, errs.ErrPLCRead)`

---

### [PLC-ERR-1.3] Error wrapping convention

All errors produced by `internal/plc` MUST use `fmt.Errorf` with `%w` verbs to preserve the chain (MVP-FND-5.2). The wrapping template MUST follow:

```
"<operation> <tag>: %w: %w", <plcSentinel>, <underlyingErr>
```

Examples:

- `fmt.Errorf("plc connect %s: %w: %w", addr, ErrPLCConnect, underlying)`
- `fmt.Errorf("read %s: %w: %w", tag, ErrPLCRead, cipErr)`
- `fmt.Errorf("write %s: %w: %w", tag, ErrPLCWrite, cipErr)`
- `fmt.Errorf("read %s: %w: %w", tag, ErrPLCTimeout, timeoutErr)`

Messages MUST be lowercase with no trailing period (MVP-FND-5.2).

#### Scenario: Error chain preserves both sentinel and underlying cause

- GIVEN `WriteTag` receives a `*gologix.CIPError` from the library
- WHEN the adapter wraps it as `fmt.Errorf("write SimFloat: %w: %w", ErrPLCWrite, cipErr)`
- THEN `errors.Is(err, ErrPLCWrite)` returns `true`
- AND `errors.As(err, &cipErr)` populates `cipErr` from the chain

---

### [PLC-ERR-1.4] Sentinel table updated in base spec

The sentinel table in `openspec/specs/errors/spec.md` (MVP-FND-5.1) MUST be updated to include the four new sentinels after this change is applied. The update MUST be done as part of the `sdd-archive` phase, not during apply.

This requirement is informational for the spec phase — it creates no code obligation at apply time beyond the sentinels themselves.

---

### [PLC-ERR-1.5] No panics on CIP errors

`internal/plc` MUST NOT panic on any error returned by gologix, including `*gologix.CIPError`, nil pointer dereferences on optional fields, or unexpected types. All gologix error paths MUST be caught and translated to the appropriate sentinel (MVP-FND-5.4).

#### Scenario: Unknown gologix error type does not panic

- GIVEN gologix returns an error type not covered by the type switch
- WHEN the adapter processes it
- THEN it returns a non-nil error wrapping `ErrPLCRead` or `ErrPLCWrite`
- AND the test runs without recovery (no panic)
