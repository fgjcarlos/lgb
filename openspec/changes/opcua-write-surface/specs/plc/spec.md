---
change: opcua-write-surface
phase: spec
domain: plc
date: 2026-11-10
status: draft
type: delta
---

# Delta for PLC

## ADDED Requirements

### [PCS-CFG-5.3] `opcua_write_enabled` field on TagDef — per-tag OPC UA opt-in

`config.TagDef` MUST expose a per-tag opt-in for OPC UA writes.

| Field | YAML / API key | Type | Default | Description |
|-------|----------------|------|---------|-------------|
| `OPCUAEnabled` | `opcua_write_enabled` | `bool` | `false` | Engineering per-tag opt-in for OPC UA writes; `false` means OPC UA writes to this tag are DENIED regardless of any ACL rule |

`OPCUAEnabled` MUST be persisted in `plc_tags.opcua_write_enabled` (integer 0/1) and round-tripped through the
`/api/plcs` JSON shape as `opcua_write_enabled`. It is an engineering-set safety switch, set alongside `Writable`.
Its default is `false` (OPC UA writes off by default — a tag must be explicitly opted in). The column MUST be added
by the same idempotent `ALTER TABLE` migration pattern used for `dcmd_enabled`, so an existing database gains the
column with the default `0` and no stored row is rewritten. `OPCUAEnabled=false` is an absolute prohibition for the
OPC UA path: no ACL rule, no role, and no OPC UA session override it. The `AuthorizeOPCUA` enforcement function MUST
consult this field as Layer 2 after the `Writable` master switch. `OPCUAEnabled` is excluded from `ValidatePLC`
(presence/absence is always valid).

Note on koanf tag: the field uses `koanf:"opcua_write_enabled"` (snake_case), matching the YAML key and all other
koanf tags in `config.go`.

#### Scenario: opcua_write_enabled field loads from YAML

- GIVEN a YAML config with a tag entry containing `opcua_write_enabled: true`
- WHEN the config is loaded
- THEN `cfg.PLCs[0].Tags[0].OPCUAEnabled` is `true`

#### Scenario: opcua_write_enabled defaults to false when absent

- GIVEN a YAML config with a tag entry that omits `opcua_write_enabled`
- WHEN the config is loaded
- THEN `cfg.PLCs[0].Tags[0].OPCUAEnabled` is `false`

#### Scenario: opcua_write_enabled is persisted and round-tripped

- GIVEN a tag is created or updated via `POST /api/plcs/{plc}/tags` with `opcua_write_enabled: true`
- WHEN the call succeeds
- THEN the response JSON contains `"opcua_write_enabled": true`
- AND `plc_tags.opcua_write_enabled` in the SQLite store is `1`

#### Scenario: opcua_write_enabled field does not affect config validation

- GIVEN a tag with `opcua_write_enabled: true` and all other fields valid
- WHEN `ValidatePLC` is called
- THEN it returns nil (`opcua_write_enabled` has no validation constraint at config parse time)

#### Scenario: opcua_write_enabled=false blocks OPC UA writes regardless of ACL

- GIVEN tag `Feed.Rate` has `Writable=true` and `opcua_write_enabled=false`
- AND an ACL rule grants `operator` write on `Feed.Rate`
- WHEN an OPC UA write arrives for `Feed.Rate`
- THEN the write is denied
- AND the audit event records `reason="opcua not enabled"` and `source="opcua"`

#### Scenario: Both Writable=true and opcua_write_enabled=true are required for OPC UA writes

- GIVEN tag `Feed.Rate` has `Writable=false` and `opcua_write_enabled=true`
- WHEN an OPC UA write arrives for `Feed.Rate`
- THEN the write is denied
- AND the audit event records `reason="tag not writable"` and `source="opcua"`

#### Scenario: A legacy database gains the column without data loss

- GIVEN a `plcs.db` created before `opcua_write_enabled` existed
- WHEN the store is opened
- THEN the migration adds the column
- AND every pre-existing tag has `opcua_write_enabled = 0`
