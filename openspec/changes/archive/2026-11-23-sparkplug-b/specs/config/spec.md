---
change: sparkplug-b
phase: spec
domain: config
date: 2026-05-23
status: draft
type: delta
---

# Delta for Config

## ADDED Requirements

### [SPK-CFG-2.1] Extended MQTTSection — Sparkplug B fields

The `MQTTSection` struct in `internal/config/config.go` MUST be extended with the following fields:

| Field (Go) | YAML key | Type | Default | Description |
|------------|----------|------|---------|-------------|
| `GroupID` | `groupID` | `string` | `""` | Sparkplug B group identifier |
| `EdgeNodeID` | `edgeNodeID` | `string` | `""` | Sparkplug B edge node identifier |
| `QoS` | `qos` | `int` | `1` | MQTT QoS level (0, 1, or 2) |
| `KeepAlive` | `keepAlive` | `string` | `"30s"` | MQTT keep-alive interval (Go duration string) |
| `CleanSession` | `cleanSession` | `bool` | `true` | MQTT clean session flag |

All new fields MUST use camelCase YAML keys consistent with MVP-FND-2.1.

#### Scenario: Sparkplug fields load from YAML

- GIVEN a YAML config with `mqtt.groupID: "plant-a"` and `mqtt.edgeNodeID: "lgb-1"`
- WHEN `Load(path)` is called
- THEN `cfg.MQTT.GroupID` is `"plant-a"`
- AND `cfg.MQTT.EdgeNodeID` is `"lgb-1"`

#### Scenario: QoS defaults to 1 when absent

- GIVEN a YAML config with no `mqtt.qos` field
- WHEN the loader resolves defaults
- THEN `cfg.MQTT.QoS` is `1`

---

### [SPK-CFG-2.2] Validation — GroupID and EdgeNodeID required when MQTT is configured

When `mqtt.brokerURL` is non-empty, `mqtt.groupID` and `mqtt.edgeNodeID` MUST both be non-empty. Violations MUST wrap `ErrConfigInvalid` and be included in the `errors.Join` aggregate from `Validate()`.

#### Scenario: Missing GroupID when broker is set

- GIVEN `mqtt.brokerURL` is non-empty and `mqtt.groupID` is empty
- WHEN `Validate()` is called
- THEN it returns an error wrapping `ErrConfigInvalid`
- AND the error message references `mqtt.groupID`

#### Scenario: No validation error when broker is empty

- GIVEN `mqtt.brokerURL` is empty
- WHEN `Validate()` is called with empty `mqtt.groupID`
- THEN no error is returned for `mqtt.groupID`

---

### [SPK-CFG-2.3] Validation — QoS must be 0, 1, or 2

`mqtt.qos` MUST be in the range [0, 2]. Violation wraps `ErrConfigInvalid`.

#### Scenario: Invalid QoS value

- GIVEN `mqtt.qos: 3` in config
- WHEN `Validate()` is called
- THEN it returns an error wrapping `ErrConfigInvalid`
- AND the error message references `mqtt.qos`

---

### [SPK-CFG-2.4] Validation — KeepAlive is a valid duration

`mqtt.keepAlive` MUST parse with `time.ParseDuration` and MUST be > 0 when non-empty. Violation wraps `ErrConfigInvalid`.

#### Scenario: Invalid keepAlive duration

- GIVEN `mqtt.keepAlive: "not-a-duration"` in config
- WHEN `Validate()` is called
- THEN it returns an error wrapping `ErrConfigInvalid`

---

### [SPK-CFG-2.5] Per-PLC Tags field

The `PLC` struct in `internal/config/config.go` MUST be extended with a `Tags` field:

| Field (Go) | YAML key | Type | Default | Description |
|------------|----------|------|---------|-------------|
| `Tags` | `tags` | `[]TagDef` | `nil` | Ordered list of tags to read and publish |

`TagDef` MUST be a new struct:

| Field (Go) | YAML key | Type | Description |
|------------|----------|------|-------------|
| `Name` | `name` | `string` | PLC tag name (e.g. `"Motor.Speed"`) |
| `Type` | `type` | `string` | Sparkplug B datatype name (e.g. `"Float"`, `"Int32"`) |

#### Scenario: Tags load from YAML

- GIVEN a YAML config with a PLC entry containing two tags
- WHEN `Load(path)` is called
- THEN `cfg.PLCs[0].Tags` has length 2
- AND each `TagDef` has the correct `Name` and `Type`

#### Scenario: Empty Tags slice is valid

- GIVEN a PLC entry with no `tags` field
- WHEN `Load(path)` is called
- THEN `cfg.PLCs[0].Tags` is nil or empty
- AND `Validate()` returns no error for that PLC

---

### [SPK-CFG-2.6] Validation — TagDef name and type are non-empty

For each `TagDef` in each PLC's `Tags` slice, both `name` and `type` MUST be non-empty. Violation wraps `ErrConfigInvalid` and MUST be included in the `errors.Join` aggregate.

#### Scenario: TagDef with empty name fails validation

- GIVEN a PLC tag with `name: ""` and `type: "Int32"`
- WHEN `Validate()` is called
- THEN it returns an error wrapping `ErrConfigInvalid`
- AND the error message references the tag index

---

### [SPK-CFG-2.7] Validation — TagDef type is a known Sparkplug B scalar type

`TagDef.Type` MUST be one of the supported Phase 1 scalar types: `Boolean`, `Int8`, `Int16`, `Int32`, `Int64`, `UInt8`, `UInt16`, `UInt32`, `UInt64`, `Float`, `Double`, `String`. Any other value MUST produce a violation wrapping `ErrConfigInvalid`.

#### Scenario: Unknown tag type fails validation

- GIVEN a PLC tag with `type: "UDT"` (not a Phase 1 scalar)
- WHEN `Validate()` is called
- THEN it returns an error wrapping `ErrConfigInvalid`

---

## MODIFIED Requirements

### [PLC-CFG-1.7] Backward compatibility

Existing YAML configs that define `plcs[]` entries with only `name` and `address` MUST load and validate without error. No existing field is removed or renamed. The new `Tags` field is optional — absent entries are valid and result in an empty scan list for that PLC.
(Previously: Tags field did not exist; backward compat only covered name/address.)

#### Scenario: Existing config without tags loads successfully

- GIVEN a YAML config with PLCs that have no `tags` field
- WHEN `Load(path)` is called
- THEN it returns no error
- AND `Validate()` passes

#### Scenario: Config with only name and address is valid

- GIVEN a PLC entry with only `name` and `address` populated
- WHEN `Validate()` is called
- THEN it returns nil for that entry
