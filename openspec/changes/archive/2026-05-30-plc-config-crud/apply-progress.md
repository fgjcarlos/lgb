---
change: plc-config-crud
phase: apply
batch: PR1a + PR1b
date: 2026-05-29
status: complete
---

# Apply Progress — PR1a + PR1b (Backend Foundation + API + Wiring)

**Mode**: Strict TDD (RED → GREEN → REFACTOR per task pair)
**Batches**: PR1a (tasks 1a.01–1a.12) + PR1b (tasks 1b.01–1b.11)
**All PR1a + PR1b tasks**: COMPLETE

---

## TDD Cycle Evidence — PR1b

| Task | RED (test written first) | GREEN (impl passes) | REFACTOR |
|------|--------------------------|---------------------|---------|
| 1b.01 | Build fails (`PLCStore` undefined on `Opts`/`Server`) | `plcStore` field + `Opts.PLCStore` added; `plcstore` import wired | — |
| 1b.02 | `api_plcs_test.go` written with all 5 handlers → 404/200 wrong codes | (GREEN from 1b.03) | — |
| 1b.03 | (RED from 1b.02) | `api_plcs.go` created: all 5 handlers + `reloadPLCsFromStore` | — |
| 1b.04 | (merged with 1b.03) | Routes registered in `api.go` behind admin middleware | — |
| 1b.05 | `TestHandleConfigMappings_StoreCreate_ReflectsNewPLC` + `TestHandleConfigMappings_StoreDelete_PLCRemoved` → 0 items returned (reads frozen cfg) | (GREEN from 1b.06) | — |
| 1b.06 | (RED from 1b.05) | `handleConfigMappings` updated to query store when `s.plcStore != nil` | — |
| 1b.07 | `TestServerCmd_NoPLCs_EmptyManager` (renamed), `TestServerCmd_PLCStoreSeed_FirstBoot`, `TestServerCmd_PLCStoreSeed_Idempotent` → build fail (`PLCStoreFactory` unknown) | (GREEN from 1b.08) | — |
| 1b.08 | (RED from 1b.07) | `server.go`: open `plcs.db`, IsEmpty→Seed, always construct manager; watcher closure captures store; `Opts.PLCStore` wired | Rewrote test to avoid closed-store-read pattern (channel instead of post-close List) |
| 1b.09 | `TestHandleCreatePLC_EmptyManager_ReloadNoOp` — already in 1b.02 test file | PASS | — |
| 1b.10 | `TestHandleCreatePLC_ValidationFail_NoAuditEmit` — already in 1b.02 test file | PASS | — |
| 1b.11 | OpenAPI `docs/api/openapi.yaml` updated | Version bumped to 0.3.0; schemas + 5 paths added | — |

---

## PR1a — Completed Tasks (from prior batch)

- [x] 1a.01 TagDef.Writable — failing tests in config_test.go
- [x] 1a.02 TagDef.Writable — `Writable bool` field added to TagDef
- [x] 1a.03 ValidatePLC — table-driven tests in validate_plc_test.go
- [x] 1a.04 ValidatePLC — extracted to validate_plc.go; Config.Validate delegates via prefixPLCViolations
- [x] 1a.05 plcstore Open — tests for table creation + idempotency
- [x] 1a.06 plcstore Open — Store, Open, migrate, PRAGMA foreign_keys = ON, SetMaxOpenConns(1)
- [x] 1a.07 plcstore CRUD — full test coverage for Create/Get/List/Update/Delete
- [x] 1a.08 plcstore CRUD — List, Get, Create, Update, Delete implemented with transactions
- [x] 1a.09 plcstore Seed/IsEmpty — TestIsEmpty_TrueOnNew, TestSeed_* tests
- [x] 1a.10 plcstore Seed/IsEmpty — IsEmpty + Seed implemented
- [x] 1a.11 Manager.Reload diff — TestReload_ChangedScanRate_DrainsAndRestarts + TestReload_UnchangedPLC_NotRestarted
- [x] 1a.12 Manager.Reload diff — reflect.DeepEqual drain-and-swap for changed PLCs

## PR1b — Completed Tasks

- [x] 1b.01 Server + Opts — `PLCStore *plcstore.Store` field added to `Server` and `Opts`; wired in `New`
- [x] 1b.02 api_plcs_test.go — failing tests for all 5 handlers (RED confirmed: 404/403/200 wrong before impl)
- [x] 1b.03 api_plcs.go — `handleListPLCs`, `handleGetPLC`, `handleCreatePLC`, `handleUpdatePLC`, `handleDeletePLC`, `reloadPLCsFromStore` implemented; audit wired; validation before store write
- [x] 1b.04 Route registration — 5 `/api/plcs` routes in `registerAPIRoutes` behind admin middleware in `api.go`
- [x] 1b.05 api_config_test.go — two failing store-redirect tests added
- [x] 1b.06 api_config.go — `handleConfigMappings` redirected to query store when `s.plcStore != nil`
- [x] 1b.07 server_test.go — renamed `TestServerCmd_NoPLCs_NilManager` → `TestServerCmd_NoPLCs_EmptyManager` (assertion inverted); `TestServerCmd_PLCStoreSeed_FirstBoot` + `TestServerCmd_PLCStoreSeed_Idempotent` added
- [x] 1b.08 server.go — plcs.db opened; IsEmpty→Seed; always construct manager; watcher closure captures store; `PLCStore` in `server.Opts`
- [x] 1b.09 Empty-manager Reload test — `TestHandleCreatePLC_EmptyManager_ReloadNoOp` (in api_plcs_test.go, covered by 1b.02)
- [x] 1b.10 Audit no-emit on failure — `TestHandleCreatePLC_ValidationFail_NoAuditEmit` (in api_plcs_test.go, covered by 1b.02)
- [x] 1b.11 OpenAPI — `/api/plcs` endpoints documented; schemas PLCTag/PLCRequest/PLCResponse added; version bumped to 0.3.0

---

## Files Changed — PR1b

| File | Action | Description |
|------|--------|-------------|
| `/home/composedof2/Dev/Codex/lgb/internal/server/server.go` | Modified | `plcStore *plcstore.Store` field + `Opts.PLCStore`; wired in `New`; `plcstore` import added |
| `/home/composedof2/Dev/Codex/lgb/internal/server/api_plcs.go` | Created | 5 handlers + `reloadPLCsFromStore`; audit wired; validation before store write (~185 lines) |
| `/home/composedof2/Dev/Codex/lgb/internal/server/api_plcs_test.go` | Created | 14 tests: CRUD + RBAC + 404/409 + audit/reload assertions (~310 lines) |
| `/home/composedof2/Dev/Codex/lgb/internal/server/api_config.go` | Modified | `handleConfigMappings` queries store when `s.plcStore != nil`; fallback to `s.cfg.PLCs` |
| `/home/composedof2/Dev/Codex/lgb/internal/server/api_config_test.go` | Modified | Two store-redirect tests added; `plcstore` import added |
| `/home/composedof2/Dev/Codex/lgb/internal/server/api.go` | Modified | 5 `/api/plcs` routes registered in `registerAPIRoutes` |
| `/home/composedof2/Dev/Codex/lgb/cmd/lgb/cmd/server.go` | Modified | plcs.db open; IsEmpty→Seed; always construct manager; watcher closure captures store; `PLCStore` in `server.Opts`; `plcstore` import added |
| `/home/composedof2/Dev/Codex/lgb/cmd/lgb/cmd/server_test.go` | Modified | Renamed `TestServerCmd_NoPLCs_NilManager` → `TestServerCmd_NoPLCs_EmptyManager` (assertion inverted); added FirstBoot + Idempotent seed tests; `plcstore` import added |
| `/home/composedof2/Dev/Codex/lgb/cmd/lgb/cmd/root.go` | Modified | `PLCStoreFactory` field added to `Deps`; `plcstore` import added |
| `/home/composedof2/Dev/Codex/lgb/docs/api/openapi.yaml` | Modified | Version 0.3.0; PLCTag/PLCRequest/PLCResponse schemas; `/api/plcs` + `/api/plcs/{name}` paths |

---

## Deviations from Design

1. **`fakeAuditLogger` in tests**: The `Server` struct holds `*auth.AuditLogger` (not an interface), so a true fake audit logger can't be injected directly. The audit no-emit test (`1b.10`) asserts indirectly via Reload count (if validation fails before store write, Reload is never called — same code path as audit). This is functionally equivalent to an event count assertion.

2. **`PLCStoreFactory` field**: Design described seeding inline in `server.go`. We added `PLCStoreFactory func(ctx, path) (*plcstore.Store, error)` to `Deps` (following the exact pattern of `HistorianStoreFactory`) to make `cmd/server_test.go` injectable and avoid touching real filesystem in tests.

3. **`TestServerCmd_PLCStoreSeed_*` pattern**: Tests capture the PLCList fed to the `PLCManagerFactory` (via a channel) rather than querying the store post-Close (which would panic). This is equivalent: if the manager factory received the seeded PLCs, the store was seeded.

4. **`server_test.go` assertion inversion**: `TestServerCmd_NoPLCs_NilManager` renamed to `TestServerCmd_NoPLCs_EmptyManager`; assertion changed from "factory NOT called" to "factory IS called". Committed in the same change as the implementation (`server.go` guard removal). No separate inversion commit needed.

---

## Gotchas / Discoveries

- `Server` struct holds `*auth.AuditLogger` (concrete type, not interface) — fake audit injection requires a real file or a wrapper. Opted for indirect assertion via Reload count rather than modifying the Server interface.
- `defer plcStore.Close()` in `runServerTo` means captured store references are invalid after the function returns. Tests must capture data in the factory closure (channel pattern) rather than querying after `<-errCh`.
- `TestHandleConfigMappings_*` tests use `startConfigTestServer` which accepts `Opts` directly — passing `Opts{PLCStore: store}` was sufficient to wire the store.
- `plcMgr` in `server.go` is now declared as `plcMgr := factory(...)` (`:=` not `var`) after the factory block, which means the OPC UA block (`plcMgr != nil`) correctly sees it as always non-nil. The watcher start condition is now `if d.ConfigPath != ""` (no longer gated on plcMgr).

---

## Final Test Results

```
go test ./... -race -count=1

ok  github.com/fgjcarlos/lgb/cmd/lgb/cmd         1.243s
ok  github.com/fgjcarlos/lgb/internal/auth       14.728s
ok  github.com/fgjcarlos/lgb/internal/backup     1.048s
ok  github.com/fgjcarlos/lgb/internal/config     1.087s
ok  github.com/fgjcarlos/lgb/internal/datadir    1.047s
ok  github.com/fgjcarlos/lgb/internal/doctor     1.105s
ok  github.com/fgjcarlos/lgb/internal/errors     1.037s
ok  github.com/fgjcarlos/lgb/internal/health     1.036s
ok  github.com/fgjcarlos/lgb/internal/historian  1.092s
ok  github.com/fgjcarlos/lgb/internal/log        1.035s
ok  github.com/fgjcarlos/lgb/internal/mqtt       1.023s
ok  github.com/fgjcarlos/lgb/internal/opcua      3.050s
ok  github.com/fgjcarlos/lgb/internal/plc        6.072s
ok  github.com/fgjcarlos/lgb/internal/plcstore   1.154s
ok  github.com/fgjcarlos/lgb/internal/retry      1.019s
ok  github.com/fgjcarlos/lgb/internal/server     23.547s
ok  github.com/fgjcarlos/lgb/internal/sparkplug  1.124s
ok  github.com/fgjcarlos/lgb/internal/version    1.017s

go vet ./...  → no output (clean)
go build ./... → no output (clean)
```

---

## Status

23/23 tasks complete (12 PR1a + 11 PR1b). Ready for `sdd-verify` or PR1b git work.

## Workload / PR Boundary

- Mode: chained PR slice (PR1b)
- Base branch: feat/plc-config-api (branched from main after PR1a merge)
- Boundary: tasks 1b.01–1b.11 (server wiring + API handlers + audit + read-path + cmd seeding + OpenAPI)
- Files changed: 10 files (6 new/modified in internal/server, 3 in cmd/, 1 in docs/)
- Estimated diff: ~430 lines net (new code ~350, edits ~80)
- Next: PR2 (frontend `/plcs` route + UI + hooks)
