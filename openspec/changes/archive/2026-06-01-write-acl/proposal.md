# Proposal: PLC Tag Write Enforcement with Role×Tag ACL

## Intent

The gateway is **read-only** from the operator's perspective — there is NO live write path. `Driver.WriteTag` exists (`internal/plc/gologix.go:191`) but is orphaned (never called outside tests), and the Sparkplug DCMD handler drops every inbound command because `OnCommand` is nil in production (`cmd/lgb/cmd/server.go:402-408`; the field is wired only in tests). This change **builds the first write capability**, with authorization baked in from day one. Writes hit physical fish-farm-feeder hardware, so the design is **safe-by-default**: no write succeeds unless it passes both an engineering master switch and an operational ACL.

## Scope

### In Scope
- New SQLite ACL store: table `tag_acl(role, plc, tag) → allow_write` + CRUD.
- One shared **enforcement core** consumed by BOTH write surfaces (no duplicated authz).
- HTTP write endpoint (operator-facing) wiring the orphaned `Driver.WriteTag`.
- Sparkplug **DCMD** surface: set the nil `OnCommand` callback through the same core.
- Two-layer enforcement: `tag.Writable` master switch → ACL matrix.
- Audit every attempt (`tag.write`) — allow and deny — with actor, plc, tag, value, outcome.
- Admin ACL management API + frontend matrix UI.

### Out of Scope
- **OPC UA write wiring** — DCMD is the only Sparkplug-side surface this round.
- Change-on-Value (COV) and per-PLC dashboard — sibling changes, not this one.
- Rank-based roles — `auth.RequireRole` stays exact-match allowlist.

## Capabilities

### New Capabilities
- `tag-write-acl`: role×tag write permission matrix + two-layer enforcement + audit.

### Modified Capabilities
- `plc`: `Writable` flag becomes an enforced master switch (was stored, never checked).

## Approach

Single enforcement function `Authorize(actor, plc, tag, val) → decision`:
1. **Master switch** — `tag.Writable == false` → DENY (even for admin). Engineering-level safety.
2. **ACL** — consulted only if master is on; `(role,plc,tag)` must have `allow_write`.
Both surfaces call this, then `Driver.WriteTag`, then audit. ACL store follows the `plcstore`/`auth` template (`modernc.org/sqlite`, `SetMaxOpenConns(1)`, `PRAGMA foreign_keys=ON`, migrate-on-open, `:memory:` seam). `AuditEvent.Detail` carries plc/tag/value/outcome; `TargetID` carries plc_id.

## Affected Areas

| Area | Impact | Description |
|------|--------|-------------|
| `internal/aclstore/` | New | ACL SQLite store + CRUD |
| `internal/plc/write` (enforcement) | New | Shared `Authorize` + write+audit core |
| `cmd/lgb/cmd/server.go:402` | Modified | Set `OnCommand` → enforcement core |
| HTTP API + OpenAPI | Modified | `POST` write endpoint + ACL admin CRUD |
| `frontend/` | Modified | Write control + ACL matrix page |

## Risks

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| Wrong write damages hardware | High impact | Master switch defaults off; both layers must pass |
| Orphaned `WriteTag` has untested edge cases | Med | Integration tests before exposing the surface |
| Concurrent writes to same tag | Med | Single-driver serialization; document no read-back guarantee |
| Audit gaps hide an attack | Med | Audit before returning, on both allow and deny paths |
| DCMD spoofing via MQTT | Med | Enforcement core treats DCMD as untrusted; same ACL gate |

## Rollback Plan

Stacked PRs revert independently. Worst case: drop the write endpoint and leave `OnCommand` nil — gateway returns to read-only with zero data-model loss (ACL table is additive).

## Dependencies

- `plc-config-store` (merged) — provides `plc_tags.writable`.

## PR Breakdown (stacked-to-main, chained)

| PR | Scope | ~LOC | Depends on |
|----|-------|------|-----------|
| PR1 | ACL store: table + CRUD + seed/IsEmpty | ~250 | — |
| PR2 | Enforcement core + HTTP write endpoint + `WriteTag` wiring + audit | ~320 | PR1 |
| PR3 | Sparkplug DCMD `OnCommand` → same core | ~180 | PR2 |
| PR4 | ACL admin CRUD API + OpenAPI | ~280 | PR1 |
| PR5 | Frontend: write control + ACL matrix page | ~360 | PR2,PR4 |

## Success Criteria

- [ ] Write to a `Writable=false` tag is DENIED for every role, including admin.
- [ ] HTTP and DCMD writes share one enforcement path (no duplicated authz).
- [ ] Every write attempt (allow + deny) produces a `tag.write` audit event.
- [ ] Admin can manage the role×tag matrix from the UI.
- [ ] OPC UA write remains absent (out of scope), no regressions to read path.
