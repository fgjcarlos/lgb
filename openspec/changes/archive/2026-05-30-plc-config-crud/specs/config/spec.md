---
change: plc-config-crud
phase: spec
domain: config
date: 2026-05-29
status: draft
type: delta
---

# Delta for Config

## ADDED Requirements

### [PCS-CFG-5.1] `writable` field on TagDef

The `TagDef` struct in `internal/config/config.go` MUST be extended with a `Writable` field:

| Field (Go) | YAML key | Type | Default | Description |
|------------|----------|------|---------|-------------|
| `Writable` | `writable` | `bool` | `false` | Forward-compat write permission marker; stored but NOT enforced in this change |

`Writable` MUST be persisted in `plc_tags.writable` (integer 0/1). No access control logic depends on this field in this change — it is stored only.

#### Scenario: writable field loads from YAML

- GIVEN a YAML config with a tag entry containing `writable: true`
- WHEN `Load(path)` is called
- THEN `cfg.PLCs[0].Tags[0].Writable` is `true`

#### Scenario: writable defaults to false when absent

- GIVEN a YAML config with a tag entry that omits `writable`
- WHEN `Load(path)` is called
- THEN `cfg.PLCs[0].Tags[0].Writable` is `false`

#### Scenario: writable field does not affect validation

- GIVEN a tag with `writable: true` and all other fields valid
- WHEN `Validate()` is called
- THEN it returns nil (writable has no validation constraint)

---

## MODIFIED Requirements

### [PLC-CFG-1.1] Extended PLC struct fields

The `PLC` struct in `internal/config/config.go` MUST include the following fields in addition to the existing `Name` and `Address` fields:

| Field (Go) | YAML key | Type | Default | Description |
|------------|----------|------|---------|-------------|
| `Slot` | `slot` | `int` | `0` | CIP backplane slot number (ControlLogix: 0–15) |
| `SocketTimeout` | `socketTimeout` | `string` | `"5s"` | Per-operation deadline; valid Go duration string |
| `ScanRate` | `scanRate` | `string` | `"1s"` | Tag scan interval; valid Go duration string |
| `KeepAlive` | `keepAlive` | `bool` | `true` | Enable TCP keep-alive on the CIP connection |
| `Path` | `path` | `string` | `""` | Optional CIP path override (e.g. `"1,0"`); empty means use gologix default |

All new fields MUST use camelCase YAML keys consistent with the project's case-preservation convention (MVP-FND-2.1).

YAML `plcs[]` entries are a BOOTSTRAP SEED only: they are read once when the `plcstore` is empty and ignored thereafter. The runtime source of truth for PLC definitions is `plcstore.Store`. YAML `plcs[]` MUST NOT be written back by any code path.

(Previously: YAML `plcs[]` was the authoritative runtime config. This change reclassifies it as a one-time bootstrap seed.)

#### Scenario: Existing config without optional fields loads successfully

- GIVEN a YAML config with PLC entries having only `name` and `address`
- WHEN `Load(path)` is called
- THEN it returns without error and defaults apply for omitted fields

#### Scenario: YAML plcs ignored after store is seeded

- GIVEN the store already contains PLCs
- AND the YAML `plcs[]` section differs from the store contents
- WHEN the gateway starts (or the file watcher fires)
- THEN the non-PLC YAML fields are applied (gateway, server, etc.)
- AND the PLC set is sourced from the store, not from YAML

#### Scenario: Non-PLC YAML reload still applies

- GIVEN the store has PLCs and the gateway is running
- WHEN the YAML file is modified (e.g. `gateway.logLevel` changes)
- THEN the watcher fires and the non-PLC fields are updated
- AND the PLC list used by the Manager comes from the store

---

### [PLC-CFG-1.7] Backward compatibility

Existing YAML configs that define `plcs[]` entries with only `name` and `address` MUST load and validate without error. No existing field is removed or renamed. The new `Tags[].Writable` field is optional — absent entries are valid and default to `false`. The YAML `plcs[]` section is treated as a seed-only input; presence of `plcs[]` in YAML does not cause an error and does not override the store after first boot.

(Previously: Tags field did not have a `writable` sub-field; YAML `plcs[]` was always the runtime source.)

#### Scenario: Config with only name and address is valid

- GIVEN a PLC entry with only `name` and `address` populated
- WHEN `Validate()` is called
- THEN it returns nil for that entry

#### Scenario: Config without writable field is valid

- GIVEN a tag entry in YAML that has no `writable` key
- WHEN `Load(path)` is called and `Validate()` is run
- THEN no error is returned for the missing field
