---
change: opcua-write-surface
phase: spec
domain: tag-write-acl
date: 2026-11-10
status: draft
type: delta
---

# Delta for Tag Write ACL

## ADDED Requirements

### [TWA-OPCUA-3.3] OPC UA write surface

The gateway's OPC UA server MUST accept inbound OPC UA `Write` requests on tag nodes and route them through the
shared write guard. **OPC UA writes are OFF by default.** A tag is OPC UA-writable only when BOTH
`tag.Writable == true` AND `tag.OPCUAEnabled == true`. There is NO role lookup, NO actor resolution, and NO ACL
consultation for OPC UA writes: the OPC UA server runs anonymous user tokens, so there is no principal to
authorize — the per-tag `opcua_write_enabled` flag IS the entire OPC UA authorization decision.

The gateway MUST install its own OPC UA `Write` handler on the gopcua server **before** the server starts serving.
The handler MUST NOT mutate the address space: a write either reaches the PLC through the guard or is rejected with
a UA status code. (gopcua's default Write service replaces the node's value callback, which silently breaks the read
path of that tag without ever writing the PLC.)

The handler MUST:

1. Resolve the written node to its `(plc, tag)` pair. An unknown node id MUST be rejected with
   `ua.StatusBadNodeIDUnknown`; a write to an attribute other than `Value` MUST be rejected with
   `ua.StatusBadAttributeIDInvalid`.
2. Convert the written value to the tag's declared type. A value that cannot be converted MUST be rejected with
   `ua.StatusBadTypeMismatch`, audited as a deny, and MUST NOT reach the PLC.
3. Call `AuthorizeOPCUA(ctx, plcName, tagName)` — no actor, no role, no credential lookup.
4. On allow: call `Driver.WriteTag`, emit an audit event with `source="opcua"`, and answer `ua.StatusOK`.
5. On deny: emit an audit event with `outcome="deny"`, `source="opcua"`, and answer `ua.StatusBadUserAccessDenied`.
6. On PLC write error: emit an audit event with `outcome="deny"`, `source="opcua"`, and answer
   `ua.StatusBadInternalError`.

The handler MUST NOT call `aclStore.CanWrite` or `AuthorizeHTTP`. An operator-level HTTP ACL rule granting write on a
tag does NOT enable OPC UA writes to that tag, and `dcmd_enabled` does NOT enable OPC UA writes to that tag.

When the OPC UA server is constructed without a write guard or without a PLC writer, every write attempt MUST be
denied with `ua.StatusBadUserAccessDenied` and audited as a deny. The read-only server MUST keep working.

Each tag node MUST expose an `AccessLevel` attribute: `CurrentRead` when the tag is not OPC UA-writable,
`CurrentRead | CurrentWrite` when it is. The level is derived from the same metadata the guard reads.

#### Scenario: (a) OPC UA write proceeds when both flags are true

- GIVEN tag `Feed.Rate` has `Writable=true` AND `OPCUAEnabled=true`
- WHEN an OPC UA client writes `3.0` to the `Feed.Rate` node
- THEN `Driver.WriteTag("Feed.Rate", 3.0)` is called
- AND the write is answered with `ua.StatusOK`
- AND an audit event with `action="tag.write"`, `outcome="allow"`, `source="opcua"` is recorded
- AND no ACL lookup is performed

#### Scenario: (b) OPC UA write dropped when the opt-in is false — ACL rule does NOT help

- GIVEN tag `Feed.Rate` has `Writable=true` AND `OPCUAEnabled=false`
- AND an ACL rule `(operator, Silo-1, Feed.Rate, allow_write=1)` exists (HTTP operators can write)
- WHEN an OPC UA client writes to the `Feed.Rate` node
- THEN `Driver.WriteTag` is NOT called
- AND the write is answered with `ua.StatusBadUserAccessDenied`
- AND an audit event with `action="tag.write"`, `outcome="deny"`, `source="opcua"`, `reason="opcua not enabled"` is recorded
- AND `aclStore.CanWrite` is NOT called

#### Scenario: (c) OPC UA write dropped when Writable=false — regardless of the opt-in

- GIVEN tag `Emergency.Stop` has `Writable=false` AND `OPCUAEnabled=true`
- WHEN an OPC UA client writes to the `Emergency.Stop` node
- THEN `Driver.WriteTag` is NOT called
- AND the write is answered with `ua.StatusBadUserAccessDenied`
- AND an audit event with `action="tag.write"`, `outcome="deny"`, `source="opcua"`, `reason="tag not writable"` is recorded

#### Scenario: (d) OPC UA deny audit records source=opcua with no actor

- GIVEN any tag with `OPCUAEnabled=false`
- WHEN an OPC UA write is denied
- THEN the audit event contains `source="opcua"`
- AND the audit event does NOT contain an actor username or role field (or records them as empty/absent)

#### Scenario: (e) Unknown node id is rejected

- GIVEN an OPC UA client writes to a node id that is not a tag node
- WHEN the write is processed
- THEN the write is answered with `ua.StatusBadNodeIDUnknown`
- AND `Driver.WriteTag` is NOT called

#### Scenario: (f) A value that does not match the tag type is denied

- GIVEN tag `Feed.Rate` has `Writable=true`, `OPCUAEnabled=true` and type `Float`
- WHEN an OPC UA client writes a non-numeric value to the `Feed.Rate` node
- THEN the write is answered with `ua.StatusBadTypeMismatch`
- AND `Driver.WriteTag` is NOT called
- AND an audit event with `outcome="deny"`, `source="opcua"` is recorded

#### Scenario: (g) A denied write never mutates the read path

- GIVEN tag `Feed.Rate` is OPC UA-writable
- WHEN an OPC UA client writes to the `Feed.Rate` node
- THEN the node keeps serving the live tag value to subsequent reads
- AND the write is applied through the PLC driver, not by replacing the node's value

---

## MODIFIED Requirements

### [TWA-ENFORCE-2.1] Enforce — source-dispatched enforcement core

The enforcement core (`internal/writeguard`) exposes three top-level functions that apply the correct gate for each
write surface.

**HTTP path**: `AuthorizeHTTP(ctx, actor Actor, plcName, tagName string, val any) Decision`. `Actor` carries username
and role. Enforcement:

1. **Layer 1 — master switch**: if `tag.Writable == false`, return `Decision{Allowed: false, Reason: "tag not writable"}`. Applies to every role including admin. No ACL lookup is performed.
2. **Layer 2 — ACL**: call `aclStore.CanWrite(ctx, actor.Role, plcName, tagName)`. If false, return `Decision{Allowed: false, Reason: "acl deny"}`.
3. If both layers pass, return `Decision{Allowed: true}`.

**DCMD path**: `AuthorizeDCMD(ctx, plcName, tagName string) Decision`. There is NO actor, NO role, and NO ACL lookup. Enforcement:

1. **Layer 1 — master switch**: if `tag.Writable == false`, return `Decision{Allowed: false, Reason: "tag not writable"}`.
2. **Layer 2 — DCMD flag**: if `tag.DCMDEnabled == false`, return `Decision{Allowed: false, Reason: "dcmd not enabled"}`.
3. If both layers pass, return `Decision{Allowed: true}`.

**OPC UA path**: `AuthorizeOPCUA(ctx, plcName, tagName string) Decision`. There is NO actor, NO role, and NO ACL lookup. Enforcement:

1. **Layer 1 — master switch**: if `tag.Writable == false`, return `Decision{Allowed: false, Reason: "tag not writable"}`.
2. **Layer 2 — OPC UA flag**: if `tag.OPCUAEnabled == false`, return `Decision{Allowed: false, Reason: "opcua not enabled"}`.
3. If both layers pass, return `Decision{Allowed: true}`.

Neither function performs the actual write or audit — callers do that after receiving the decision. The three
functions MUST NOT share an ACL-consulting code branch. An ACL rule allowing an operator to write a tag via HTTP
does NOT affect `AuthorizeDCMD` or `AuthorizeOPCUA` in any way.

#### Scenario: Writable=false denies every role including admin (HTTP)

- GIVEN tag `Feed.Rate` has `Writable=false`
- AND an ACL rule granting `admin` write on `Feed.Rate` exists
- WHEN `AuthorizeHTTP` is called with `actor.Role="admin"`
- THEN `Decision.Allowed` is `false`
- AND `Decision.Reason` is `"tag not writable"`

#### Scenario: Writable=true + ACL deny returns deny (HTTP)

- GIVEN tag `Feed.Rate` has `Writable=true`
- AND no ACL rule exists for `(operator, Silo-1, Feed.Rate)`
- WHEN `AuthorizeHTTP` is called with `actor.Role="operator"`
- THEN `Decision.Allowed` is `false`
- AND `Decision.Reason` is `"acl deny"`

#### Scenario: Both layers pass returns allow (HTTP)

- GIVEN tag `Feed.Rate` has `Writable=true`
- AND an ACL rule `(operator, Silo-1, Feed.Rate, allow_write=1)` exists
- WHEN `AuthorizeHTTP` is called with `actor.Role="operator"`
- THEN `Decision.Allowed` is `true`

#### Scenario: Writable=false denies DCMD regardless of DCMDEnabled

- GIVEN tag `Emergency.Stop` has `Writable=false` and `DCMDEnabled=true`
- WHEN `AuthorizeDCMD` is called
- THEN `Decision.Allowed` is `false`
- AND `Decision.Reason` is `"tag not writable"`

#### Scenario: DCMDEnabled=false denies DCMD regardless of ACL

- GIVEN tag `Feed.Rate` has `Writable=true` and `DCMDEnabled=false`
- AND an ACL rule grants `operator` write on `Feed.Rate` (HTTP would be allowed)
- WHEN `AuthorizeDCMD` is called
- THEN `Decision.Allowed` is `false`
- AND `Decision.Reason` is `"dcmd not enabled"`
- AND `aclStore.CanWrite` is NOT called

#### Scenario: Writable=true + DCMDEnabled=true allows DCMD

- GIVEN tag `Feed.Rate` has `Writable=true` and `DCMDEnabled=true`
- WHEN `AuthorizeDCMD` is called
- THEN `Decision.Allowed` is `true`

#### Scenario: Writable=false denies OPC UA regardless of OPCUAEnabled

- GIVEN tag `Emergency.Stop` has `Writable=false` and `OPCUAEnabled=true`
- WHEN `AuthorizeOPCUA` is called
- THEN `Decision.Allowed` is `false`
- AND `Decision.Reason` is `"tag not writable"`

#### Scenario: OPCUAEnabled=false denies OPC UA regardless of ACL

- GIVEN tag `Feed.Rate` has `Writable=true` and `OPCUAEnabled=false`
- AND an ACL rule grants `operator` write on `Feed.Rate` (HTTP would be allowed)
- WHEN `AuthorizeOPCUA` is called
- THEN `Decision.Allowed` is `false`
- AND `Decision.Reason` is `"opcua not enabled"`
- AND `aclStore.CanWrite` is NOT called

#### Scenario: Writable=true + OPCUAEnabled=true allows OPC UA

- GIVEN tag `Feed.Rate` has `Writable=true` and `OPCUAEnabled=true`
- WHEN `AuthorizeOPCUA` is called
- THEN `Decision.Allowed` is `true`

---

### [TWA-AUDIT-4.1] Audit every write attempt

Every write attempt — whether allowed or denied, whether via HTTP, DCMD or OPC UA — MUST emit exactly one
`AuditEvent` with:

| Field | Value |
|-------|-------|
| `Action` | `"tag.write"` |
| `ActorUsername` | username from JWT claims (HTTP path); empty string `""` (DCMD and OPC UA paths — no actor) |
| `TargetID` | unused; zero value (`int64(0)`). With `omitempty`, the field is absent from the JSON output. PLC identity is carried by name in `Detail`, not as an id. |
| `Detail` | flat `key=value` string: `"plc=<plc> tag=<tag> value=<value> outcome=<allow\|deny> source=<http\|dcmd\|opcua>"`, with ` reason=<reason>` appended when a reason is present. NOT JSON — the whole `AuditEvent` is already JSON-encoded to `events.jsonl`, so JSON inside `Detail` would be double-encoded. |

The `plc` and `source` fields MUST always be present in `Detail`; `source` MUST be one of `"http"`, `"dcmd"` or
`"opcua"`. For DCMD and OPC UA audit events, no role or actor-username information is included — those surfaces have
no actor. A denied write MUST still produce an audit event. Audit MUST fire before the handler returns.
`auditLog` calls MUST be nil-safe.

#### Scenario: Denied HTTP write still produces an audit event with source=http

- GIVEN tag `Feed.Rate` has `Writable=true`
- AND no ACL rule grants the actor write access
- WHEN `AuthorizeHTTP` returns `Allowed=false`
- THEN an audit event with `action="tag.write"`, `outcome="deny"`, `source="http"` is recorded
- AND the audit event is recorded before the HTTP handler returns

#### Scenario: Allowed HTTP write produces an audit event with source=http

- GIVEN `AuthorizeHTTP` returns `Allowed=true`
- WHEN `Driver.WriteTag` succeeds
- THEN an audit event with `action="tag.write"`, `outcome="allow"`, `source="http"` is recorded

#### Scenario: Denied DCMD write produces an audit event with source=dcmd and no role

- GIVEN tag `Feed.Rate` has `DCMDEnabled=false`
- WHEN a DCMD metric is received and `AuthorizeDCMD` returns `Allowed=false`
- THEN an audit event with `action="tag.write"`, `outcome="deny"`, `source="dcmd"` is recorded
- AND `ActorUsername` is `""` (empty — DCMD has no actor)
- AND the audit event is recorded before the DCMD callback returns

#### Scenario: Denied OPC UA write produces an audit event with source=opcua and no role

- GIVEN tag `Feed.Rate` has `OPCUAEnabled=false`
- WHEN an OPC UA write is denied
- THEN an audit event with `action="tag.write"`, `outcome="deny"`, `source="opcua"` is recorded
- AND `ActorUsername` is `""` (empty — OPC UA carries no authenticated principal)
- AND the audit event is recorded before the write response is returned
