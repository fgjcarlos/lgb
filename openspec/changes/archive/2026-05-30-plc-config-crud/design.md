# Design: PLC Configuration CRUD (UI-managed)

## Technical Approach

Introduce a new `internal/plcstore` SQLite package that becomes the live source of truth for PLC
configuration. It mirrors `internal/auth/store.go` exactly (`modernc.org/sqlite`,
`SetMaxOpenConns(1)`, migrate-on-open, sentinel errors), adding a parent `plcs` table and a child
`plc_tags` table. On first boot the store seeds itself from `cfg.PLCs` if and only if it is empty;
thereafter every mutation flows through the store and the YAML `plcs:` block is treated as inert
seed input.

The store is wired into `server.Opts`/`Server`. A new admin-gated handler file `api_plcs.go`
exposes CRUD. After each successful mutation the handler rebuilds a `*config.Config` whose `PLCs`
come from the store and calls `plcMgr.Reload` **directly**, bypassing the file watcher entirely for
PLC changes (no self-trigger loop). `Manager.Reload` is upgraded from add/remove granularity to a
full field-level diff so that an edited PLC (e.g. changed `scanRate`) is drained-and-swapped while
unchanged PLCs keep running. The `plcMgr == nil` zero-PLC boot path is closed by **always
constructing an empty manager** at startup.

The read path (`handleConfigMappings`) is redirected to query the store instead of the frozen
`s.cfg` pointer, guaranteeing it reflects mutations immediately. The watcher `onChange` closure is
refactored to capture the store, apply non-PLC YAML fields, but always source `PLCs` from the store.

The frontend adds an admin-only `/plcs` route (same `requiredRole: "admin"` gating as `/users`)
with a list + create/edit/delete UI built on React Hook Form + Zod and TanStack Query, plus
`useApi` hooks for the new endpoints.

This change is split into two PRs to stay under the 400-line budget: **PR1** backend
(store + API + Reload diff + wiring), **PR2** frontend.

## Architecture Decisions

| Decision | Choice | Rejected | Rationale |
|----------|--------|----------|-----------|
| Source of truth | Dedicated SQLite store (`plcs.db`); YAML `plcs:` is seed-only | YAML writeback (Option A); layered overlay (Option C) | Free atomicity/durability via SQLite; eliminates fsnotify self-trigger; preserves YAML comments (API never writes YAML); follows the existing two-SQLite-store pattern (`auth`/`historian`) |
| Tag storage | Child `plc_tags` table, FK `plc_id ON DELETE CASCADE` | JSON blob column on `plcs` | Queryable, relational like `auth`; gives `writable` a natural typed home for future per-tag gating |
| Identity / key | `name` is the natural key (UNIQUE), route param `{name}`; surrogate `id INTEGER PK` for the FK | Use `id` in routes | PLC name is already the manager's worker key and the YAML key; URL-stable; mirrors `cfg.PLCs` keyed-by-name semantics |
| `plcMgr == nil` boot | **Always construct an empty `Manager`** at startup (drop the `len(cfg.PLCs) > 0` guard) | Lazy construct inside the mutation handler | An empty manager is cheap (no workers, no goroutines until `Reload` adds them); removes a nil branch from every mutation/read path; `Start` over an empty worker map is a no-op |
| Read path freshness | `handleConfigMappings` queries the store directly each request | Keep `s.cfg.PLCs` mutated in place; refresh a cached pointer | `s.cfg` is a frozen startup pointer shared across goroutines; mutating it is racy. Querying the store is O(rows), always correct, no locking on `Server` |
| Mutation → Reload | Handler builds `*config.Config` from store PLCs and calls `plcMgr.Reload(ctx, cfg)` synchronously | Touch the YAML to let the watcher fire; expose a store-native Reload | Direct call is immediate, deterministic, and avoids the 200 ms debounce window + double-reload. No self-trigger because the store, not the file, changed |
| Reload diff granularity | Field-level diff of full `config.PLC` (incl. `Tags`) via `reflect.DeepEqual`; drain-and-swap changed PLCs | Keep add/remove-only (current) | Closes `[PLC-DRV-2.3]` gap ("added, removed, **or changed**"); a scanRate edit must restart the worker |
| Validation reuse | Extract `func ValidatePLC(p PLC) error` from `Config.Validate`; call it on each incoming PLC in the handler | Re-validate the whole `Config`; duplicate rules in the handler | Single source of validation truth; per-entry errors map cleanly to a 400 on one PLC |
| Concurrency | SQLite `SetMaxOpenConns(1)` + a single write transaction per mutation; no app-level mutex | `sync.Mutex` on the store; `flock` | One-process-per-gateway; the single connection serializes writes; create-with-tags is wrapped in one `BEGIN/COMMIT` for atomicity |
| Audit | Every mutation calls `auditLog.Log` with a `plc.*` action; nil-safe | Skip (repeat the user-CRUD gap) | Proposal explicitly forbids repeating the user-CRUD audit gap |
| `writable` | Stored on `plc_tags`, surfaced in API responses, **never enforced** | Enforce now | Out of scope; forward-compat only |

## Data Model

### `plcs` table

```sql
CREATE TABLE IF NOT EXISTS plcs (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    name           TEXT    NOT NULL UNIQUE,
    address        TEXT    NOT NULL,
    slot           INTEGER NOT NULL DEFAULT 0,
    socket_timeout TEXT    NOT NULL DEFAULT '',
    scan_rate      TEXT    NOT NULL DEFAULT '',
    keep_alive     INTEGER NOT NULL DEFAULT 0,  -- bool 0/1
    path           TEXT    NOT NULL DEFAULT '',
    created_at     INTEGER NOT NULL,
    updated_at     INTEGER NOT NULL
);
```

### `plc_tags` table (child)

```sql
CREATE TABLE IF NOT EXISTS plc_tags (
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    plc_id    INTEGER NOT NULL REFERENCES plcs(id) ON DELETE CASCADE,
    name      TEXT    NOT NULL,
    type      TEXT    NOT NULL,
    writable  INTEGER NOT NULL DEFAULT 0,  -- bool 0/1; STORED, NOT ENFORCED
    UNIQUE(plc_id, name)
);
```

Note: `modernc.org/sqlite` does not enforce foreign keys unless `PRAGMA foreign_keys = ON` is set
per connection. The store issues `PRAGMA foreign_keys = ON` in `migrate` (after `sql.Open`,
`SetMaxOpenConns(1)` guarantees the single connection retains the pragma). Delete therefore cascades
to `plc_tags`; the store also explicitly deletes child rows inside the delete transaction as a
belt-and-suspenders guard so the behavior holds regardless of the pragma.

Field mapping `plcs`+`plc_tags` ↔ `config.PLC`/`config.TagDef`:

| Column | Go field | Notes |
|--------|----------|-------|
| `name` | `PLC.Name` | natural key |
| `address` | `PLC.Address` | |
| `slot` | `PLC.Slot` | |
| `socket_timeout` | `PLC.SocketTimeout` | duration string |
| `scan_rate` | `PLC.ScanRate` | duration string |
| `keep_alive` | `PLC.KeepAlive` | int 0/1 ↔ bool |
| `path` | `PLC.Path` | |
| `plc_tags.name` | `TagDef.Name` | |
| `plc_tags.type` | `TagDef.Type` | |
| `plc_tags.writable` | (none yet) | new field; `config.TagDef` gains `Writable bool` |

`config.TagDef` gains a `Writable bool` field (`koanf:"writable"`), unenforced, so YAML seeds and API
round-trips can carry it. `config.PLC` is otherwise unchanged.

## Interfaces / Contracts

### New package `internal/plcstore`

```go
package plcstore

var (
    ErrPLCNotFound      = errors.New("plc not found")
    ErrPLCAlreadyExists = errors.New("plc already exists")
)

type Store struct { db *sql.DB }

func Open(ctx context.Context, path string) (*Store, error) // mirrors auth.OpenUserStore
func (s *Store) Close() error

// CRUD — all operate in terms of config.PLC so callers stay decoupled from the schema.
func (s *Store) List(ctx context.Context) ([]config.PLC, error)         // ordered by name; tags joined
func (s *Store) Get(ctx context.Context, name string) (config.PLC, error)
func (s *Store) Create(ctx context.Context, p config.PLC) error          // tx: insert plc + tags
func (s *Store) Update(ctx context.Context, name string, p config.PLC) error // tx: update plc + replace tags
func (s *Store) Delete(ctx context.Context, name string) error           // tx: cascade tags

// Bootstrap.
func (s *Store) IsEmpty(ctx context.Context) (bool, error)               // SELECT COUNT(*)==0
func (s *Store) Seed(ctx context.Context, plcs []config.PLC) error        // bulk Create in one tx; no-op on []
```

`Create`/`Update` wrap the parent row + all tag rows in a single transaction (`db.BeginTx` →
insert/replace → `Commit`). `Update` replaces tags wholesale (`DELETE FROM plc_tags WHERE plc_id=?`
then re-insert) — simpler and correct than per-tag diffing. Returning `config.PLC` from `List`/`Get`
means the store is the only place that knows SQL; handlers and `Reload` consume plain config types.

### `internal/config` additions

```go
// config.go
type TagDef struct {
    Name     string `koanf:"name"`
    Type     string `koanf:"type"`
    Writable bool   `koanf:"writable"` // NEW — stored, never enforced
}

// validate_plc.go (extracted from Config.Validate)
func ValidatePLC(p PLC) error // returns wrapped ErrConfigInvalid on first/any violation
```

`Config.Validate` is refactored to call `ValidatePLC` inside its `for i, plc := range c.PLCs` loop,
preserving the indexed messages. `ValidatePLC` runs the same rules (address non-empty, durations
parse and positive, slot 0–15, tag name/type non-empty, type is a valid Sparkplug scalar). Tag
`Writable` is not validated.

### `internal/plc/manager.go` — Reload diff

```go
func (m *Manager) Reload(ctx context.Context, cfg *config.Config) error
```

New algorithm (replaces the add/remove-only body):

```
newByName := map[name]config.PLC from cfg.PLCs
lock:
  for name, w in m.workers:
      new, exists := newByName[name]
      if !exists                          -> toDrain (removed)
      else if !reflect.DeepEqual(w.cfg, new) -> toDrain AND mark for re-add (changed)
  cancel + delete drained workers (removed and changed); collect drivers + names
unlock
wg.Wait()                                 // drained goroutines exit
close drained drivers
lock:
  for plcCfg in cfg.PLCs:
      if _, exists := m.workers[name]; !exists -> factory(plcCfg); start worker (covers new + changed)
unlock
```

Key point: a *changed* PLC is treated as remove-then-add, so its worker is fully drained (driver
closed, goroutine joined) before a fresh worker with the new config starts. Unchanged PLCs are never
touched. `reflect.DeepEqual(w.cfg, plcCfg)` compares the full `config.PLC` including the `Tags`
slice, so any field edit (scanRate, address, tag add/remove, etc.) triggers the swap.

`NewManager` already tolerates an empty `cfg.PLCs` (maps sized 0, no workers). `Start` over an empty
worker map launches no goroutines and returns nil — so an always-constructed empty manager is safe.

### `internal/server` — Server + Opts + handlers

```go
// server.go
type Server struct {
    // ... existing ...
    plcStore *plcstore.Store // nil-safe; read path + CRUD source of truth
}
type Opts struct {
    // ... existing ...
    PLCStore *plcstore.Store
}

// api_plcs.go (new) — all methods on *Server
func (s *Server) handleListPLCs(w http.ResponseWriter, r *http.Request)   // GET  /api/plcs
func (s *Server) handleGetPLC(w http.ResponseWriter, r *http.Request)     // GET  /api/plcs/{name}
func (s *Server) handleCreatePLC(w http.ResponseWriter, r *http.Request)  // POST /api/plcs
func (s *Server) handleUpdatePLC(w http.ResponseWriter, r *http.Request)  // PUT  /api/plcs/{name}
func (s *Server) handleDeletePLC(w http.ResponseWriter, r *http.Request)  // DELETE /api/plcs/{name}

// helper: after a successful mutation, rebuild config + reload the manager.
func (s *Server) reloadPLCsFromStore(ctx context.Context) error
```

`reloadPLCsFromStore` lists store PLCs, builds `&config.Config{PLCs: list}`, and calls
`s.plcMgr.Reload(ctx, cfg)` (nil-safe). It is invoked by Create/Update/Delete after the store commit
succeeds. The `Reload` context is the server run context; handlers pass `r.Context()` for the store
op and a long-lived context for `Reload` (the manager owns worker lifetimes, not the request).

### REST endpoints

| Method | Route | Role | Body | Success | Errors |
|--------|-------|------|------|---------|--------|
| GET | `/api/plcs` | admin | — | `200 {"data":[PLC...]}` | — |
| GET | `/api/plcs/{name}` | admin | — | `200 {"data":{PLC}}` | 404 `plc_not_found` |
| POST | `/api/plcs` | admin | `{PLC}` | `201 {"data":{PLC}}` | 400 `bad_request`/`invalid_plc`, 409 `duplicate_plc` |
| PUT | `/api/plcs/{name}` | admin | `{PLC}` | `200 {"data":{PLC}}` | 400, 404, 409 (rename collision) |
| DELETE | `/api/plcs/{name}` | admin | — | `204 No Content` | 404 |

Request/response PLC shape (JSON, snake_case to match `mappingResponse` style):

```json
{
  "name": "line1",
  "address": "10.0.0.5",
  "slot": 0,
  "socket_timeout": "5s",
  "scan_rate": "1s",
  "keep_alive": true,
  "path": "",
  "tags": [{"name": "Temp", "type": "Float", "writable": false}]
}
```

All routes registered in `registerAPIRoutes` behind `withMiddleware(..., authMiddleware,
RequireRole(RoleAdmin))`, guarded by `if s.plcStore != nil && s.authTokens != nil` exactly like the
user-CRUD block. The read path `GET /api/config/mappings` stays viewer+ but is redirected to the
store (see below).

Handler validation: each mutation decodes the body into a `config.PLC`, calls
`config.ValidatePLC(p)` → 400 `invalid_plc` with the violation message on failure, then the store
op. `Create` maps `ErrPLCAlreadyExists` → 409; `Get`/`Update`/`Delete` map `ErrPLCNotFound` → 404.
After a successful store write, `reloadPLCsFromStore` is called; a reload error is logged WARN but
does **not** fail the HTTP response (the store is already committed — the mutation succeeded;
worker convergence is best-effort and self-heals on next reload).

Audit (nil-safe `if s.auditLog != nil`): `plc.create` / `plc.update` / `plc.delete` with
`Username` from the request context, `Detail` = PLC name.

### Read-path redirect — `handleConfigMappings`

```go
func (s *Server) handleConfigMappings(w http.ResponseWriter, r *http.Request) {
    var plcs []config.PLC
    if s.plcStore != nil {
        plcs, _ = s.plcStore.List(r.Context())
    } else if s.cfg != nil {
        plcs = s.cfg.PLCs // fallback for tests / no-store deployments
    }
    // map to []mappingResponse exactly as today (now includes writable per tag if surfaced)
}
```

This is the chosen mechanism: **the read endpoint queries the store directly per request**. The
frozen `s.cfg` is kept only as a nil-store fallback. No `Server` field is cached or refreshed; there
is nothing to invalidate.

## Data Flow

### Seed-on-first-run (`cmd/lgb/cmd/server.go`)

```
open plcStore (plcs.db in resolvedPath) — like users.db, context.Background()
empty, err := plcStore.IsEmpty(ctx)
if empty:
    plcStore.Seed(ctx, cfg.PLCs)          // bulk insert in one tx; []  -> no-op
    log "plc store seeded from yaml" (count)
# load the live PLC set FROM THE STORE for manager construction + server wiring
storePLCs, _ := plcStore.List(ctx)
liveCfg := *cfg; liveCfg.PLCs = storePLCs  // copy so cfg stays the frozen YAML view
plcMgr = factory(&liveCfg, tagCb)          // ALWAYS constructed (even if 0 PLCs)
```

Idempotency: `IsEmpty` gates seeding, so a restart with an existing `plcs.db` never re-seeds — store
wins. Recovery falls out naturally: delete `plcs.db`, restart → `IsEmpty` true → re-seed from YAML.

The manager and `server.Opts` are built from `storePLCs` (store state), not raw `cfg.PLCs`, so even
on the very first boot the store and the running workers agree.

### Mutation flow (e.g. PUT /api/plcs/line1)

```
Client ─PUT /api/plcs/line1──> handleUpdatePLC  (admin)
  │ decode body -> config.PLC
  │ config.ValidatePLC(p)            ├─ invalid -> 400 invalid_plc
  │ plcStore.Update(ctx, "line1", p) ├─ ErrPLCNotFound -> 404
  │                                  ├─ ErrPLCAlreadyExists (rename collision) -> 409
  │                                  └─ ok (tx committed)
  │ auditLog.Log(plc.update, user, "line1")
  │ reloadPLCsFromStore(runCtx):
  │     list := plcStore.List(); plcMgr.Reload(runCtx, {PLCs:list})
  │        └─ diff: line1 changed -> drain old worker, start new; others untouched
  │ respond 200 {data: updated PLC from store}
```

No file is written, so the watcher never fires for this change — self-trigger eliminated.

### Watcher coexistence (`onChange` closure refactor)

```
go config.Watch(watchCtx, ConfigPath, func(newCfg *config.Config) {
    // non-PLC YAML fields may have changed; PLCs always come from the store.
    storePLCs, err := plcStore.List(watchCtx)
    if err != nil { log warn; return }
    merged := *newCfg
    merged.PLCs = storePLCs           // store wins; YAML plcs: ignored post-seed
    plcMgr.Reload(watchCtx, &merged)
})
```

The closure captures `plcStore` (new coupling, accepted in the proposal). A hand-edit to YAML
`plcs:` triggers a reload, but `merged.PLCs` is sourced from the store, so the edit is ignored —
consistent with "store always wins". Non-PLC reloads (which today only feed `Reload` anyway) behave
unchanged. The watcher goroutine now always starts because `plcMgr` is always non-nil.

## File Changes

| File | Action | Description |
|------|--------|-------------|
| `internal/plcstore/store.go` | Create | `Store`, `Open`, `Close`, `List`, `Get`, `Create`, `Update`, `Delete`, `IsEmpty`, `Seed`, `migrate`; mirrors `auth/store.go` |
| `internal/plcstore/store_test.go` | Create | CRUD + tags + seed/IsEmpty + cascade-delete tests on a temp/`:memory:` DB |
| `internal/config/config.go` | Modify | Add `TagDef.Writable bool`; `Validate` delegates per-PLC to `ValidatePLC` |
| `internal/config/validate_plc.go` | Create | Extracted `ValidatePLC(p PLC) error` |
| `internal/config/validate_plc_test.go` | Create | Per-field validation table tests |
| `internal/plc/manager.go` | Modify | `Reload` field-level diff + drain-and-swap of changed PLCs |
| `internal/plc/manager_test.go` | Modify | Add "edit scanRate restarts worker" + "unchanged untouched" cases using the existing fake `DriverFactory` |
| `internal/server/api_plcs.go` | Create | 5 handlers + `reloadPLCsFromStore` helper; audited |
| `internal/server/api_plcs_test.go` | Create | CRUD + RBAC (403 viewer/operator) + 404/409 + audit assertions |
| `internal/server/api_config.go` | Modify | `handleConfigMappings` reads `plcStore`, `s.cfg` fallback |
| `internal/server/api.go` | Modify | Register `/api/plcs` admin routes in `registerAPIRoutes` |
| `internal/server/server.go` | Modify | Add `plcStore` field + `Opts.PLCStore`; wire in `New` |
| `cmd/lgb/cmd/server.go` | Modify | Open `plcs.db`; seed-on-empty; build manager from store PLCs; always construct manager; `onChange` captures store |
| `cmd/lgb/cmd/server_test.go` | Modify | Adjust the "no PLCs -> factory NOT called" test (manager now always constructed; assert empty manager) |
| `frontend/src/hooks/useApi.ts` | Modify | `usePLCs`, `usePLC`, create/update/delete mutations + `PLCRow`/`PLCTag` types |
| `frontend/src/pages/PLCs.tsx` | Create | List + create/edit/delete UI (RHF + Zod), admin-only |
| `frontend/src/router.tsx` | Modify | Add `/plcs` route with `requiredRole: "admin"` |
| `docs/api/openapi.yaml` | Modify | Document the 5 `/api/plcs` endpoints |

## Frontend (high-level)

- **Route**: `/plcs`, `requiredRole: "admin"` in `router.tsx` — identical gating to `/users`.
  `ProtectedRoute` already enforces role; server enforces it too.
- **Hooks** (`useApi.ts`): `usePLCs()` (TanStack `useQuery`, key `["plcs"]`),
  `usePLC(name)`, and mutation helpers calling `apiFetch` against `POST/PUT/DELETE /api/plcs[...]`,
  invalidating `["plcs"]` on success. Mirrors `useUsers` + the Users mutation pattern.
- **Page** (`PLCs.tsx`): a `Card` + `Table` listing PLCs (name, address, scanRate, tag count); a
  create/edit form using `react-hook-form` + `zodResolver`. Zod schema mirrors `ValidatePLC`
  (name/address required, durations optional-but-valid, slot 0–15, tags with name+type from the
  Sparkplug enum + a `writable` checkbox). Delete via `AlertDialog` confirmation. Tag rows are a
  dynamic field array. `UnavailableBanner` on 404/503 like other pages.
- The `writable` checkbox is shown and persisted but documented in-UI as "reserved (not enforced)".

## Testing Strategy

| Layer | What to Test | Approach / Seam |
|-------|-------------|-----------------|
| Unit — store | CRUD, tags round-trip, `IsEmpty`, `Seed` idempotency, cascade delete, duplicate→`ErrPLCAlreadyExists`, rename collision | `plcstore.Open(ctx, ":memory:")` or temp file — same seam as `auth.OpenUserStore`. No fakes needed |
| Unit — validation | Each `ValidatePLC` rule (address, durations, slot range, tag name/type/Sparkplug) | Pure function; table-driven |
| Unit — manager | scanRate edit drains+restarts worker; unchanged PLC untouched; add/remove still work; reload over empty manager | Inject fake `DriverFactory` (already exists per `manager.go`); count Connect/Close calls per worker |
| Integration — API | CRUD happy paths; 400 invalid; 404 missing; 409 duplicate/rename; RBAC 403 for viewer/operator; audit events emitted | `startAPITestServerWithOpts` + in-memory `plcstore.Store` + real `TokenService` + a fake `PLCManager` capturing `Reload` calls |
| Integration — read path | `GET /api/config/mappings` reflects store state immediately after a mutation (not stale `s.cfg`) | Mutate via store, GET, assert payload changed |
| Cmd | seed-on-empty fires once; restart does not re-seed; manager always constructed (zero-PLC boot) | Temp datadir; adjust existing `server_test.go` factory assertion |

Primary seams: (1) `plcstore.Open(":memory:")` for store isolation; (2) the existing
`DriverFactory` for manager reload tests (no real PLC); (3) a fake `PLCManager` interface impl that
records `Reload` invocations for handler tests; (4) `config.ValidatePLC` is a pure function.

## Migration / Rollout

Additive: new package, new handler file, new frontend route + contained edits. No schema migration
of existing DBs (new `plcs.db`). First boot of an upgraded binary seeds `plcs.db` from the existing
YAML `plcs:`, so behavior is unchanged for existing deployments until the operator mutates via UI.
Rollback = revert the merge commit(s); `plcs.db` is independent and can be deleted to force a clean
re-seed. `CGO_ENABLED=0` holds — pure-Go `modernc.org/sqlite` already in use, no new deps.

## Risks / Tradeoffs (locked)

| Risk | Mitigation / Decision |
|------|----------------------|
| Two-sources-of-truth (YAML vs store) confusion | Seed-only semantics documented; `onChange` and startup both source PLCs from the store; recovery = delete `plcs.db` |
| `Reload` diff misses a changed field | `reflect.DeepEqual` over the whole `config.PLC` (incl. `Tags`); strict-TDD coverage per field |
| Reload failure after a committed store write | Mutation HTTP response still 200/201/204 (store is truth); reload error logged WARN; self-heals on next reload/restart |
| `reflect.DeepEqual` on `Tags` order sensitivity | `List`/`Seed` return tags in a deterministic order (`ORDER BY plc_tags.id`/insertion); store-sourced configs compare stably; tag reordering is a legitimate "changed" anyway |
| Always-on empty manager changes a test expectation | Explicitly update `server_test.go`: assert the manager is constructed but holds zero workers on zero-PLC boot |
| FK cascade not enforced by default in `modernc.org/sqlite` | `PRAGMA foreign_keys = ON` in `migrate` + explicit child delete inside the delete tx |
| 400-line PR budget | Split PR1 backend / PR2 frontend (per proposal) |

## Open Questions

- [ ] Should `GET /api/config/mappings` surface the new `writable` field, or stay name/type only for
  backward compat? (Lean: keep `mappingResponse` as-is for compat; expose `writable` only on the new
  `/api/plcs` endpoints. The CRUD UI uses `/api/plcs`, not `/mappings`.)
