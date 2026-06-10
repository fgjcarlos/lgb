---
change: write-acl
phase: spec
domain: tag-write-acl
date: 2026-06-01
status: active
type: new
---

# Tag Write ACL Specification

## Purpose

Defines the `internal/aclstore` package (SQLite role×tag ACL store), the `internal/writeguard` package (two-function enforcement core — `AuthorizeHTTP` and `AuthorizeDCMD`), the HTTP tag-write endpoint, the Sparkplug DCMD write surface, and the admin ACL management API. This capability is the first live write path for the LGB gateway. All write attempts — regardless of surface — go through surface-specific enforcement functions and are audited.

The design originally sketched a single `Authorize(ctx, WriteRequest)` function branching on a `Source` enum. As-built uses two separate top-level functions (`AuthorizeHTTP` and `AuthorizeDCMD`) whose type signatures make it structurally impossible to call the wrong gate for a given surface. The behavioral requirements below fully reflect the two-function model.

---

## Requirements

### [TWA-STORE-1.1] ACL store — schema and open

The `internal/aclstore` package MUST expose a `Store` type backed by `modernc.org/sqlite` (pure-Go). On `Open(ctx, path)` the store MUST:

1. Issue `PRAGMA foreign_keys = ON`.
2. Run migrate-on-open, creating the `tag_acl` table if it does not exist.
3. Call `SetMaxOpenConns(1)` to serialise writes and keep the PRAGMA stable.

| Table | Columns |
|-------|---------|
| `tag_acl` | `id INTEGER PK AUTOINCREMENT`, `role TEXT NOT NULL`, `plc TEXT NOT NULL`, `tag TEXT NOT NULL`, `allow_write INTEGER NOT NULL DEFAULT 0`, `UNIQUE(role, plc, tag)` |

`CGO_ENABLED=0` MUST remain valid. The surrogate `id` is internal; it is NOT exposed in any API response.

#### Scenario: Table created on first open

- GIVEN no `acl.db` file exists at `path`
- WHEN `Open(ctx, path)` is called
- THEN the file is created
- AND the `tag_acl` table exists in the schema

#### Scenario: Open is idempotent

- GIVEN `acl.db` already exists with the `tag_acl` table
- WHEN `Open(ctx, path)` is called again
- THEN it returns without error and existing rows are intact

#### Scenario: In-memory seam works for tests

- GIVEN `path` is `":memory:"`
- WHEN `Open(ctx, ":memory:")` is called
- THEN it returns a fully functional store with zero rows
- AND all CRUD operations work without filesystem access

---

### [TWA-STORE-1.2] IsEmpty and Seed

The store MUST expose `IsEmpty(ctx) (bool, error)` and `Seed(ctx, rules []ACLRule) error`. `Seed` MUST be a no-op when `rules` is empty OR when the store is already non-empty (idempotent).

#### Scenario: Empty store reports IsEmpty=true

- GIVEN the store has zero rows
- WHEN `IsEmpty(ctx)` is called
- THEN it returns `(true, nil)`

#### Scenario: Seed populates an empty store

- GIVEN the store has zero rows
- AND `rules` contains two ACL entries
- WHEN `Seed(ctx, rules)` is called
- THEN the store contains exactly two rows

#### Scenario: Seed is no-op on non-empty store

- GIVEN the store already contains one row
- WHEN `Seed(ctx, newRules)` is called with different rules
- THEN the existing row is unchanged and no new rows are added

---

### [TWA-STORE-1.3] CRUD — Create rule

The store MUST expose `CreateRule(ctx, r ACLRule) error`. Duplicate `(role, plc, tag)` MUST return `ErrRuleAlreadyExists`. Role MUST be one of `admin`, `operator`, `viewer`; an invalid role MUST return `ErrInvalidRole`. `CreateRule` returns only `error`; callers re-fetch via `GetRule`.

#### Scenario: Create a valid rule

- GIVEN the store is empty
- WHEN `CreateRule` is called with `(role="operator", plc="Silo-1", tag="Feed.Rate", allow_write=true)`
- THEN it returns nil
- AND `ListRules` includes the new entry

#### Scenario: Duplicate rule returns ErrRuleAlreadyExists

- GIVEN a rule for `(operator, Silo-1, Feed.Rate)` exists
- WHEN `CreateRule` is called with the same triple
- THEN it returns `ErrRuleAlreadyExists`

#### Scenario: Invalid role returns ErrInvalidRole

- GIVEN `role="superuser"` (not a valid role)
- WHEN `CreateRule` is called
- THEN it returns `ErrInvalidRole`

---

### [TWA-STORE-1.4] CRUD — Read rules

The store MUST expose:
- `ListRules(ctx) ([]ACLRule, error)` — all rows ordered by `(role, plc, tag)`
- `GetRule(ctx, id int64) (ACLRule, error)` — single row by surrogate id; missing returns `ErrRuleNotFound`
- `ListRulesByRole(ctx, role string) ([]ACLRule, error)` — filtered by role

#### Scenario: ListRules returns all rows

- GIVEN three rules in the store
- WHEN `ListRules(ctx)` is called
- THEN the returned slice has length 3

#### Scenario: GetRule unknown id returns ErrRuleNotFound

- GIVEN no rule with id 9999
- WHEN `GetRule(ctx, 9999)` is called
- THEN it returns `ErrRuleNotFound`

---

### [TWA-STORE-1.5] CRUD — Update and Delete rule

The store MUST expose:
- `UpdateRule(ctx, id int64, r ACLRule) error` — replaces role/plc/tag/allow_write; missing id returns `ErrRuleNotFound`; collision returns `ErrRuleAlreadyExists`; invalid role returns `ErrInvalidRole`
- `DeleteRule(ctx, id int64) error` — missing id returns `ErrRuleNotFound`

#### Scenario: Delete removes the rule

- GIVEN a rule with id 1
- WHEN `DeleteRule(ctx, 1)` is called
- THEN `GetRule(ctx, 1)` returns `ErrRuleNotFound`

#### Scenario: Update unknown id returns ErrRuleNotFound

- GIVEN no rule with id 9999
- WHEN `UpdateRule(ctx, 9999, r)` is called
- THEN it returns `ErrRuleNotFound`

---

### [TWA-STORE-1.6] CanWrite lookup

The store MUST expose `CanWrite(ctx, role, plc, tag string) (bool, error)`. It returns `(true, nil)` only when an exact-match row with `allow_write=1` exists for `(role, plc, tag)`. Any other case — no row, `allow_write=0` — returns `(false, nil)`. An error only on store failure.

#### Scenario: Exact match with allow_write=true

- GIVEN a rule `(operator, Silo-1, Feed.Rate, allow_write=1)` exists
- WHEN `CanWrite(ctx, "operator", "Silo-1", "Feed.Rate")` is called
- THEN it returns `(true, nil)`

#### Scenario: No matching row returns false

- GIVEN no rule for `(viewer, Silo-1, Feed.Rate)`
- WHEN `CanWrite(ctx, "viewer", "Silo-1", "Feed.Rate")` is called
- THEN it returns `(false, nil)`

#### Scenario: Row with allow_write=false returns false

- GIVEN a rule `(operator, Silo-1, Feed.Rate, allow_write=0)` exists
- WHEN `CanWrite(ctx, "operator", "Silo-1", "Feed.Rate")` is called
- THEN it returns `(false, nil)`

---

### [TWA-ENFORCE-2.1] Enforce — source-dispatched enforcement core

The enforcement core (`internal/writeguard`) exposes two top-level functions that apply the correct gate for each write surface.

**HTTP path**: `AuthorizeHTTP(ctx, actor Actor, plcName, tagName string, val any) Decision`. `Actor` carries username and role. Enforcement:

1. **Layer 1 — master switch**: if `tag.Writable == false`, return `Decision{Allowed: false, Reason: "tag not writable"}`. Applies to every role including admin. No ACL lookup is performed.
2. **Layer 2 — ACL**: call `aclStore.CanWrite(ctx, actor.Role, plcName, tagName)`. If false, return `Decision{Allowed: false, Reason: "acl deny"}`.
3. If both layers pass, return `Decision{Allowed: true}`.

**DCMD path**: `AuthorizeDCMD(ctx, plcName, tagName string) Decision`. There is NO actor, NO role, and NO ACL lookup. Enforcement:

1. **Layer 1 — master switch**: if `tag.Writable == false`, return `Decision{Allowed: false, Reason: "tag not writable"}`.
2. **Layer 2 — DCMD flag**: if `tag.DCMDEnabled == false`, return `Decision{Allowed: false, Reason: "dcmd not enabled"}`.
3. If both layers pass, return `Decision{Allowed: true}`.

Neither function performs the actual write or audit — callers do that after receiving the decision. The two functions MUST NOT share an ACL-consulting code branch. An ACL rule allowing an operator to write a tag via HTTP does NOT affect `AuthorizeDCMD` in any way.

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

---

### [TWA-ENFORCE-2.2] Deny-by-default (empty ACL — HTTP path)

With no ACL rules configured, every HTTP write MUST be denied. `AuthorizeHTTP` MUST NOT allow writes when `aclStore` is empty, regardless of `tag.Writable` status. (DCMD deny-by-default is enforced by `DCMDEnabled` defaulting to `false` — see PCS-CFG-5.2.)

#### Scenario: Empty ACL denies all HTTP writes

- GIVEN the ACL store has zero rows
- AND tag `Feed.Rate` has `Writable=true`
- WHEN `AuthorizeHTTP` is called with any actor and any tag
- THEN `Decision.Allowed` is `false`
- AND `Decision.Reason` is `"acl deny"`

---

### [TWA-ENFORCE-2.3] Tag existence validation

Before calling `Authorize`, the write handler MUST verify that the requested tag exists in the PLC configuration. An unknown tag MUST be rejected with an error distinct from an ACL denial.

#### Scenario: Write to unknown tag returns 404

- GIVEN no tag named `"Ghost.Tag"` exists on PLC `"Silo-1"`
- WHEN `POST /api/plcs/Silo-1/tags/Ghost.Tag/write` is called with a valid token
- THEN the response is `404 Not Found` with code `tag_not_found`
- AND no ACL lookup is performed
- AND no audit event is emitted

---

### [TWA-HTTP-3.1] HTTP write endpoint

The system MUST expose `POST /api/plcs/{plc}/tags/{tag}/write`, gated by `auth.RequireRole(RoleAdmin, RoleOperator, RoleViewer)` (any authenticated role may attempt; ACL decides the outcome).

Request body: `{"value": <any scalar>}`. Response codes:

| Condition | Status | Code |
|-----------|--------|------|
| Write succeeded | `200 OK` | — |
| Tag not found | `404 Not Found` | `tag_not_found` |
| ACL deny (either layer) | `403 Forbidden` | `write_denied` |
| Malformed body / bad value type | `400 Bad Request` | `bad_request` |
| Unauthenticated | `401 Unauthorized` | — |

The handler MUST: (1) validate the tag exists, (2) call `AuthorizeHTTP`, (3) on allow: call `Driver.WriteTag`, (4) emit audit event with `source="http"`, (5) return response. On deny: emit audit event with `source="http"`, return `403`. The audit event MUST be written before the handler returns in both allow and deny paths.

Note on write-value typing: the frontend sends `value` as a string. The gateway passes the value as-is to `Driver.WriteTag` without coercion. Type coercion (string → numeric) is a known limitation accepted for this change.

#### Scenario: Authorized write succeeds — HTTP

- GIVEN tag `Feed.Rate` has `Writable=true`
- AND an ACL rule allows `operator` to write `Feed.Rate` on `Silo-1`
- AND a valid operator JWT is provided
- WHEN `POST /api/plcs/Silo-1/tags/Feed.Rate/write` with body `{"value": 2.5}`
- THEN response is `200 OK`
- AND an audit event with `action="tag.write"`, `outcome="allow"`, actor, plc, tag, value is recorded

#### Scenario: ACL denied write returns 403 — HTTP

- GIVEN tag `Feed.Rate` has `Writable=true`
- AND no ACL rule grants `viewer` write on `Feed.Rate`
- AND a valid viewer JWT is provided
- WHEN `POST /api/plcs/Silo-1/tags/Feed.Rate/write` with body `{"value": 1.0}`
- THEN response is `403 Forbidden` with code `write_denied`
- AND an audit event with `action="tag.write"`, `outcome="deny"`, reason `"acl deny"` is recorded

#### Scenario: Master-switch denied write returns 403 — HTTP

- GIVEN tag `Emergency.Stop` has `Writable=false`
- AND a valid admin JWT is provided
- WHEN `POST /api/plcs/Silo-1/tags/Emergency.Stop/write` with body `{"value": true}`
- THEN response is `403 Forbidden` with code `write_denied`
- AND an audit event with `outcome="deny"`, reason `"tag not writable"` is recorded

#### Scenario: Unauthenticated write returns 401

- GIVEN no Authorization header
- WHEN `POST /api/plcs/Silo-1/tags/Feed.Rate/write`
- THEN response is `401 Unauthorized`
- AND no audit event is emitted

---

### [TWA-DCMD-3.2] Sparkplug DCMD write surface

The gateway MUST set `OnCommand` in the Sparkplug Node config to a handler that processes inbound DCMD metric writes. DCMD input is UNTRUSTED (arrives over MQTT). **DCMD is OFF by default.** A tag is DCMD-writable only when BOTH `tag.Writable == true` AND `tag.DCMDEnabled == true`. There is NO role lookup, NO actor resolution, and NO ACL consultation for DCMD writes. The per-tag `dcmd_enabled` flag IS the entire DCMD authorization decision.

Implementation note: the handler is wired via `SetCommandHandler` on the `EdgeNode` AFTER `server.New` returns but BEFORE `srv.Run` (which calls `spNode.Start` and subscribes to DCMD topics). This ordering is load-bearing — `SetCommandHandler` docstring states "MUST be called before Start". The ordering has no compile-time guard; `TestDCMDHandler_AllowWhenBothFlagsTrue` serves as the regression safety net.

`deviceID` in the DCMD callback IS the PLC name (set in `defaultSparkplugNodeFactory` as `DeviceID: p.Name`).

The DCMD handler MUST:

1. Parse the DCMD payload to extract `(plc, tag, value)`.
2. Call `AuthorizeDCMD(ctx, plcName, tagName)` — no actor, no role, no credential lookup.
3. On allow: call `Driver.WriteTag`, emit audit event with `source="dcmd"`.
4. On deny: drop the command silently (no MQTT error response), emit audit event with `outcome="deny"`, `source="dcmd"`.

The DCMD handler MUST NOT call `aclStore.CanWrite` or `AuthorizeHTTP`. No MQTT credential is resolved. An operator-level HTTP ACL rule granting write on a tag does NOT enable DCMD writes to that tag.

#### Scenario: (a) DCMD write proceeds when both flags are true

- GIVEN tag `Feed.Rate` has `Writable=true` AND `DCMDEnabled=true`
- WHEN a DCMD metric for `Feed.Rate` with value `3.0` is received
- THEN `Driver.WriteTag("Feed.Rate", 3.0)` is called
- AND an audit event with `action="tag.write"`, `outcome="allow"`, `source="dcmd"` is recorded
- AND no ACL lookup is performed

#### Scenario: (b) DCMD dropped when DCMDEnabled=false — ACL rule does NOT help

- GIVEN tag `Feed.Rate` has `Writable=true` AND `DCMDEnabled=false`
- AND an ACL rule `(operator, Silo-1, Feed.Rate, allow_write=1)` exists (HTTP operators can write)
- WHEN a DCMD metric for `Feed.Rate` is received
- THEN `Driver.WriteTag` is NOT called
- AND the command is dropped (no MQTT response published)
- AND an audit event with `action="tag.write"`, `outcome="deny"`, `source="dcmd"`, `reason="dcmd not enabled"` is recorded
- AND `aclStore.CanWrite` is NOT called

#### Scenario: (c) DCMD dropped when Writable=false — regardless of DCMDEnabled

- GIVEN tag `Emergency.Stop` has `Writable=false` AND `DCMDEnabled=true`
- WHEN a DCMD metric for `Emergency.Stop` is received
- THEN `Driver.WriteTag` is NOT called
- AND an audit event with `action="tag.write"`, `outcome="deny"`, `source="dcmd"`, `reason="tag not writable"` is recorded

#### Scenario: (d) DCMD deny audit records source=dcmd with no role

- GIVEN any tag with `DCMDEnabled=false`
- WHEN a DCMD metric is received and denied
- THEN the audit event contains `source="dcmd"`
- AND the audit event does NOT contain an actor username or role field (or records them as empty/absent)

---

### [TWA-AUDIT-4.1] Audit every write attempt

Every write attempt — whether allowed or denied, whether via HTTP or DCMD — MUST emit exactly one `AuditEvent` with:

| Field | Value |
|-------|-------|
| `Action` | `"tag.write"` |
| `ActorUsername` | username from JWT claims (HTTP path); empty string `""` (DCMD path — no actor) |
| `TargetID` | unused; zero value (`int64(0)`). With `omitempty`, the field is absent from the JSON output. PLC identity is carried by name in `Detail`, not as an id. |
| `Detail` | flat `key=value` string: `"plc=<plc> tag=<tag> value=<value> outcome=<allow\|deny> source=<http\|dcmd>"`, with ` reason=<reason>` appended when a reason is present. NOT JSON — the whole `AuditEvent` is already JSON-encoded to `events.jsonl`, so JSON inside `Detail` would be double-encoded. |

The `plc` and `source` fields MUST always be present in `Detail`; `source` MUST be either `"http"` or `"dcmd"`. For DCMD audit events, no role or actor-username information is included — DCMD has no actor. A denied write MUST still produce an audit event. Audit MUST fire before the handler returns. `auditLog` calls MUST be nil-safe.

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

---

### [TWA-API-5.1] Admin ACL CRUD API

The system MUST expose ACL management endpoints under `/api/acl/rules`. All endpoints MUST be gated by `auth.RequireRole(RoleAdmin)`.

| Method | Path | Success | Error cases |
|--------|------|---------|-------------|
| `GET` | `/api/acl/rules` | `200 {"data":[...]}` | `401`, `403` |
| `GET` | `/api/acl/rules/{id}` | `200 {"data":<rule>}` | `404 rule_not_found` |
| `POST` | `/api/acl/rules` | `201 {"data":<rule>}` | `400 invalid_rule`, `409 duplicate_rule` |
| `PUT` | `/api/acl/rules/{id}` | `200 {"data":<rule>}` | `400`, `404`, `409` |
| `DELETE` | `/api/acl/rules/{id}` | `204 No Content` | `404 rule_not_found` |

Each mutation MUST emit an audit event (action: `acl.create` | `acl.update` | `acl.delete`). Audit fires only after a successful store write.

Note on plc/tag existence validation: the ACL store accepts free-form `plc`/`tag` values. No existence check against the plcstore is performed at the API layer (no FK between the two SQLite files). This is intentional — operators may pre-create ACL rules before provisioning the PLC.

#### Scenario: Admin lists ACL rules

- GIVEN two ACL rules in the store and a valid admin token
- WHEN `GET /api/acl/rules`
- THEN response is `200 OK` and `data` has length 2

#### Scenario: Non-admin is rejected with 403

- GIVEN a valid operator token
- WHEN `GET /api/acl/rules`
- THEN response is `403 Forbidden`

#### Scenario: Admin creates a rule

- GIVEN admin token and body `{"role":"operator","plc":"Silo-1","tag":"Feed.Rate","allow_write":true}`
- WHEN `POST /api/acl/rules`
- THEN response is `201 Created`
- AND an audit event with `action="acl.create"` is recorded

#### Scenario: Duplicate rule returns 409

- GIVEN a rule for `(operator, Silo-1, Feed.Rate)` already exists
- WHEN `POST /api/acl/rules` with the same triple
- THEN response is `409 Conflict` with code `duplicate_rule`

#### Scenario: Invalid role returns 400

- GIVEN admin token and body `{"role":"superuser","plc":"Silo-1","tag":"Feed.Rate"}`
- WHEN `POST /api/acl/rules`
- THEN response is `400 Bad Request` with code `invalid_rule`

#### Scenario: Admin deletes a rule

- GIVEN rule id 1 exists and admin token
- WHEN `DELETE /api/acl/rules/1`
- THEN response is `204 No Content`
- AND `GET /api/acl/rules/1` subsequently returns `404`
- AND an audit event with `action="acl.delete"` is recorded

---

## Acceptance Test Matrix

| Req ID | Scenario | Test Type |
|--------|----------|-----------|
| TWA-STORE-1.1 | Table created on first open | Unit (tmp file) |
| TWA-STORE-1.1 | Open is idempotent | Unit (tmp file) |
| TWA-STORE-1.1 | In-memory seam works | Unit |
| TWA-STORE-1.2 | IsEmpty on empty store | Unit |
| TWA-STORE-1.2 | Seed populates empty store | Unit |
| TWA-STORE-1.2 | Seed no-op on non-empty store | Unit |
| TWA-STORE-1.3 | Create valid rule | Unit |
| TWA-STORE-1.3 | Duplicate rule returns ErrRuleAlreadyExists | Unit |
| TWA-STORE-1.3 | Invalid role returns ErrInvalidRole | Unit |
| TWA-STORE-1.4 | ListRules returns all rows | Unit |
| TWA-STORE-1.4 | GetRule unknown id returns ErrRuleNotFound | Unit |
| TWA-STORE-1.5 | Delete removes the rule | Unit |
| TWA-STORE-1.5 | Update unknown id returns ErrRuleNotFound | Unit |
| TWA-STORE-1.6 | CanWrite — exact match allow | Unit |
| TWA-STORE-1.6 | CanWrite — no row returns false | Unit |
| TWA-STORE-1.6 | CanWrite — allow_write=0 returns false | Unit |
| TWA-ENFORCE-2.1 | Writable=false denies admin (HTTP) | Unit |
| TWA-ENFORCE-2.1 | Writable=true + ACL deny (HTTP) | Unit |
| TWA-ENFORCE-2.1 | Both layers pass (HTTP) | Unit |
| TWA-ENFORCE-2.1 | Writable=false denies DCMD regardless of DCMDEnabled | Unit |
| TWA-ENFORCE-2.1 | DCMDEnabled=false denies DCMD regardless of ACL | Unit |
| TWA-ENFORCE-2.1 | Writable=true + DCMDEnabled=true allows DCMD | Unit |
| TWA-ENFORCE-2.2 | Empty ACL denies all HTTP writes | Unit |
| TWA-ENFORCE-2.3 | Unknown tag returns 404 | Unit (httptest) |
| TWA-HTTP-3.1 | Authorized write succeeds | Unit (httptest) |
| TWA-HTTP-3.1 | ACL denied write returns 403 | Unit (httptest) |
| TWA-HTTP-3.1 | Master-switch denied returns 403 | Unit (httptest) |
| TWA-HTTP-3.1 | Unauthenticated returns 401 | Unit (httptest) |
| TWA-DCMD-3.2 | (a) DCMD proceeds when Writable+DCMDEnabled=true | Unit |
| TWA-DCMD-3.2 | (b) DCMD dropped when DCMDEnabled=false; ACL rule irrelevant | Unit |
| TWA-DCMD-3.2 | (c) DCMD dropped when Writable=false; DCMDEnabled irrelevant | Unit |
| TWA-DCMD-3.2 | (d) DCMD deny audit has source=dcmd and no role | Unit |
| TWA-AUDIT-4.1 | Denied HTTP write produces audit event with source=http | Unit |
| TWA-AUDIT-4.1 | Allowed HTTP write produces audit event with source=http | Unit |
| TWA-AUDIT-4.1 | Denied DCMD write produces audit event with source=dcmd and no role | Unit |
| TWA-API-5.1 | Admin lists ACL rules | Unit (httptest) |
| TWA-API-5.1 | Non-admin returns 403 | Unit (httptest) |
| TWA-API-5.1 | Admin creates rule | Unit (httptest) |
| TWA-API-5.1 | Duplicate returns 409 | Unit (httptest) |
| TWA-API-5.1 | Invalid role returns 400 | Unit (httptest) |
| TWA-API-5.1 | Admin deletes rule | Unit (httptest) |
