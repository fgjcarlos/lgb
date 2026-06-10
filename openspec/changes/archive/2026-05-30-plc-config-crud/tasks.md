---
change: plc-config-crud
phase: tasks
date: 2026-05-29
status: ready
---

# Tasks: PLC Configuration CRUD

## Reload-Signature Decision

**Resolved**: keep `Reload(ctx context.Context, cfg *config.Config) error` (existing signature).
After each mutation `reloadPLCsFromStore` calls `plcStore.List`, builds `&config.Config{PLCs: list}`,
and passes it to `plcMgr.Reload`. No call-site changes needed outside `server.go`/`server_test.go`.
The spec float of `[]config.PLC` is **rejected** — it would cascade across the watcher closure,
`PLCManager` interface, and all test fakes for a zero net gain.

**PCS-RELOAD-3.1 reconciliation**: the spec's "plcMgr == nil when zero PLCs" is superseded by
the design's locked decision: always construct an empty Manager. The empty-manager observable state
(zero workers) satisfies the spirit of PCS-RELOAD-3.1. `server_test.go` must be updated accordingly.

---

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | ~850–1 050 (new files: ~530; edits: ~200; frontend: ~250; OpenAPI: ~70) |
| 400-line budget risk | High |
| Chained PRs recommended | Yes |
| Suggested split | PR 1a → PR 1b → PR 2 |
| Delivery strategy | ask-on-risk |
| Chain strategy | stacked-to-main |

Decision needed before apply: Yes
Chained PRs recommended: Yes
Chain strategy: stacked-to-main
400-line budget risk: High

### Suggested Work Units

| Unit | Goal | Likely PR | Est. lines | Notes |
|------|------|-----------|------------|-------|
| 1a | Store + config + Reload diff (foundation) | PR 1a | ~400–480 | `internal/plcstore`, `config.TagDef.Writable`, `ValidatePLC`, `manager.Reload` field-level diff; self-contained; base = main |
| 1b | Server wiring + API handlers + audit + read-path + cmd seeding | PR 1b | ~370–450 | `server.go`, `api_plcs.go`, `api_config.go`, `api.go`, `cmd/server.go` + tests; base = PR 1a branch |
| 2 | Frontend `/plcs` route + UI + hooks | PR 2 | ~230–280 | `PLCs.tsx`, `useApi.ts`, `router.tsx`; base = PR 1b branch or main |

PR 1a is the minimum self-contained unit. PR 1b depends on PR 1a types. PR 2 depends on PR 1b endpoints.
If the team prefers two PRs, 1a+1b can be collapsed into a single backend PR (~800 lines) with `size:exception`.

---

## Phase 1a: Store + Config + Reload (PR 1a — base: main)

### [PCS-CFG-5.1] TagDef.Writable

- [x] 1a.01 **[RED]** Add two failing tests to `internal/config/config_test.go`: (a) YAML tag with `writable: true` → `cfg.PLCs[0].Tags[0].Writable == true`; (b) tag omitting `writable` → defaults `false`. *(PCS-CFG-5.1; ~15 lines)*
- [x] 1a.02 **[GREEN]** Add `Writable bool \`koanf:"writable"\`` to `TagDef` in `internal/config/config.go`. Make 1a.01 pass. *(~3 lines)*

### [PCS-CFG-5.1 / PLC-CFG-1.1] ValidatePLC extraction

- [x] 1a.03 **[RED]** Create `internal/config/validate_plc_test.go` with a table-driven test covering every `ValidatePLC` rule: empty address, invalid scanRate, invalid socketTimeout, slot<0, slot>15, empty tag name, empty tag type, invalid tag type (non-Sparkplug scalar), `writable:true` has no validation error, all-valid returns nil, multiple violations aggregated via `errors.Join`. *(PCS-STORE-1.7, PLC-CFG-1.1; ~60 lines)*
- [x] 1a.04 **[GREEN]** Create `internal/config/validate_plc.go` with `func ValidatePLC(p PLC) error` containing the extracted per-PLC rules. Refactor `Config.Validate` to call `ValidatePLC` inside `for i, plc := range c.PLCs`. Make 1a.03 pass + existing `config_test.go` still green. *(~50 lines)*

### [PCS-STORE-1.1] plcstore package — schema + open

- [x] 1a.05 **[RED]** Create `internal/plcstore/store_test.go`. Write `TestOpen_CreatesTables`: call `Open(ctx, t.TempDir()+"/plcs.db")`, query `sqlite_master` for both tables — `plcs` and `plc_tags`; assert both exist. Write `TestOpen_Idempotent`: open twice, assert no error and existing row survives. *(PCS-STORE-1.1; ~30 lines)*
- [x] 1a.06 **[GREEN]** Create `internal/plcstore/store.go` with `Store` type, sentinel errors (`ErrPLCNotFound`, `ErrPLCAlreadyExists`), `Open(ctx, path)` calling `sql.Open("sqlite", path)`, `SetMaxOpenConns(1)`, `PRAGMA foreign_keys = ON`, and `migrate` that issues both `CREATE TABLE IF NOT EXISTS` statements. Make 1a.05 pass. *(~80 lines)*

### [PCS-STORE-1.3 / 1.4 / 1.5 / 1.6] CRUD

- [x] 1a.07 **[RED]** Extend `internal/plcstore/store_test.go` with tests for all CRUD scenarios: `Create` happy path (PLC+tags round-trip via `List`), duplicate name → `ErrPLCAlreadyExists`, `Get` by name happy + missing → `ErrPLCNotFound`, `List` ordered-by-name with tags, `Update` scanRate persists + tags replaced atomically + missing → `ErrPLCNotFound`, `Delete` cascades tags + missing → `ErrPLCNotFound`. Use `Open(ctx, ":memory:")` seam. *(PCS-STORE-1.3–1.6; ~120 lines)*
- [x] 1a.08 **[GREEN]** Implement `List`, `Get`, `Create`, `Update`, `Delete` on `*Store` in `internal/plcstore/store.go`. `Create`/`Update` wrap parent + tag rows in a single `db.BeginTx`/`Commit`. `Update` deletes then re-inserts tags. `Delete` issues explicit child delete then parent delete inside one tx (belt-and-suspenders for cascade). Map `UNIQUE constraint failed` → `ErrPLCAlreadyExists`; `sql.ErrNoRows` → `ErrPLCNotFound`. Make 1a.07 pass. *(~160 lines)*

### [PCS-STORE-1.2] Seed / IsEmpty

- [x] 1a.09 **[RED]** Add `TestIsEmpty_TrueOnNew` and `TestSeed_*` tests: empty store returns `true`; `Seed(ctx, plcs)` with two PLCs → store has two rows with correct tags; second `Seed` call on non-empty store is a no-op (idempotent). *(PCS-STORE-1.2; ~30 lines)*
- [x] 1a.10 **[GREEN]** Add `IsEmpty(ctx) (bool, error)` (`SELECT COUNT(*) FROM plcs`) and `Seed(ctx, []config.PLC) error` (guard `IsEmpty`; bulk-create in one tx; empty slice → no-op) to `internal/plcstore/store.go`. Make 1a.09 pass. *(~35 lines)*

### [PLC-DRV-2.3] Manager.Reload — field-level diff

- [x] 1a.11 **[RED]** Add failing tests to `internal/plc/manager_test.go`: (a) `TestReload_ChangedScanRate_DrainsAndRestarts`: verify old worker drain + new worker start on scanRate edit; assert Connect/Close call counts via fake driver; (b) `TestReload_UnchangedPLC_NotRestarted`: two PLCs, one changes, assert only the changed worker's driver is closed; (c) ensure existing add/remove tests still pass. *(PLC-DRV-2.3; ~60 lines)*
- [x] 1a.12 **[GREEN]** Upgrade `Reload` in `internal/plc/manager.go`: change the drain-detection loop to use `reflect.DeepEqual(w.cfg, newCfgByName[name])` — if not-equal and name exists, include in `toDrain` AND mark for re-add. After wait+close, the add-new-workers loop then creates fresh workers for changed-and-removed entries. Make 1a.11 pass + existing reload tests green. *(~25 lines net change)*

**PR 1a total estimate: ~470 lines. Base branch: main.**

---

## Phase 1b: Server Wiring + API + Audit + Read-path + Cmd (PR 1b — base: PR 1a branch)

### [PCS-STORE-1.1 / server wiring] Server + Opts

- [x] 1b.01 Add `PLCStore *plcstore.Store` to `server.Opts` and `plcStore *plcstore.Store` field to `Server` struct in `internal/server/server.go`; wire in `New`. *(PCS-STORE-1.1; ~10 lines)*

### [PCS-API-2.1–2.5] /api/plcs handlers

- [x] 1b.02 **[RED]** Create `internal/server/api_plcs_test.go`. Write failing tests for all five handlers using `startAPITestServerWithOpts` + in-memory `plcstore.Store` + a fake `PLCManager` stub that records `Reload` calls: `GET /api/plcs` empty→`[]`, two PLCs→len=2, unauthed→401; `GET /api/plcs/{name}` found→200, missing→404; `POST /api/plcs` admin→201+audit, viewer→403, bad body→400 invalid_plc, duplicate→409; `PUT /api/plcs/{name}` admin→200+audit, viewer→403, missing→404; `DELETE /api/plcs/{name}` admin→204+audit, viewer→403, missing→404. Assert `Reload` called on each mutation. *(PCS-API-2.1–2.5, PCS-AUDIT-4.1; ~150 lines)*
- [x] 1b.03 **[GREEN]** Create `internal/server/api_plcs.go` with `handleListPLCs`, `handleGetPLC`, `handleCreatePLC`, `handleUpdatePLC`, `handleDeletePLC`, and `reloadPLCsFromStore`. Each mutation: decode body → `config.ValidatePLC` → store op → `auditLog.Log` (nil-safe) → `reloadPLCsFromStore`. Map sentinel errors to HTTP codes. Reload error: log WARN, do not fail HTTP. Make 1b.02 pass. *(~185 lines)*

### [PCS-API-2.1–2.5] Route registration

- [x] 1b.04 Register the five `/api/plcs` routes in `registerAPIRoutes` in `internal/server/api.go`, behind `withMiddleware(..., authMiddleware, RequireRole(RoleAdmin))`, guarded by `if s.plcStore != nil && s.authTokens != nil` (mirrors user-CRUD block). `GET /api/plcs` and `GET /api/plcs/{name}` use `RequireRole(RoleViewer)`. *(PCS-API-2.1–2.5; ~20 lines)*

### [PCS-API-2.6] Read-path redirect

- [x] 1b.05 **[RED]** Add two failing tests to `internal/server/api_config_test.go`: (a) after store `Create`, `GET /api/config/mappings` returns the new PLC without restart; (b) after store `Delete`, mapping no longer contains the PLC. *(PCS-API-2.6; ~30 lines)*
- [x] 1b.06 **[GREEN]** Update `handleConfigMappings` in `internal/server/api_config.go`: if `s.plcStore != nil` call `s.plcStore.List(r.Context())`, else fall back to `s.cfg.PLCs`. Make 1b.05 pass. *(~15 lines)*

### [PCS-STORE-1.2 / PCS-RELOAD-3.1] cmd/server.go seeding + always-manager

- [x] 1b.07 **[RED]** Add/update tests in `cmd/lgb/cmd/server_test.go`: (a) rename `TestServerCmd_NoPLCs_NilManager` → `TestServerCmd_NoPLCs_EmptyManager`: factory IS called (manager always constructed), assert factory called; (b) `TestServerCmd_PLCStoreSeed_FirstBoot`: temp datadir, one YAML PLC → store seeded, store `List` returns 1 entry; (c) `TestServerCmd_PLCStoreSeed_Idempotent`: pre-populate store before restart → `Seed` not called again (IsEmpty=false). *(PCS-STORE-1.2, PCS-RELOAD-3.1; ~60 lines)*
- [x] 1b.08 **[GREEN]** Update `cmd/lgb/cmd/server.go`: (a) open `plcStore` from `resolvedPath/plcs.db`; (b) `IsEmpty → Seed(cfg.PLCs)`; (c) `storePLCs := plcStore.List(ctx)`; (d) build `liveCfg := *cfg; liveCfg.PLCs = storePLCs`; (e) drop `if len(cfg.PLCs) > 0` guard — always call `factory(&liveCfg, tagCb)`; (f) refactor `onChange` closure to capture `plcStore`, call `plcStore.List` for PLCs, merge into `newCfg`. Pass `PLCStore` in `server.Opts`. Make 1b.07 pass. *(~60 lines net change)*

### [PCS-API-2.3 scenario: zero-PLC] Fake PLCManager in api_plcs_test.go

- [x] 1b.09 **[RED]** Add test for `POST /api/plcs` when `plcMgr` wraps an empty-worker manager: assert Reload is called and returns nil (no panic). Satisfies PCS-RELOAD-3.1 observable invariant. *(PCS-RELOAD-3.1; ~15 lines)*

### [PCS-AUDIT-4.1] Audit no-emit on failure

- [x] 1b.10 **[RED]** Add test in `internal/server/api_plcs_test.go`: failed `POST /api/plcs` (validation error, 400) → assert `auditLog` event count unchanged. *(PCS-AUDIT-4.1; ~15 lines)*

### OpenAPI

- [x] 1b.11 Add 5 `/api/plcs` endpoint definitions to `docs/api/openapi.yaml`: schemas for `PLCRequest`/`PLCResponse`/`PLCTag`; all verbs with status codes matching spec table. *(~70 lines)*

**PR 1b total estimate: ~430 lines. Base branch: PR 1a branch.**

---

## Phase 2: Frontend — /plcs route + UI + hooks (PR 2 — base: PR 1b branch)

### Frontend hooks

- [x] 2.01 Add `PLCRow` and `PLCTag` TypeScript types and `usePLCs()`, `usePLC(name)`, `useCreatePLC()`, `useUpdatePLC()`, `useDeletePLC()` hooks to `frontend/src/hooks/useApi.ts` (TanStack Query, key `["plcs"]`; invalidate on mutation success; mirrors `useUsers` pattern). *(~80 lines)*

### Frontend page

- [x] 2.02 Create `frontend/src/pages/PLCs.tsx`: `Card` + `Table` listing PLCs (name, address, scanRate, tag count); a Sheet/modal with React Hook Form + Zod for create/edit (name required, address required, slot 0–15, durations optional-but-valid, tags as dynamic field array with name/type from Sparkplug enum + `writable` checkbox); `AlertDialog` for delete confirmation; `UnavailableBanner` on error; empty-state when `data = []`. *(~160 lines)* — used `Dialog` (no `Sheet` component in the kit); `useFieldArray` for tags; name disabled on edit (natural key is immutable).

### Frontend routing

- [x] 2.03 Add `/plcs` entry to `frontend/src/router.tsx` with `requiredRole: "admin"` (identical pattern to `/users`). *(~8 lines)*

### Frontend build verification

- [x] 2.04 Ran `npm run lint` (tsc --noEmit) + `npm run build` in `frontend/` — both green, bundle generated, no TS errors or Zod schema mismatches.

**PR 2 total estimate: ~250 lines. Base branch: PR 1b branch.**

---

## Task Dependency Graph

```
1a.01 → 1a.02 (TagDef.Writable — unblocks 1a.07 tag round-trip)
1a.03 → 1a.04 (ValidatePLC — unblocks 1b.02 handler validation)
1a.05 → 1a.06 (store open — unblocks 1a.07)
1a.07 → 1a.08 (CRUD — unblocks 1a.09)
1a.09 → 1a.10 (Seed/IsEmpty — unblocks 1b.07/1b.08)
1a.11 → 1a.12 (Reload diff — unblocks 1b.02 Reload assertion)
[all 1a tasks must land before any 1b task]

1b.01 (Opts wiring — must precede 1b.02/1b.03/1b.06)
1b.02 → 1b.03 (handlers RED→GREEN)
1b.03 → 1b.04 (routes after handlers exist)
1b.05 → 1b.06 (read-path RED→GREEN)
1b.07 → 1b.08 (cmd RED→GREEN)
1b.09, 1b.10 (parallel to 1b.07; depend on 1b.02 test file existing)
1b.11 (parallel to 1b.07-1b.10; depends only on API shape from 1b.03)
[all 1b tasks must land before PR 2]

2.01 → 2.02 → 2.03 → 2.04
```

---

## PR Chain Summary

| PR | Base | Tasks | Approx Lines | Notes |
|----|------|-------|--------------|-------|
| PR 1a | main | 1a.01–1a.12 | ~470 | Foundation: store, config, Reload diff |
| PR 1b | PR 1a branch | 1b.01–1b.11 | ~430 | API + wiring + audit + cmd + OpenAPI |
| PR 2 | PR 1b branch | 2.01–2.04 | ~250 | Frontend route + UI + hooks |
| **Total** | | **27 tasks** | **~1 150** | |

Chain strategy: **stacked-to-main** — PR 1a merges to main; PR 1b targets PR 1a branch
(shows only its own diff); PR 2 targets PR 1b branch. Only the final PR (or a merge PR)
targets main when the team is ready.
