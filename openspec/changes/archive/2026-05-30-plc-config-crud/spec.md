---
change: plc-config-crud
phase: spec
date: 2026-05-29
status: draft
---

# Spec Index: PLC Configuration CRUD

## Domain Specs

| Domain | Type | File |
|--------|------|------|
| plc-config-store | New (full spec) | [specs/plc-config-store/spec.md](specs/plc-config-store/spec.md) |
| plc | Modified (delta) | [specs/plc/spec.md](specs/plc/spec.md) |
| config | Modified (delta) | [specs/config/spec.md](specs/config/spec.md) |

---

## Summary

### New Capability — `plc-config-store`

37 acceptance scenarios across 6 requirement groups:

| Group | Req IDs | Scope |
|-------|---------|-------|
| Store schema + open | PCS-STORE-1.1 | SQLite migrate-on-open, two tables, CGO=0 |
| Seed-on-first-run | PCS-STORE-1.2 | One-time import from `cfg.PLCs`; no-op if non-empty |
| CRUD | PCS-STORE-1.3 – 1.6 | Create/Read/Update/Delete with `ErrDuplicate` / `ErrNotFound` |
| Validation | PCS-STORE-1.7 | Mirrors config validation; aggregated via `errors.Join` |
| REST endpoints | PCS-API-2.1 – 2.6 | GET list/one (viewer+), POST/PUT/DELETE (admin); `handleConfigMappings` reads store |
| Reload + zero-PLC | PCS-RELOAD-3.1 | Lazy manager init; last PLC removed stops manager |
| Audit | PCS-AUDIT-4.1 | Every successful mutation writes an audit event |

### Modified Capability — `plc`

1 requirement modified:

| Req ID | Change |
|--------|--------|
| PLC-DRV-2.3 | Reload now compares all fields + tags (not name-only); accepts slice directly from mutation path (no watcher required); unchanged PLCs MUST NOT restart |

6 scenarios (2 new: scanRate change, tag-list change; 1 updated: direct-from-mutation call; 3 retained from prior spec).

### Modified Capability — `config`

1 requirement added, 2 requirements modified:

| Req ID | Type | Change |
|--------|------|--------|
| PCS-CFG-5.1 | ADDED | `writable bool` field on `TagDef`; stored, unenforced |
| PLC-CFG-1.1 | MODIFIED | YAML `plcs[]` reclassified as bootstrap seed only; store is runtime source of truth |
| PLC-CFG-1.7 | MODIFIED | Extended backward compat note to cover `writable` absence and seed-only semantics |

---

## Key Constraints

- `writable` (PCS-CFG-5.1): persisted in `plc_tags.writable`, never enforced — write-path gating is a sibling change.
- Seed guard (PCS-STORE-1.2): `Seed` MUST be a strict no-op when `COUNT(plcs) > 0`.
- Reload source of truth (PLC-DRV-2.3): `Reload` input list comes from the store, not from YAML, after first boot.
- Zero-PLC path (PCS-RELOAD-3.1): `plcMgr == nil` MUST NOT panic; the mutation handler initialises it lazily.
- Audit gap prevention (PCS-AUDIT-4.1): every successful mutation audits — no silent writes.
- Empty arrays (PCS-API-2.1): `data` MUST serialize as `[]`, never `null`.
- No new dependencies: `modernc.org/sqlite` is already in `go.mod`; no new `require` entries.
