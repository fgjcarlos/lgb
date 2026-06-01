# Exploration — `plc-config-crud`

PLC configuration CRUD from the web UI: who is the source of truth once the UI can mutate PLC config?

## Current state (verified against code)

- **Config struct + `TagDef`**: `internal/config/config.go`. `Config.PLCs []PLC`; each `PLC` has `Name, Address, Slot, SocketTimeout, ScanRate, KeepAlive, Path, Tags []TagDef`. `TagDef` has only `Name, Type` — **no `writable`/ACL field**.
- **Loading**: koanf 3-layer (defaults → YAML → `LGB_*` env), `internal/config/loader.go`. **No `yaml.Marshal` anywhere** — nothing can write YAML back out. `go.yaml.in/yaml/v3` is an *indirect* dep only.
- **Watcher**: `internal/config/watcher.go` — `config.Watch(ctx, path, cb)` via koanf file.Provider.Watch (fsnotify) with ~200ms debounce. Any write to the YAML triggers a reload.
- **Watcher wiring**: `cmd/lgb/cmd/server.go` (~line 277) → `plcMgr.Reload(watchCtx, newCfg)`.
- **`plcMgr.Reload(ctx, cfg)`**: `internal/plc/manager.go` (~191-261). Compares `cfg.PLCs` by name; stops removed, starts new, leaves existing untouched. **Whole-PLC granularity — does NOT restart a worker whose config (e.g. scanRate) merely changed.**
- **No mutation path**: `api_config.go` (`handleConfigMappings`) is explicitly read-only.
- **SQLite already used twice**: `historian` (`lgb.db`) and `auth` (`users.db`), both `modernc.org/sqlite`, pattern `sql.Open → SetMaxOpenConns(1) → migrate`. `internal/auth/store.go` (~200 LOC full user CRUD) is the direct template.
- **RBAC reference**: `auth.RequireRole(auth.RoleAdmin)` + `withMiddleware`; full mutation example in `api_users.go`.
- **Audit**: JSONL at `{dataDir}/events.jsonl` (`internal/auth/audit.go`). Only login is audited; **user CRUD emits no audit events** (relevant gap).

### Two sharp constraints discovered
1. **`Server.cfg` is a frozen pointer** — set at startup, never updated when the watcher reloads. `handleConfigMappings` therefore serves the STARTUP config even after a hot-reload. Any mutation approach must decide how to keep the read path fresh.
2. **`plcMgr` is only created when `len(cfg.PLCs) > 0`** (`server.go` ~line 170). A gateway that boots with zero PLCs and then adds one via the UI hits a `plcMgr == nil` path that must be handled.

## The fork — three options

### Option A — YAML stays source of truth (API writes YAML back)
API mutates the live `[]PLC`, serializes the full slice back to the YAML file, watcher hot-reloads.

- **Self-trigger loop**: API write → fsnotify → debounce → `plcMgr.Reload`. `Reload` is idempotent so the extra reload is harmless but wasteful; a 200ms window leaves running state transiently inconsistent vs the HTTP response. Clean handling needs a `suppressWatcher` flag (shared mutable state between handler + watcher goroutine) or accepting the double-reload.
- **YAML round-trip destroys comments/formatting/ordering** — koanf has no Marshal; would add `go.yaml.in/yaml/v3` as a *direct* dep. Significant regression if operators keep the YAML in Git with explanatory comments.
- **Atomicity**: `os.WriteFile` is not atomic — needs `CreateTemp`+`Rename` (~20 LOC). A crash mid-write corrupts the ONLY source of truth.
- **Concurrent admin edits**: read-modify-write race → needs a process mutex.

### Option B — Separate mutable SQLite store (YAML becomes bootstrap-only)
New `internal/plcstore` (mirrors `auth/store.go`). Seeded from YAML on first run; API reads/writes the store exclusively; mutation handler calls `plcMgr.Reload` directly (watcher bypassed for PLCs).

- **Self-trigger loop eliminated.** Atomicity & crash-safety **free** via SQLite/WAL transactions.
- **YAML comments preserved** (API never writes YAML).
- **Two-sources-of-truth risk HIGH**: YAML and store diverge the moment a mutation happens; an operator editing YAML expecting a reload finds it ignored. The watcher's `onChange` must merge non-PLC YAML fields with PLC data from the store → callback must capture `*plcstore.Store` (new coupling).
- **Bootstrap/migration**: seed-if-empty on first run; a `lgb plc import` CLI for re-seed.
- **Recovery**: corrupt store → re-seed from YAML.

### Option C — Layered overlay (base YAML + overlay store merged at load)
Base YAML always read; SQLite overlay shadows it (overlay wins, overlay-only added, tombstones for deletes).

- Legitimate pattern for fleet base-config + per-site overrides, but **no documented requirement** for it here.
- Comments preserved (YAML never written). Two-sources contained by merge but debugging surface doubles (check two places). Most complex (tombstones, re-merge on every YAML change).

## Tradeoff matrix

| Criterion | A — YAML writeback | B — SQLite store | C — Overlay |
|---|---|---|---|
| Self-trigger loop | Present (suppression adds complexity) | Eliminated | Present for YAML; overlay bypasses |
| Atomicity/durability | Hand-rolled atomic rename | Free (SQLite/WAL) | Overlay=B; base=A |
| YAML comment preservation | Destroyed | Preserved | Preserved |
| Reuse existing machinery | Reload+validate; new yaml.Marshal+writer | Reload+validate; new SQLite store | Reload; new merge+overlay |
| Two-sources-of-truth | None | High | Medium |
| Mutation → plcMgr | Watcher (+200ms) or direct | Direct, immediate | Direct + re-merge |
| RBAC + audit | Same for all | Same | Same |
| Migration/bootstrap | None | Seed on first run + CLI | Overlay starts empty |
| New external deps | +`yaml/v3` direct | None | None |
| Est. effort | Medium (6-8 tasks) | Medium-High (8-10) | High (10-14) |
| Blast radius | Bug corrupts only config | Store corrupt → YAML fallback | Most complex failure modes |
| Operational simplicity | One file (current mental model) | State in plcs.db (doc burden) | Two places to check |

## Reasoned lean

**Option B** is the stronger choice for this codebase: atomicity is free, the watcher self-trigger disappears, YAML comments survive (real concern for industrial deployments that Git their config), and it follows the existing two-SQLite-store pattern (`auth` is the template) with a clean re-seed recovery path. Cost: a new `plcstore` package + a watcher-callback refactor.

**Option A** stays defensible if the YAML is a pure runtime artifact (not Git-tracked, no comments) and speed is the priority — the double-reload is harmless.

**Option C** is not recommended without a concrete fleet/overlay requirement.

## Open questions for the proposal phase

1. Is the YAML checked into Git in customer deployments? (If yes → comment preservation kills Option A.)
2. `plcs.db` location — `{dataDir}` alongside `users.db`/`lgb.db`? (existing pattern says yes)
3. Tags: child table (`plc_tags`, queryable) vs JSON blob (simple)?
4. Scope of the watcher `onChange` refactor for Option B (closure must capture the store).
5. Fix the user-CRUD audit gap in this change, or scope to PLC only?
6. `plcMgr.Reload` ignores changed-but-not-added/removed PLCs (e.g. scanRate edit). Address here?
7. Concurrency for Option A: process mutex enough, or flock? (one-process-per-gateway → mutex is enough)

## Risks carried into proposal

- Option A: comment destruction + non-atomic write + new direct yaml dep.
- `Server.cfg` frozen pointer → stale reads after mutation (affects all options).
- `plcMgr` nil when booting with zero PLCs (affects all options).
- `plcMgr.Reload` does not restart workers on in-place config change (limits usefulness of "edit").
- Audit gap on user CRUD must not be repeated for PLC CRUD.
