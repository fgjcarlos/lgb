---
change: write-acl
phase: spec
domain: plc
date: 2026-05-31
status: draft
type: delta
---

# Delta for PLC

## MODIFIED Requirements

### [PCS-CFG-5.1] `writable` field on TagDef — enforced master switch

The `TagDef` struct in `internal/config/config.go` MUST include a `Writable` field:

| Field (Go) | YAML key | Type | Default | Description |
|------------|----------|------|---------|-------------|
| `Writable` | `writable` | `bool` | `false` | Engineering master switch for writes; `false` means NO write for ANY role, including admin |

`Writable` MUST be persisted in `plc_tags.writable` (integer 0/1) and round-tripped through the `/api/plcs` JSON shape. `Writable=false` is an absolute engineering-level prohibition: no access-control rule, no role, and no API call can override it. The `AuthorizeHTTP` and `AuthorizeDCMD` enforcement functions (see TWA-ENFORCE-2.1) MUST each consult this field as Layer 1 before any further gate. `Writable` is excluded from `ValidatePLC` (presence/absence is always valid).

(Previously: stored and round-tripped but NOT enforced — no access-control logic depended on it.)

#### Scenario: writable field loads from YAML

- GIVEN a YAML config with a tag entry containing `writable: true`
- WHEN `Load(path)` is called
- THEN `cfg.PLCs[0].Tags[0].Writable` is `true`

#### Scenario: writable defaults to false when absent

- GIVEN a YAML config with a tag entry that omits `writable`
- WHEN `Load(path)` is called
- THEN `cfg.PLCs[0].Tags[0].Writable` is `false`

#### Scenario: writable field does not affect config validation

- GIVEN a tag with `writable: true` and all other fields valid
- WHEN `ValidatePLC` (or `Validate()`) is called
- THEN it returns nil (writable has no validation constraint at config parse time)

#### Scenario: Writable=false blocks write regardless of ACL or role

- GIVEN tag `Emergency.Stop` has `Writable=false` in the plc_tags store
- AND a valid ACL rule grants `admin` write on `Emergency.Stop`
- WHEN any write attempt is made via HTTP or DCMD for any role including `admin`
- THEN the write is DENIED before the ACL is consulted
- AND the audit event records `reason="tag not writable"`

#### Scenario: Writable=true permits the ACL to be consulted (HTTP)

- GIVEN tag `Feed.Rate` has `Writable=true` in the plc_tags store
- WHEN an HTTP write attempt is made
- THEN the enforcement core proceeds to Layer 2 (ACL lookup)
- AND the final allow/deny outcome is determined by the ACL, not by the master switch

---

### [PCS-CFG-5.2] `dcmd_enabled` field on TagDef — per-tag DCMD opt-in

The `TagDef` struct in `internal/config/config.go` MUST include a `DCMDEnabled` field:

| Field (Go) | YAML key | Type | Default | Description |
|------------|----------|------|---------|-------------|
| `DCMDEnabled` | `dcmd_enabled` | `bool` | `false` | Engineering per-tag opt-in for Sparkplug DCMD writes; `false` means DCMD writes to this tag are DROPPED regardless of any ACL rule |

`DCMDEnabled` MUST be persisted in `plc_tags.dcmd_enabled` (integer 0/1) and round-tripped through the `/api/plcs` JSON shape. It is an engineering-set safety switch, set alongside `Writable`. Its default is `false` (DCMD off by default — a tag must be explicitly opted in). `DCMDEnabled=false` is an absolute prohibition for the DCMD path: no ACL rule, no role, and no MQTT credential overrides it. The `AuthorizeDCMD` enforcement function (see TWA-ENFORCE-2.1, TWA-DCMD-3.2) MUST consult this field as Layer 2 after the `Writable` master switch. `DCMDEnabled` is excluded from `ValidatePLC` (presence/absence is always valid). The `dcmd_enabled` flag is entirely independent of the HTTP role×tag ACL — the two systems share only the `Writable` master switch.

#### Scenario: dcmd_enabled field loads from YAML

- GIVEN a YAML config with a tag entry containing `dcmd_enabled: true`
- WHEN `Load(path)` is called
- THEN `cfg.PLCs[0].Tags[0].DCMDEnabled` is `true`

#### Scenario: dcmd_enabled defaults to false when absent

- GIVEN a YAML config with a tag entry that omits `dcmd_enabled`
- WHEN `Load(path)` is called
- THEN `cfg.PLCs[0].Tags[0].DCMDEnabled` is `false`

#### Scenario: dcmd_enabled is persisted and round-tripped

- GIVEN a tag is created or updated via `POST /api/plcs/{plc}/tags` with `dcmd_enabled: true`
- WHEN the tag is retrieved via `GET /api/plcs/{plc}/tags/{tag}`
- THEN the response JSON contains `"dcmd_enabled": true`
- AND `plc_tags.dcmd_enabled` in the SQLite store is `1`

#### Scenario: dcmd_enabled field does not affect config validation

- GIVEN a tag with `dcmd_enabled: true` and all other fields valid
- WHEN `ValidatePLC` (or `Validate()`) is called
- THEN it returns nil (dcmd_enabled has no validation constraint at config parse time)

#### Scenario: dcmd_enabled=false blocks DCMD regardless of ACL

- GIVEN tag `Feed.Rate` has `Writable=true` and `DCMDEnabled=false` in the plc_tags store
- AND a valid ACL rule grants `operator` write on `Feed.Rate` (HTTP access permitted)
- WHEN a DCMD metric for `Feed.Rate` is received
- THEN the write is DROPPED before the ACL is consulted
- AND the audit event records `reason="dcmd not enabled"` and `source="dcmd"`

#### Scenario: Both Writable=true and dcmd_enabled=true required for DCMD writes

- GIVEN tag `Setpoint.Temp` has `Writable=true` and `DCMDEnabled=true`
- WHEN a DCMD metric for `Setpoint.Temp` is received
- THEN `AuthorizeDCMD` returns `Allowed=true`
- AND `Driver.WriteTag` is called
