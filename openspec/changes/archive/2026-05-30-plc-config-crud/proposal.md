# Proposal: PLC Configuration CRUD (UI-managed)

## Intent

The gateway has no way to add, edit, or remove PLCs at runtime. PLC config is YAML-only, requires a file edit + process context, and the read-only `handleConfigMappings` cannot mutate it. Phase 1 roadmap requires UI-driven PLC management. Operators must manage PLCs from the web UI; YAML becomes a one-time seed, not the live source.

## Scope

### In Scope
- New `internal/plcstore` SQLite package (`plcs.db` in `{dataDir}`), mirroring `auth/store.go`: PLC CRUD + child `plc_tags` table.
- Seed-on-first-run: when the store is empty, import `cfg.PLCs` once; thereafter the store wins.
- Admin-gated REST endpoints under `/api/plcs` (GET list/one, POST, PUT, DELETE) via `RequireRole(RoleAdmin)` + `withMiddleware`.
- Audit every mutation through `internal/auth/audit.go` (do NOT repeat the user-CRUD audit gap).
- Redirect the read path (`handleConfigMappings`) to the store so it never serves stale `s.cfg`.
- Extend/wrap `plcMgr.Reload` to drain-and-swap workers for *changed* PLCs (e.g. scanRate edit), not just added/removed.
- Handle `plcMgr == nil` (gateway booted with zero PLCs, then a PLC added via UI).
- Watcher `onChange` refactor: closure captures `*plcstore.Store`; merges non-PLC YAML fields with PLC data from the store.
- Reuse `internal/config` per-PLC validation for store-bound PLCs (extract per-entry validation if needed).
- Frontend `/plcs` route + management UI (list/create/edit/delete).
- Add a `writable bool` column to `plc_tags` for forward-compat, persisted but NOT enforced anywhere.

### Out of Scope
- Bidirectional write-ACL ENFORCEMENT (the `writable` field is stored, never gated).
- Sparkplug / OPC UA write wiring.
- Per-PLC live status dashboard.
- A `lgb plc import` CLI re-seed command (deferred; seed-on-empty covers MVP).

## Capabilities

### New Capabilities
- `plc-config-store`: SQLite-backed PLC config store, seed-on-first-run, admin REST CRUD, audited mutations, store-as-source-of-truth read path.

### Modified Capabilities
- `plc`: `[PLC-DRV-2.3]` hot-reload MUST actually drain-and-swap *changed* PLCs (spec already says "added, removed, or changed"; implementation only handles add/remove — closing the gap is a behavior change). Reload may be invoked directly by the store mutation path, bypassing the file watcher.
- `config`: `[PLC-CFG-1.x]` YAML `plcs[]` is reclassified as bootstrap/seed input, not the live runtime source. New `writable` tag attribute added to the schema (stored, unenforced).

## Approach

**Option B — dedicated SQLite store as source of truth** (per locked decision). YAML `plcs:` seeds the store once on first empty boot; all mutations go through `plcstore`, which directly calls `plcMgr.Reload` (or starts `plcMgr` if nil). This eliminates the file-watcher self-trigger loop, gets atomicity/durability free from SQLite/WAL, and preserves YAML comments (the API never writes YAML).

**Decisions resolving exploration open questions:**
- **Tags → child table** (`plc_tags` FK `plc_id`). More queryable, matches the relational auth schema, and gives the `writable` column a natural home for future per-tag gating. (JSON blob rejected: harder to query/extend.)
- **Watcher merge**: `onChange` closure captures `*plcstore.Store`; on a YAML reload it applies non-PLC fields (gateway/server/etc.) but sources `PLCs` from the store, so hand-edits to YAML `plcs:` are ignored after seeding (consistent with "store always wins").
- **Edit propagation**: extend `Reload` to compare existing PLCs field-by-field and drain-and-swap any whose config changed; satisfies the edit-from-UI requirement.
- **`writable`**: added to schema now for forward-compat, never enforced in this change.

## Affected Areas

| Area | Impact | Description |
|------|--------|-------------|
| `internal/plcstore/store.go` | New | SQLite PLC + tag CRUD, migrate-on-open, `SetMaxOpenConns(1)` |
| `internal/server/api_plcs.go` | New | Admin-gated CRUD handlers, audited |
| `internal/server/api_config.go` | Modified | `handleConfigMappings` reads store, not `s.cfg` |
| `internal/server/server.go` | Modified | Wire `*plcstore.Store` into `Opts`/struct |
| `internal/plc/manager.go` | Modified | `Reload` drains-and-swaps changed PLCs; handle empty start |
| `internal/config/*` | Modified | Extract per-PLC validation; reclassify YAML as seed; add `writable` |
| `cmd/lgb/cmd/server.go` | Modified | Seed-on-empty; `onChange` closure captures store; `plcMgr==nil` path |
| `web/` (`/plcs` route) | New | PLC management UI |

## Risks

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| Two-sources-of-truth confusion (YAML vs store) | High | Doc seed-only semantics; merge logic ignores YAML `plcs:` post-seed; recovery = delete `plcs.db` to re-seed |
| `plcMgr == nil` on zero-PLC boot then add | Med | Construct/start manager lazily inside the mutation path |
| `Reload` field-diff misses a changed field | Med | Diff full PLC struct (incl. tags); strict-TDD coverage per field |
| Stale read path after mutation | Med | Read path goes straight to the store |
| Scope creep into write-ACL enforcement | Med | `writable` stored but explicitly unenforced; out-of-scope stated |
| Exceeds 400-line PR budget | High | Split: PR1 backend (store + API + Reload + wiring), PR2 frontend |

## Rollback Plan

Backend is additive (`plcstore` package + new handler file) plus contained edits to `manager.go`, `api_config.go`, and `server.go` wiring. Rollback = revert the merge commit(s). The store file `plcs.db` is independent; deleting it triggers a clean re-seed from YAML on next boot. Frontend `/plcs` route is an isolated addition revertible on its own.

## Dependencies

- `internal/auth` (store pattern, RBAC, audit) — already merged.
- `internal/plc` manager + `internal/config` validation — already present.
- `modernc.org/sqlite` — already a dependency (pure-Go, no new deps).

## Success Criteria

- [ ] Empty store seeds from YAML `plcs:` exactly once on first boot.
- [ ] `POST/PUT/DELETE /api/plcs` are admin-only (403 for viewer/operator) and audited.
- [ ] Editing a PLC's scanRate restarts its worker (drain-and-swap on change).
- [ ] Adding a PLC to a gateway booted with zero PLCs works (`plcMgr==nil` handled).
- [ ] `handleConfigMappings` reflects store state immediately after any mutation.
- [ ] YAML `plcs:` hand-edits are ignored after seeding; non-PLC YAML reloads still apply.
- [ ] `/plcs` UI lists, creates, edits, deletes PLCs.
- [ ] `plc_tags.writable` column exists, is persisted, and is NOT enforced.
- [ ] All tests pass: `go test ./... -race -count=1`; cross-compiles for all 4 targets (no CGo).
