# Design: PLC Tag Write Enforcement with Role×Tag ACL

## Technical Approach

One enforcement core, `Authorize(ctx, req) → Decision`, consumed by BOTH the HTTP write
handler and the DCMD callback — but the two surfaces take DIFFERENT gates because they have
DIFFERENT trust properties. `Authorize` branches on `req.Source`:
- **HTTP**: `tag.Writable` (master switch) AND ACL `(role-from-JWT, plc, tag) → allow_write`.
- **DCMD**: `tag.Writable` (master switch) AND `dcmd_enabled[plc,tag]` (explicit per-tag
  opt-in). NO role lookup, NO ACL consult, NO fake principal.
Both deny-by-default and both audit (allow AND deny). Codebase premises verified:
`Driver.WriteTag` exists but is unreached; `Manager` has no write method; `CommandHandler`
is `func(deviceID, tag string, value any)` (no principal — confirms MQTT carries no
identity); `AuditEvent` has no plc/tag/value fields; roles are exact-match. The
`plc_tags` table already carries `writable` per tag — `dcmd_enabled` rides alongside it.

## Architecture Decisions

### 1. Package layout (cycle-safe)
**Choice**: ACL store in `internal/aclstore`. Enforcement core in a NEW top-level
`internal/writeguard` (NOT `internal/plc/write` as the proposal sketched).
**Rationale**: the core must import `plc` (Manager), `aclstore`, and `auth` (audit).
Putting it inside `internal/plc` would force `plc` to import `aclstore`+`auth`, risking a
cycle and bloating the driver package. `writeguard` sits ABOVE `plc` — no inbound edge.

### 2. Reaching a driver to write
**Choice**: add `Manager.WriteTag(plcName, tag string, val any) error` (PR2). Looks up the
worker under `mu.RLock`, returns `ErrPLCNotFound` if absent, else delegates to
`driver.WriteTag`. Add `WriteTag` to the server's `PLCManager` interface.
**Rationale**: `Driver(name)` exists but exposing raw drivers to callers leaks lifecycle;
a Manager method keeps serialization and lookup in one place. Driver contract already
guarantees single-goroutine I/O.

### 3. The Authorize contract
```go
type Source int
const ( SourceHTTP Source = iota; SourceDCMD )

type WriteRequest struct {
    Source Source            // SELECTS the gate, not just an audit tag
    Actor  string            // human username (HTTP) or "" (DCMD — no identity)
    Role   auth.Role         // from Claims.Role (HTTP only); IGNORED for SourceDCMD
    PLC, Tag string; Value any
}
type Decision struct { Allowed bool; Reason string } // Reason for audit: "master_switch_off"|"no_acl_rule"|"dcmd_not_enabled"|"allowed"
func (g *Guard) Authorize(ctx context.Context, req WriteRequest) Decision
```
`Source` is now LOAD-BEARING: it selects which gate runs, not merely a label the audit
records. Both paths first check the master switch `tag.Writable`, read from the **plcstore**
(`Get(plc).Tags[tag].Writable`) — single source of truth, already hot-reloaded. Then:
- `SourceHTTP` → ACL lookup in `aclstore` for `(req.Role, plc, tag)`; `Role` from `Claims.Role`.
- `SourceDCMD` → per-tag flag `dcmd_enabled`, also read from the **plcstore** (rides on
  `plc_tags`, see §6/§4b). `Role` and `Actor` are NOT consulted for DCMD.

This means the DCMD path NEVER touches `aclstore` and NEVER maps to any `auth.Role`.

**AS-BUILT NOTE**: The implementation uses two separate top-level functions (`AuthorizeHTTP`
and `AuthorizeDCMD`) rather than a single `Authorize(ctx, WriteRequest)` branching on
`req.Source`. This is a better design — type signatures make it structurally impossible to
call the wrong gate. The `Source` enum exists in the package but is not used as a runtime
discriminator by callers. All behavioral requirements are fully satisfied.

### 4. ACL schema
```sql
CREATE TABLE IF NOT EXISTS tag_acl (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  role        TEXT    NOT NULL,
  plc         TEXT    NOT NULL,
  tag         TEXT    NOT NULL,
  allow_write INTEGER NOT NULL DEFAULT 0,
  UNIQUE(role, plc, tag)
);
CREATE INDEX IF NOT EXISTS idx_tag_acl_lookup ON tag_acl(role, plc, tag);
```
**Free-form** `plc`/`tag` (no FK to plcs DB — separate database file; cross-DB FK impossible
with two `*sql.DB`). Validation against plcstore happens at the admin-API layer (PR4), not
the schema. Deny-by-default: absent row = no allow. NOTE: this `tag_acl` table governs the
HTTP surface ONLY. DCMD does NOT use it (see §4b).

### 4b. DCMD opt-in storage — `dcmd_enabled` on `plc_tags`
**Choice**: add a `dcmd_enabled INTEGER NOT NULL DEFAULT 0` column to the existing
`plc_tags` table in **plcstore**, co-located with `writable`. NOT a separate aclstore table.
```sql
CREATE TABLE IF NOT EXISTS plc_tags (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    plc_id       INTEGER NOT NULL REFERENCES plcs(id) ON DELETE CASCADE,
    name         TEXT    NOT NULL,
    type         TEXT    NOT NULL,
    writable     INTEGER NOT NULL DEFAULT 0,
    dcmd_enabled INTEGER NOT NULL DEFAULT 0,   -- NEW: per-tag DCMD opt-in
    UNIQUE(plc_id, name)
);
```
**Rationale (decisive)**: `writable` and `dcmd_enabled` are the SAME KIND of control — per-tag
ENGINEERING safety switches set by whoever provisions the PLC, deny-by-default, NOT operational
ACL. The role×tag matrix is operational (who-can-do-what, changes often); the two master
switches are structural (does this tag physically accept writes at all, and from which surface).
Co-locating keeps both safety switches on the same row, hot-reloaded by the same plcstore
mechanism `Authorize` already reads for `Writable` — so the DCMD path needs ZERO new store
dependency. Verified the column threads cleanly through the EXACT surfaces `writable` already
touches:
- `internal/config/config.go` `TagDef` — add `DcmdEnabled bool koanf:"dcmd_enabled"` beside `Writable`.
- `internal/plcstore/store.go` — `migrate` DDL, `insertTags` INSERT, `listTags` SELECT/Scan
  (all three already carry `writable`); plus a `ALTER TABLE plc_tags ADD COLUMN dcmd_enabled`
  idempotent migration for existing DBs (see Migration section).
- `internal/server/api_plcs.go` — `plcTagResponse` + request mapping (mirrors the `Writable`
  field at lines 17/36/67).
- `frontend/src/pages/PLCs.tsx` — Zod schema (`dcmd_enabled: z.boolean()`), `append` default,
  and a second checkbox beside the existing `Writable` one (~line 463-467).

**Rejected — separate aclstore table**: would force the writeguard core to read TWO stores
for the DCMD master+enable pair, split a single per-tag safety concept across two DB files
with no FK between them, and duplicate hot-reload plumbing. No upside: dcmd-enable is not
operational ACL and does not belong with the role matrix.

### 5. AUDIT DATA MODEL — DECISION (REVISED at PR2)
**Decision: pack into `Detail` as a flat `key=value` string now; do NOT extend `AuditEvent` this round, and do NOT use JSON inside `Detail`.**

| Option | Pros | Cons |
|--------|------|------|
| Pack `Detail` (key=value: plc,tag,value,outcome,source,reason) | Follows existing pattern (`plc.create` uses a plain-string `Detail`); zero churn; one PR; human-readable in the jsonl | "writes to tag X" needs a string scan, not an indexed query |
| Pack `Detail` (JSON) | Marginally more parseable | **Double-encoded**: the whole `AuditEvent` is already `json.Encoder.Encode`d to events.jsonl, so JSON inside `Detail` becomes escaped JSON-in-JSON (`"detail":"{\"plc\":...}"`) — ugly and harder to read/scan, not easier |
| Extend `AuditEvent` (+PLC,Tag,Value,Outcome) | Structured queries | Touches every existing audit call + jsonl format; events.jsonl is append-only flat file with NO query layer anyway |

Decisive evidence: the audit sink is `events.jsonl` via `json.Encoder` (`internal/auth/audit.go:45`) — the entire event, including `Detail`, is JSON-encoded once. Putting JSON inside the `Detail` string therefore double-encodes it (escaped quotes), which is strictly worse than a flat string for the same flat-file scan. There is NO query engine, so structured columns/JSON buy nothing today. Use `Action:"tag.write"`, `Username:actor` (empty for DCMD), and `Detail:"plc=<plc> tag=<tag> value=<value> outcome=<allow|deny> source=<http|dcmd> reason=<reason>"`. The `source` and `plc` fields MUST always be present. Defer any structured-audit extension to when a queryable audit store actually exists.

### 6. DCMD trust model — SECURITY-CRITICAL (REVISED)
**Choice**: DCMD is OFF by default and gated by an EXPLICIT per-tag opt-in flag
(`dcmd_enabled`), IN ADDITION to the `tag.Writable` master switch. DCMD does NOT map to any
`auth.Role`, does NOT use a fixed `sparkplug-dcmd` principal, and NEVER consults the
role×tag ACL (`tag_acl`). The DCMD enforcement path is exactly:
```
SourceDCMD allowed  ⟺  tag.Writable == true  AND  dcmd_enabled[plc,tag] == true
```
There are THREE distinct enforcement paths in the system, each fully decoupled:
| Surface | Identity | Gate 1 (master) | Gate 2 (authorization) |
|---------|----------|-----------------|------------------------|
| HTTP write | `Claims.Role` from JWT | `tag.Writable` | ACL `(role, plc, tag) → allow_write` |
| DCMD write | NONE (MQTT is anonymous) | `tag.Writable` | `dcmd_enabled[plc,tag]` per-tag opt-in |

**Rationale (the locked sub-decision)**: a role-based ACL for DCMD is THEATER. MQTT carries
no identity — `CommandHandler` is `func(deviceID, tag, value)` with no principal — so there
is no role to verify and inventing a fixed `operator` principal would be a fiction the code
pretends to authorize. The previous model (`sparkplug-dcmd → operator`, same ACL) had a
concrete failure: granting the HUMAN operator role HTTP-write to a tag would SILENTLY also
open that tag to DCMD spoofing, because both surfaces shared the `(operator, plc, tag)` row.
That couples two unrelated trust surfaces. The per-tag `dcmd_enabled` flag IS the DCMD
authorization — the only control that means anything when there is no identity to check — and
it DECOUPLES the surfaces: enabling HTTP-operator write does nothing to DCMD, and vice versa.
Deny-by-default holds on BOTH gates independently: a tag with no `dcmd_enabled` is not
writable via DCMD even if `Writable` and an operator ACL row exist. Every DCMD attempt is
audited with `source:"dcmd"`.

### 7. Concurrency / write semantics
Single-driver serialization: `Driver` contract guarantees single-goroutine I/O; gologix
serializes via internal mutex. `Manager.WriteTag` holds `mu.RLock` only for lookup, then
calls the driver. **Fire-and-forget**: return the driver's write error; NO read-back /
confirmation this round (documented limitation). Concurrent writes to the same tag are not
ordered beyond what the driver mutex provides.

### 8. Frontend matrix UI shape (PR5)
- ACL page: `<Table>` with one row per (plc,tag), one column per role
  (admin/operator/viewer). Each cell = native `<input type="checkbox">` bound to RHF;
  toggling PUTs the ACL row. TanStack Query for fetch + invalidate, matching `PLCs.tsx`.
- Write control: on the PLC/tag view, a small write form (native `<input>` + button) gated
  by `tag.writable`; disabled with a tooltip when master switch is off. Admin-route gated
  via `router.tsx` like `/plcs`.
- DCMD opt-in is NOT on the ACL matrix page — it is a per-tag engineering switch and lives
  on the EXISTING PLC tag form in `frontend/src/pages/PLCs.tsx`, a second checkbox beside the
  current `Writable` one (Zod `dcmd_enabled: z.boolean()`, `append` default `false`). This
  surfaces in the plc-config CRUD UI, NOT the write-acl page. It is part of the schema
  precursor (see PR breakdown), not PR5.

### 9. TDD seams
- `aclstore.Open(ctx, ":memory:")` — RED tests for CRUD/Seed/IsEmpty (PR1).
- `Guard` takes interfaces: `tagWritableLookup` (fake reads plcstore) + `aclReader`
  (fake reads aclstore) + `writer` (fake `WriteTag`) + audit sink — enables pure-unit
  Authorize tests with no real DB or PLC (PR2).
- DCMD: existing `EdgeNodeConfig.OnCommand` seam (already test-wired at
  `edge_node_test.go:409`) — inject a Guard-backed handler that builds a `SourceDCMD`
  request (no Role/Actor). The `dcmd_enabled` lookup reuses the SAME `tagWritableLookup`
  fake/seam against plcstore — no new aclstore dependency for the DCMD test (PR3).
- Admin API: `:memory:` aclstore + fake audit, `httptest` (PR4), mirroring `api_plcs_test.go`.

## Data Flow
```
HTTP POST /api/plcs/{name}/tags/{tag}/write          DCMD (MQTT, anonymous)
        │ Claims.Role                                     │ deviceID,tag,val
        ▼ Source=HTTP                                     ▼ Source=DCMD (no identity)
   actorFromContext ─────────► writeguard.Guard.Authorize ◄───── OnCommand handler
                              branch on req.Source ──┐
              SourceHTTP ─────────────────────────┐  │  ┌───────────── SourceDCMD
              Writable (plcstore) AND              │  │  │  Writable (plcstore) AND
              allow_write (aclstore role×tag)      │  │  │  dcmd_enabled (plcstore plc_tags)
                                          allow ◄──┴──┴──┴──► deny
                                            │                  │
                                  Manager.WriteTag             │
                                       (driver)                │
                                            └──── auditLog.Log("tag.write") ◄──┘  (both paths)
```
DCMD reads BOTH its gates (`Writable` + `dcmd_enabled`) from plcstore and never touches
aclstore. HTTP reads `Writable` from plcstore and `allow_write` from aclstore.

## File Changes
| File | Action | Description |
|------|--------|-------------|
| `internal/aclstore/store.go` | Create | ACL SQLite store mirroring plcstore (PR1) |
| `internal/writeguard/guard.go` | Create | `Guard`, `Authorize`, write+audit core (PR2) |
| `internal/plc/manager.go` | Modify | add `Manager.WriteTag(plc,tag,val)` (PR2) |
| `internal/server/server.go` | Modify | add `WriteTag` to `PLCManager`; inject aclStore+guard (PR2/PR4) |
| `internal/server/api_write.go` | Create | `POST .../write` handler (PR2) |
| `internal/config/config.go` | Modify | add `TagDef.DcmdEnabled bool` beside `Writable` (PR3-pre) |
| `internal/plcstore/store.go` | Modify | `dcmd_enabled` column: migrate DDL + ADD COLUMN migration, `insertTags`, `listTags` (PR3-pre) |
| `internal/server/api_plcs.go` | Modify | `dcmd_enabled` in tag response + request mapping (PR3-pre) |
| `frontend/src/pages/PLCs.tsx` | Modify | `dcmd_enabled` Zod field + checkbox on the existing tag form (PR3-pre) |
| `cmd/lgb/cmd/server.go:402` | Modify | set `OnCommand` → guard-backed `SourceDCMD` handler (PR3) |
| `internal/server/api_acl.go` | Create | ACL admin CRUD + OpenAPI (PR4) |
| `frontend/src/pages/ACL.tsx`, `router.tsx`, `useApi.ts` | Create/Modify | matrix UI + write control (PR5) |

## Testing Strategy
| Layer | What | Approach |
|-------|------|----------|
| Unit | Authorize SourceHTTP: master-off denies admin; no ACL rule denies; both-pass allows | fakes for lookup/acl/writer/audit |
| Unit | Authorize SourceDCMD: master-off denies; dcmd_enabled=false denies EVEN WITH a matching operator ACL row; master+dcmd_enabled allows; ACL is never consulted | fakes; assert aclReader gets ZERO calls for SourceDCMD |
| Unit | aclstore CRUD/Seed/IsEmpty | `:memory:` |
| Unit | plcstore round-trips `DcmdEnabled` (default false; true persists); ADD COLUMN migration on a pre-existing DB | `:memory:` + a seeded legacy schema |
| Integration | HTTP write 200/403/404; audit emitted on allow+deny | `httptest` + `:memory:` |
| Integration | DCMD invokes guard; denied write (dcmd_enabled off) audited, never reaches driver | `OnCommand` seam + fake Manager |

## PR Breakdown & Dependencies (REVISED)
The `dcmd_enabled` flag riding on `plc_tags` adds a small plc-config-store dependency to the
DCMD path. Plan:
- **PR1** — `internal/aclstore` (HTTP role×tag ACL store). Independent.
- **PR2** — `internal/writeguard` Guard + `Manager.WriteTag` + HTTP `POST .../write` handler;
  `Authorize` with the `Source` branch (HTTP path fully wired; DCMD path coded but unreached).
- **PR3-pre** (NEW, small precursor) — `dcmd_enabled` schema change in plc-config-store:
  `TagDef.DcmdEnabled`, plcstore column + `ALTER TABLE … ADD COLUMN` migration, `api_plcs`
  field, and the `PLCs.tsx` tag-form checkbox. This is a cross-change touch on the already-
  merged plc-config-store feature. Can ship as its own tiny PR OR fold into the front of PR3.
  **PR3 depends on PR3-pre** — the DCMD guard path reads `dcmd_enabled` from plcstore.
- **PR3** — wire `OnCommand` → guard-backed `SourceDCMD` handler at `cmd/lgb/cmd/server.go:402`.
- **PR4** — ACL admin CRUD API (`api_acl.go`).
- **PR5** — frontend ACL matrix page + HTTP write control. (DCMD checkbox is NOT here — it
  lives in PR3-pre on the existing PLC form.)

Recommendation: fold PR3-pre into PR3 unless the plc-config-store schema change needs separate
review by that feature's owner; if so, ship PR3-pre first as a standalone column+migration PR.

## Migration / Rollout
`tag_acl` is additive (new DB file). The `dcmd_enabled` column needs an idempotent
`ALTER TABLE plc_tags ADD COLUMN dcmd_enabled INTEGER NOT NULL DEFAULT 0` for existing
plc-config-store databases (the `CREATE TABLE IF NOT EXISTS` only covers fresh DBs). Default
`0` means every existing tag is DCMD-disabled after upgrade — the conservative, deny-by-default
posture. Rollback: drop write endpoint and leave `OnCommand` nil → gateway returns to
read-only; `tag_acl` dormant, `dcmd_enabled` column harmless (ignored).

## Open Questions
- [ ] Per-device DCMD identity model — out of scope. The per-tag `dcmd_enabled` flag is the
      authorization for anonymous MQTT; a future identity-aware DCMD could add an ACL path,
      but that does not change today's flag-based gate.
