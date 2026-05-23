---
change: plc-driver
phase: spec
domain: config
date: 2026-05-23
status: draft
type: delta
---

# Config Domain Delta Specification — plc-driver

## Purpose

Extends the `PLC` struct in `internal/config/config.go` with the fields required by the PLC driver. Adds validation rules for the new fields. All additions are additive and backward-compatible — existing YAML files that omit new fields receive safe defaults.

Base spec: `openspec/specs/config/spec.md` (mvp-foundation). All requirements in the base spec remain in force; this document adds only the delta.

---

## Requirements

### [PLC-CFG-1.1] Extended PLC struct fields

The `PLC` struct in `internal/config/config.go` MUST be extended to include the following fields in addition to the existing `Name` and `Address` fields:

| Field (Go) | YAML key | Type | Default | Description |
|------------|----------|------|---------|-------------|
| `Slot` | `slot` | `int` | `0` | CIP backplane slot number (ControlLogix: 0–15) |
| `SocketTimeout` | `socketTimeout` | `string` | `"5s"` | Per-operation deadline; valid Go duration string |
| `ScanRate` | `scanRate` | `string` | `"1s"` | Tag scan interval; valid Go duration string |
| `KeepAlive` | `keepAlive` | `bool` | `true` | Enable TCP keep-alive on the CIP connection |
| `Path` | `path` | `string` | `""` | Optional CIP path override (e.g. `"1,0"`); empty means use gologix default |

All new fields MUST use camelCase YAML keys consistent with the project's case-preservation convention (MVP-FND-2.1).

#### Scenario: New fields default when absent from YAML

- GIVEN a YAML config with a PLC entry containing only `name` and `address`
- WHEN the loader parses the config
- THEN `PLC.Slot` is `0`
- AND `PLC.SocketTimeout` is `"5s"` (or resolved via `time.ParseDuration` to `5 * time.Second`)
- AND `PLC.ScanRate` is `"1s"`
- AND `PLC.KeepAlive` is `true`
- AND `PLC.Path` is `""`

#### Scenario: Explicit values override defaults

- GIVEN a YAML config with `slot: 2`, `socketTimeout: "10s"`, `scanRate: "500ms"`, `keepAlive: false`, `path: "1,0"`
- WHEN the loader parses the config
- THEN all five fields reflect the explicit values

---

### [PLC-CFG-1.2] Validation — address is required

Each entry in `plcs[]` MUST have a non-empty `address`. The existing `Validate()` function MUST be extended to check this. The violation MUST be added to the `errors.Join` aggregate and wrap `ErrConfigInvalid`.

#### Scenario: PLC entry with empty address fails validation

- GIVEN a config with a PLC entry where `address` is `""`
- WHEN `Validate()` is called
- THEN the returned error is non-nil
- AND `errors.Is(err, ErrConfigInvalid)` returns `true`
- AND the error message contains `"plcs[N].address: must not be empty"`

---

### [PLC-CFG-1.3] Validation — socketTimeout is a valid duration

If `socketTimeout` is non-empty, it MUST parse with `time.ParseDuration`. Violation wraps `ErrConfigInvalid`.

Valid range: MUST be > 0. Zero or negative values MUST be rejected.

#### Scenario: Invalid socketTimeout fails validation

- GIVEN a PLC config with `socketTimeout: "not-a-duration"`
- WHEN `Validate()` is called
- THEN the returned error wraps `ErrConfigInvalid`
- AND the message contains `"socketTimeout"`

#### Scenario: Non-positive socketTimeout fails validation

- GIVEN a PLC config with `socketTimeout: "-1s"`
- WHEN `Validate()` is called
- THEN the returned error wraps `ErrConfigInvalid`
- AND the message contains `"must be positive"`

---

### [PLC-CFG-1.4] Validation — scanRate is a valid duration

If `scanRate` is non-empty, it MUST parse with `time.ParseDuration` and MUST be > 0. Violation wraps `ErrConfigInvalid`.

#### Scenario: Invalid scanRate fails validation

- GIVEN a PLC config with `scanRate: "0s"`
- WHEN `Validate()` is called
- THEN the returned error wraps `ErrConfigInvalid`
- AND the message contains `"scanRate: must be positive"`

---

### [PLC-CFG-1.5] Validation — slot is within CIP range

`slot` MUST be in the range 0–15 inclusive. Violation wraps `ErrConfigInvalid`.

#### Scenario: Slot out of range fails validation

- GIVEN a PLC config with `slot: 16`
- WHEN `Validate()` is called
- THEN the returned error wraps `ErrConfigInvalid`
- AND the message contains `"slot: must be between 0 and 15"`

---

### [PLC-CFG-1.6] Validation — all PLC violations aggregated

Violations across multiple PLC entries and across multiple fields within a single entry MUST all be included in the single `errors.Join` aggregate returned by `Validate()`. No PLC entry check MUST short-circuit validation of subsequent entries.

#### Scenario: Two PLC entries with two violations each — all four reported

- GIVEN a config with two PLC entries, each missing `address` and having an invalid `socketTimeout`
- WHEN `Validate()` is called
- THEN the returned error contains four violation messages
- AND `errors.Is(err, ErrConfigInvalid)` returns `true`

---

### [PLC-CFG-1.7] Backward compatibility

Existing YAML configs that define `plcs[]` entries with only `name` and `address` MUST load and validate without error. No existing field is removed or renamed.

#### Scenario: Legacy PLC config loads without error

- GIVEN a YAML file with `plcs: [{name: "plc1", address: "192.168.1.10"}]`
- WHEN `Load(path)` is called
- THEN it returns a non-nil `*Config` with no error
- AND `cfg.PLCs[0].ScanRate` is `"1s"` (default)
