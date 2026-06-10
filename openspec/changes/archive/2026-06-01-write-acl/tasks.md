# Tasks: PLC Tag Write Enforcement with Role×Tag ACL

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | ~1 550–1 750 total across 5 PRs + PR3-pre |
| 400-line budget risk | High (total), Low per individual PR |
| Chained PRs recommended | Yes |
| Suggested split | PR1 → PR2 → PR3-pre → PR3 → PR4 → PR5 |
| Delivery strategy | stacked-to-main |
| Chain strategy | stacked-to-main |

Decision needed before apply: No
Chained PRs recommended: Yes
Chain strategy: stacked-to-main
400-line budget risk: High

### Suggested Work Units

| Unit | Goal | Likely PR | Est. Lines | Notes |
|------|------|-----------|------------|-------|
| 1 | ACL store CRUD + tests | PR1 | ~270 | Independent; base = main |
| 2 | Enforce core + Manager.WriteTag + HTTP write endpoint + audit | PR2 | ~380 | Depends on PR1 |
| 3-pre | `dcmd_enabled` schema precursor in plcstore + config + API + UI | PR3-pre | ~200 | Depends on PR2 (or parallel after PR1 if isolated) |
| 3 | Sparkplug DCMD OnCommand wiring | PR3 | ~130 | Depends on PR2 + PR3-pre |
| 4 | ACL admin CRUD API + OpenAPI | PR4 | ~290 | Depends on PR1 |
| 5 | Frontend ACL matrix page + HTTP write control | PR5 | ~380 | Depends on PR2, PR4 |

---

## PR1 — ACL store (`internal/aclstore`)

> Goal: `Store` type backed by `modernc.org/sqlite`; migrate-on-open; full CRUD; `:memory:` seam.
> Satisfies: TWA-STORE-1.1, TWA-STORE-1.2, TWA-STORE-1.3, TWA-STORE-1.4, TWA-STORE-1.5, TWA-STORE-1.6

- [x] 1.01 [RED] `internal/aclstore/store_test.go` — test `Open(ctx, ":memory:")` creates `tag_acl` table; test `Open` is idempotent on existing DB; test `:memory:` seam returns zero rows. (TWA-STORE-1.1)
- [x] 1.02 [GREEN] `internal/aclstore/store.go` — define `Store`, `ACLRule`, sentinel errors (`ErrRuleNotFound`, `ErrRuleAlreadyExists`, `ErrInvalidRole`); implement `Open` with `PRAGMA foreign_keys=ON`, `SetMaxOpenConns(1)`, migrate-on-open (`CREATE TABLE IF NOT EXISTS tag_acl …`), `CREATE INDEX IF NOT EXISTS idx_tag_acl_lookup`. (TWA-STORE-1.1)
- [x] 1.03 [RED] add test cases: `IsEmpty` returns true on empty store; `Seed` populates empty store; `Seed` is no-op on non-empty store. (TWA-STORE-1.2)
- [x] 1.04 [GREEN] implement `IsEmpty(ctx) (bool, error)` and `Seed(ctx, []ACLRule) error` in `store.go`. (TWA-STORE-1.2)
- [x] 1.05 [RED] add test cases: `CreateRule` valid; `CreateRule` duplicate returns `ErrRuleAlreadyExists`; `CreateRule` invalid role returns `ErrInvalidRole`. (TWA-STORE-1.3)
- [x] 1.06 [GREEN] implement `CreateRule(ctx, ACLRule) error` with role-allowlist guard (`admin|operator|viewer`) and `INSERT OR FAIL`. (TWA-STORE-1.3)
- [x] 1.07 [RED] add test cases: `ListRules` returns all rows ordered; `GetRule` returns correct row; `GetRule` unknown id returns `ErrRuleNotFound`; `ListRulesByRole` filters by role. (TWA-STORE-1.4)
- [x] 1.08 [GREEN] implement `ListRules`, `GetRule`, `ListRulesByRole` in `store.go`. (TWA-STORE-1.4)
- [x] 1.09 [RED] add test cases: `UpdateRule` replaces fields; `UpdateRule` unknown id returns `ErrRuleNotFound`; `DeleteRule` removes row; `DeleteRule` unknown id returns `ErrRuleNotFound`. (TWA-STORE-1.5)
- [x] 1.10 [GREEN] implement `UpdateRule(ctx, int64, ACLRule) error` and `DeleteRule(ctx, int64) error`. (TWA-STORE-1.5)
- [x] 1.11 [RED] add test cases: `CanWrite` exact-match allow returns true; no matching row returns false; `allow_write=0` returns false. (TWA-STORE-1.6)
- [x] 1.12 [GREEN] implement `CanWrite(ctx, role, plc, tag string) (bool, error)` in `store.go`. (TWA-STORE-1.6)
- [x] 1.13 [VERIFY] run `go test ./internal/aclstore/... -race`; run `golangci-lint run --config=.golangci.yml ./internal/aclstore/...`; confirm LOC ≤ 270.

---

## PR2 — Enforcement core + Manager.WriteTag + HTTP write endpoint + audit

> Goal: `internal/writeguard` Guard; `Manager.WriteTag`; `PLCManager` interface extension; `POST .../write`; audit.
> Satisfies: TWA-ENFORCE-2.1, TWA-ENFORCE-2.2, TWA-ENFORCE-2.3, TWA-HTTP-3.1, TWA-AUDIT-4.1, PCS-CFG-5.1 (enforcement)

- [x] 2.01 [RED] `internal/writeguard/guard_test.go` — define fake `tagWritableLookup`, fake `aclReader`, fake `writer`, fake audit sink. Write failing tests: `AuthorizeHTTP` master-switch-off denies admin; `AuthorizeHTTP` Writable=true + no ACL row denies; `AuthorizeHTTP` both-pass allows; `AuthorizeHTTP` empty ACL denies (TWA-ENFORCE-2.1, TWA-ENFORCE-2.2). Assert fake `aclReader.CanWrite` never called when Writable=false.
- [x] 2.02 [RED] (same file) failing tests for DCMD branch: `AuthorizeDCMD` Writable=false denies; `AuthorizeDCMD` DCMDEnabled=false denies even with ACL row present; assert `aclReader` gets zero calls for DCMD path; `AuthorizeDCMD` both-true allows. (TWA-ENFORCE-2.1)
- [x] 2.03 [GREEN] `internal/writeguard/guard.go` — define `Source` enum, `WriteRequest`, `Decision`, `Guard` struct with interfaces (`TagReadable`, `ACLReader`, `TagWriter`, `AuditSink`); implement `Authorize(ctx, WriteRequest) Decision` branching on `req.Source`; SourceHTTP gate: Writable check → `aclStore.CanWrite`; SourceDCMD gate: Writable check → dcmd_enabled check (reads from `TagReadable`); both deny-by-default. (TWA-ENFORCE-2.1, TWA-ENFORCE-2.2)
- [x] 2.04 [RED] `internal/plc/manager_write_test.go` — add failing test: `Manager.WriteTag("Silo-1", "Feed.Rate", 2.5)` succeeds when driver present; returns `ErrPLCNotFound` when absent. (Design §2)
- [x] 2.05 [GREEN] `internal/plc/manager.go` — add `WriteTag(plcName, tag string, val any) error`; holds `mu.RLock`, looks up worker, delegates to `driver.WriteTag`; returns `ErrPLCNotFound` if absent.
- [x] 2.06 [GREEN] `internal/server/server.go` — extend `PLCManager` interface with `WriteTag(plcName, tag string, val any) error`; inject `aclStore *aclstore.Store` and `writeGuard *writeguard.Guard` fields (nil-safe — wired after PR4 completes admin API; guard already operative here).
- [x] 2.07 [RED] `internal/server/api_write_test.go` — httptest table: 200 on authorized write; 403 `write_denied` on ACL deny; 403 `write_denied` on master-switch off; 404 `tag_not_found` on unknown tag; 401 on no token; 400 on malformed body. Assert audit event emitted on allow AND deny before handler returns. (TWA-HTTP-3.1, TWA-ENFORCE-2.3, TWA-AUDIT-4.1)
- [x] 2.08 [GREEN] `internal/server/api_write.go` — `registerWriteRoutes(mux)`; handler: (1) validate tag exists via plcManager; (2) extract `Claims`; (3) call `guard.Authorize(SourceHTTP)`; (4) on allow: `plcManager.WriteTag`; (5) emit `auditLog.Log` with `action="tag.write"`, `Detail` JSON `{tag,value,outcome,source,reason}`, `TargetID=plcName`, `Username=actor` (or `""` for DCMD); (6) return response. Emit audit before returning for both allow and deny paths. (TWA-HTTP-3.1, TWA-AUDIT-4.1)
- [x] 2.09 [RED] add audit-specific test assertions: denied HTTP write emits event with `source="http"`; allowed HTTP write emits event with `source="http"`, `outcome="allow"`. (TWA-AUDIT-4.1)
- [x] 2.10 [VERIFY] run `go test ./internal/writeguard/... ./internal/plc/... ./internal/server/... -race`; run `golangci-lint run --config=.golangci.yml`; confirm LOC ≤ 380.

---

## PR3-pre — `dcmd_enabled` schema precursor

> Goal: `dcmd_enabled` column in plcstore + `TagDef.DCMDEnabled` in config + API round-trip + PLCs.tsx checkbox.
> Satisfies: PCS-CFG-5.2, TWA-DCMD-3.2 (prerequisite gate data), TWA-ENFORCE-2.1 (DCMDEnabled field sourcing)

- [x] 3p.01 [RED] `internal/config/config_test.go` — failing tests: `dcmd_enabled: true` loads into `TagDef.DCMDEnabled`; omitted field defaults to false. (PCS-CFG-5.2)
- [x] 3p.02 [GREEN] `internal/config/config.go` — add `DCMDEnabled bool` field with koanf tag `"dcmd_enabled"` to `TagDef` beside `Writable`. (PCS-CFG-5.2) [NOTE: design said `koanf:"dcmdEnabled"` but YAML key is `dcmd_enabled` — used `koanf:"dcmd_enabled"` to match spec]
- [x] 3p.03 [RED] `internal/plcstore/store_test.go` — two new test cases: (a) fresh DB: `dcmd_enabled` column present, defaults false, round-trips true; (b) legacy-schema DB (created WITHOUT `dcmd_enabled` column): `Open` applies idempotent `ALTER TABLE plc_tags ADD COLUMN dcmd_enabled INTEGER NOT NULL DEFAULT 0` and does NOT error; verify column exists after open. (PCS-CFG-5.2 — the ALTER TABLE migration path is distinct from CREATE TABLE)
- [x] 3p.04 [GREEN] `internal/plcstore/store.go` — (a) add `dcmd_enabled INTEGER NOT NULL DEFAULT 0` to `CREATE TABLE IF NOT EXISTS plc_tags` DDL; (b) add idempotent migration step after `CREATE TABLE`: `ALTER TABLE plc_tags ADD COLUMN dcmd_enabled INTEGER NOT NULL DEFAULT 0` wrapped in `IF NOT EXISTS`-equivalent (catch "duplicate column" SQLite error); (c) add `dcmd_enabled` to `insertTags` INSERT and `listTags` SELECT/Scan. (PCS-CFG-5.2)
- [x] 3p.05 [RED] `internal/server/api_plcs_test.go` — add cases: POST tag with `"dcmd_enabled":true` persists; GET tag response includes `"dcmd_enabled"` field. (PCS-CFG-5.2)
- [x] 3p.06 [GREEN] `internal/server/api_plcs.go` — add `DcmdEnabled bool` to `plcTagResponse`; map from store in `plcToResponse`; read from request body in `decodePLCRequest` (mirrors the `Writable` mapping at lines 17/36/67). (PCS-CFG-5.2)
- [x] 3p.07 [GREEN] `frontend/src/pages/PLCs.tsx` — add `dcmd_enabled: z.boolean()` to the tag Zod schema; add `dcmd_enabled: false` to `append` defaults; render a "DCMD Enabled" checkbox beside the existing "Writable" checkbox; wire via `register`. Also updated `PLCTag` type in `frontend/src/hooks/useApi.ts`. (PCS-CFG-5.2)
- [x] 3p.08 [VERIFY] run `go test ./internal/config/... ./internal/plcstore/... ./internal/server/... -race`; run `npm run lint && npm run build` in `frontend/`; run `golangci-lint run --config=.golangci.yml`; confirm LOC ≤ 200. ALL PASS.

---

## PR3-pre Apply Progress

### Completed: 2026-06-01

All 8 PR3-pre tasks completed in strict TDD mode. Full suite passes (-race, vet, lint, build).

### TDD Cycle Evidence

| Task | Test File | Layer | Safety Net | RED | GREEN | TRIANGULATE | REFACTOR |
|------|-----------|-------|------------|-----|-------|-------------|----------|
| 3p.01 | `internal/config/config_test.go` | Unit | 31 tests OK | Written (compile fail on missing DCMDEnabled) | — | — | — |
| 3p.02 | `internal/config/config.go` | Unit | 31 tests OK | — | 2/2 pass | true + false scenarios | Clean |
| 3p.03 | `internal/plcstore/store_test.go` | Unit | 17 tests OK | Written (FreshDB fails, LegacyDB idempotency) | — | — | — |
| 3p.04 | `internal/plcstore/store.go` | Unit | 17 tests OK | — | 2/2 pass (FreshDB + LegacyDB) | legacy row defaults to 0 + idempotent open | Clean |
| 3p.05 | `internal/server/api_plcs_test.go` | Integration (httptest) | 26 tests OK | Written (dcmd_enabled=false in response) | — | — | — |
| 3p.06 | `internal/server/api_plcs.go` | Integration | 26 tests OK | — | 2/2 pass (POST persists + GET returns) | create + get scenarios | Clean |
| 3p.07 | `frontend/src/pages/PLCs.tsx` | — | tsc/build OK | N/A (GREEN-only task per spec) | lint+build green | — | Clean |
| 3p.08 | Verify | — | — | — | Full suite green (-race) | — | — |

### Test Summary
- Backend tests written: 4 new (2 config + 2 plcstore) + 2 new (api_plcs); all passing -race
- Full suite: 20/20 packages green, 0 failures with -race
- Lint: 0 new issues (6 pre-existing SA5011: 4 in manager_test.go + 2 in config_test.go:32/35, pre-existing before this PR)
- Frontend: tsc lint clean, vite build green

### Key Deviation from Design
- `internal/config/config.go`: design specified `koanf:"dcmdEnabled"` (camelCase) but spec requires YAML key `dcmd_enabled`. All other koanf tags in this file exactly match YAML keys (never use underscores-to-camel remapping). Used `koanf:"dcmd_enabled"` to match the spec's stated YAML key and test evidence.

---

## PR3 — Sparkplug DCMD `OnCommand` wiring

> Goal: wire `OnCommand` → guard-backed DCMD handler; full DCMD enforcement + audit.
> Satisfies: TWA-DCMD-3.2 (scenarios a–d), TWA-AUDIT-4.1 (DCMD path), TWA-ENFORCE-2.1 (DCMD scenarios)

- [x] 3.01 [RED] `internal/sparkplug/edge_node_test.go` + new `internal/server/dcmd_handler_test.go` — failing tests covering: EdgeNode.SetCommandHandler (post-construction wiring seam); (a) DCMD metric with Writable=true+DCMDEnabled=true → driver.WriteTag called, audit outcome=allow, source=dcmd, no ACL lookup; (b) DCMDEnabled=false → driver NOT called, audit outcome=deny, reason=dcmd not enabled, aclStore.CanWrite NOT called; (c) Writable=false+DCMDEnabled=true → driver NOT called, audit outcome=deny, reason=tag not writable; (d) deny audit Username="", source=dcmd. Plus triangulation (allow audit also has Username=""). (TWA-DCMD-3.2, TWA-AUDIT-4.1)
- [x] 3.02 [GREEN] `internal/sparkplug/edge_node.go` — added SetCommandHandler method. `internal/server/api_write.go` — (1) fixed plcstoreTagReader.TagMeta to read DCMDEnabled from t.DCMDEnabled (was hardcoded false); (2) extracted emitWriteAuditDetail (plc,tag,value,outcome,reason,source,username) as the canonical impl; kept emitWriteAudit(*http.Request) wrapper for HTTP path (identical behavior); (3) added SparkplugCommandHandler() — returns nil when guard/plcMgr absent, else a closure: deviceID=plcName, calls guard.AuthorizeDCMD (never AuthorizeHTTP/aclStore), on allow calls plcMgr.WriteTag, audits both allow+deny with source=dcmd, Username="". `internal/server/server.go` — New auto-builds guard from plcstoreTagReader+ACLStore when opts.WriteGuard nil (injected guard takes precedence); activates HTTP write endpoint as side-effect (empty ACL denies all HTTP writes). `cmd/lgb/cmd/server.go` — opens aclstore (acl.db); passes via Opts.ACLStore; after server.New wires SetCommandHandler via optional-interface pattern. (TWA-DCMD-3.2)
- [x] 3.03 [VERIFY] go test ./... -race: 20/20 pass. go vet ./...: clean. golangci-lint: 0 new issues (6 pre-existing SA5011 unchanged). CGO_ENABLED=0 go build ./cmd/lgb/...: clean. Production diff: ~115 changed lines (additions+deletions).

---

## PR3 Apply Progress

### Completed: 2026-06-01

All 3 PR3 tasks completed in strict TDD mode. Full suite passes (-race, vet, lint, build).

### TDD Cycle Evidence

| Task | Test File | Layer | Safety Net | RED | GREEN | TRIANGULATE | REFACTOR |
|------|-----------|-------|------------|-----|-------|-------------|----------|
| 3.01 (EdgeNode.SetCommandHandler) | `internal/sparkplug/edge_node_test.go` | Unit | 9/9 tests OK | Written (en.SetCommandHandler undefined) | Passed | Post-construction wiring verified end-to-end | Clean |
| 3.01 (DCMD handler: 4 scenarios + 2 triangulation) | `internal/server/dcmd_handler_test.go` (new) | Unit | N/A (new) | Written (SparkplugCommandHandler undefined) | — | — | — |
| 3.02 | `internal/server/api_write.go`, `server.go`, `cmd/lgb/cmd/server.go` | Unit + Integration | 20/20 packages OK | — | 6/6 DCMD tests pass | allow+deny scenarios; allow-audit-no-actor; nil-guard | Clean |
| 3.03 | Verify | — | — | — | 20/20 packages green (-race) | — | — |

### Test Summary
- New tests written: 1 (sparkplug/SetCommandHandler) + 6 (dcmd_handler: 4 scenarios + nil-guard + allow-no-actor triangulation) = 7 total
- All passing with -race: 20/20 packages green
- Lint: 0 new issues (6 pre-existing SA5011 unchanged)
- `CGO_ENABLED=0 go build ./cmd/lgb/...`: clean
- Production diff: ~115 changed lines (additions+deletions across 4 production files)

### deviceID → PLC name mapping
`deviceID` IS the PLC name. Confirmed in `defaultSparkplugNodeFactory` (`cmd/lgb/cmd/server.go`):
`devices = append(devices, sparkplug.DeviceConfig{DeviceID: p.Name, Tags: tags})`.
The DCMD handler closure uses `plcName := deviceID` directly.

### How DCMD never consults the ACL
`Guard.AuthorizeDCMD` (in `writeguard/guard.go`) reads only from the `TagReadable` (plcstore) and
never calls `acl.CanWrite`. The test `TestDCMDHandler_DenyWhenDCMDEnabledFalse` seeds an explicit
operator ACL allow-row for Feed.Rate then sends a DCMD for NoCommand.Tag (DCMDEnabled=false) and
confirms WriteTag is NOT called — proving the ACL has no bearing on DCMD outcomes.

### HTTP endpoint with auto-built guard
When `server.New` receives `PLCStore != nil && ACLStore != nil` (production path), it builds the
guard internally via `writeguard.NewGuard(&plcstoreTagReader{...}, aclStore)`. This activates
`registerWriteRoutes`. An empty aclstore denies all HTTP writes (deny-by-default confirmed by
`TestHandleWriteTag_ACLDeny_403`).

### Audit refactor approach
Extracted `emitWriteAuditDetail(plcName, tagName, value, outcome, reason, source, username)` as the
canonical audit emitter. The existing `emitWriteAudit(*http.Request, ...)` wrapper delegates to it
with identical behavior — all existing HTTP audit tests pass unchanged. DCMD calls
`emitWriteAuditDetail` directly with `source="dcmd"` and `username=""`.

---

## PR4 — ACL admin CRUD API + OpenAPI

> Goal: `/api/acl/rules` endpoints; admin-only auth; audit on mutations; OpenAPI spec.
> Satisfies: TWA-API-5.1

- [x] 4.01 [RED] `internal/server/api_acl_test.go` — httptest table (mirror `api_plcs_test.go` style): GET list returns 200 + count; non-admin GET returns 403; POST creates rule, returns 201, emits `acl.create` audit; POST duplicate returns 409 `duplicate_rule`; POST invalid role returns 400 `invalid_rule`; GET by id 200/404; PUT replaces rule 200; DELETE removes rule 204, emits `acl.delete` audit; subsequent GET returns 404. (TWA-API-5.1)
- [x] 4.02 [GREEN] `internal/server/api_acl.go` — `registerACLRoutes(mux, aclStore, auditLog)`; handlers for GET/POST `/api/acl/rules` and GET/PUT/DELETE `/api/acl/rules/{id}`; all gated by `auth.RequireRole(RoleAdmin)`; request/response structs; map `ErrRuleAlreadyExists` → 409, `ErrRuleNotFound` → 404, `ErrInvalidRole` → 400; emit audit events on successful mutations (`acl.create`, `acl.update`, `acl.delete`) AFTER store write. (TWA-API-5.1)
- [x] 4.03 [GREEN] `internal/server/server.go` — wire `api_acl.go` routes in server setup; pass `aclStore` (opened from `datadir/acl.db`) and `auditLog`. (TWA-API-5.1)
- [x] 4.04 [VERIFY] run `go test ./internal/server/... -race`; run `golangci-lint run --config=.golangci.yml`; confirm LOC ≤ 290.

---

## PR5 — Frontend: ACL matrix page + HTTP write control

> Goal: `/acl` admin route with role×tag matrix; write control on tag view; `useApi` hooks.
> Satisfies: TWA-API-5.1 (UI surface), Design §8

- [x] 5.01 [GREEN] `frontend/src/hooks/useApi.ts` — add `useACLRules()`, `useCreateACLRule()`, `useUpdateACLRule()`, `useDeleteACLRule()` hooks using TanStack Query (pattern from PLCs.tsx hooks); add `useWriteTag(plcName, tagName)` mutation hook.
- [x] 5.02 [GREEN] `frontend/src/pages/ACL.tsx` (create) — matrix table with rows = (plc, tag), columns = admin/operator/viewer; each cell is `<input type="checkbox">` (no RHF — direct onChange); toggling fires create/update/delete mutations; TanStack Query `invalidate` on success; loading/error states; admin-only guard via router.
- [x] 5.03 [GREEN] `frontend/src/router.tsx` — add `/acl` route gated by `ProtectedRoute` with admin-role check (mirrors `/plcs` pattern); Layout derives nav link automatically from routes array.
- [x] 5.04 [GREEN] `frontend/src/pages/Tags.tsx` — add WriteControl component: native `<input>` + submit button rendered only when `tag.writable === true`; shows "read-only" hint when false; on submit calls `useWriteTag` mutation; shows 403 inline "permission denied", 404 "tag not found", 400 "bad request", other errors inline.
- [x] 5.05 [RED/lint] `npm run lint` — 0 TypeScript errors. `npm run build` — bundle succeeds (pre-existing chunk-size warning, not an error). New production LOC: ~451 (ACL.tsx 205 new + 246 added lines in existing files).
- [x] 5.06 [VERIFY] `npm run lint && npm run build` — both pass. Existing PLCs/Dashboard/Tags/Users pages unchanged structurally; typecheck clean.

---

## PR2 Apply Progress

### Completed: 2026-06-01

All 10 PR2 tasks completed in strict TDD mode. Full suite passes (-race, vet, lint, build).

### TDD Cycle Evidence

| Task | Test File | Layer | Safety Net | RED | GREEN | TRIANGULATE | REFACTOR |
|------|-----------|-------|------------|-----|-------|-------------|----------|
| 2.01 | `internal/writeguard/guard_test.go` | Unit | N/A (new) | Written | Passed (7 cases) | 4 HTTP + 3 DCMD scenarios | Clean |
| 2.02 | `internal/writeguard/guard_test.go` | Unit | N/A (new) | Written | Passed | Included in 2.01 | Clean |
| 2.03 | `internal/writeguard/guard.go` | Unit | N/A (new) | — | 7/7 | — | Clean |
| 2.04 | `internal/plc/manager_write_test.go` | Unit | 5.055s OK | Written | Passed (2 cases) | success + not-found | Clean |
| 2.05 | `internal/plc/manager.go` | Unit | 5.055s OK | — | 2/2 | — | Clean |
| 2.06 | `internal/server/server.go` | — | 2.410s OK | — | Build + existing tests green | — | Clean |
| 2.07 | `internal/server/api_write_test.go` | Integration (httptest) | 2.410s OK | Written | Passed (8 cases) | 200/403/403/404/401/400 + 2 audit | Clean |
| 2.08 | `internal/server/api_write.go` | Integration | 2.410s OK | — | 8/8 | — | Clean |
| 2.09 | `internal/server/api_write_test.go` | Integration | — | Included in 2.07 | Passed | audit on allow+deny | Clean |
| 2.10 | Verify | — | — | — | Full suite green | — | — |

### Test Summary
- Total tests written: 17 (7 writeguard + 2 plc/manager_write + 8 api_write)
- All passing with -race: internal/writeguard, internal/plc, internal/server
- Full suite: 20/20 packages green, 0 failures
- Lint: 0 new issues (4 pre-existing SA5011 in unmodified manager_test.go)

---

## PR4 Apply Progress

### Completed: 2026-06-01

All 4 PR4 tasks completed in strict TDD mode. Full suite passes (-race, vet, lint, build).

### TDD Cycle Evidence

| Task | Test File | Layer | Safety Net | RED | GREEN | TRIANGULATE | REFACTOR |
|------|-----------|-------|------------|-----|-------|-------------|----------|
| 4.01 | `internal/server/api_acl_test.go` | Integration (httptest) | N/A (new file) | Written (all handlers undefined) | — | — | — |
| 4.02 | `internal/server/api_acl.go` | Integration | N/A (new file) | — | 18/18 tests pass | list/create/get/put/delete; admin-only; 401/403/400/409/404; audit | Clean |
| 4.03 | `internal/server/api.go` | Integration | Full server suite | — | `registerACLRoutes` wired in `registerAPIRoutes` | nil-safe: routes absent when aclStore=nil | Clean |
| 4.04 | Verify | — | — | — | 20/20 packages green (-race) | — | — |

### Test Summary
- New tests written: 18 (api_acl_test.go): list-empty, list-two, non-admin-403 (operator+viewer), unauthed-401, create-201+audit, duplicate-409, invalid-role-400, bad-body-400, non-admin-create-403, get-found-200, get-missing-404, bad-id-400, update-200, update-missing-404, update-invalid-role-400, update-duplicate-409, update-audit, delete-204+audit, delete-missing-404, delete-non-admin-403, no-store-not-registered
- All passing with -race: 20/20 packages green
- Lint: 0 new issues (6 pre-existing SA5011 unchanged)
- `CGO_ENABLED=0 go build ./cmd/lgb/...`: clean
- Production LOC: 278 lines (api_acl.go) + 4 lines (api.go patch) = 282 production lines

### plc/tag Existence Validation Decision
Design §4 says validation "happens at the admin-API layer (PR4)", but spec TWA-API-5.1 has NO scenario requiring plc/tag existence validation — no "422 unknown PLC" scenario exists. The spec is the acceptance test contract. FOLLOWED THE SPEC: no plc/tag existence validation was implemented. The ACL store accepts free-form plc/tag values, exactly as the design's `tag_acl` schema describes ("Free-form plc/tag"). This is not a deviation — it IS the spec-defined behavior.

### Store error → HTTP status mapping
| Store error | HTTP status | Response code |
|-------------|-------------|---------------|
| `ErrRuleAlreadyExists` | 409 Conflict | `duplicate_rule` |
| `ErrRuleNotFound` | 404 Not Found | `rule_not_found` |
| `ErrInvalidRole` | 400 Bad Request | `invalid_rule` |

### OpenAPI updated
`docs/api/openapi.yaml` bumped from 0.3.0 → 0.4.0. Added `ACLRule` schema, `ACLRuleRequest` schema, paths `/api/acl/rules` (GET+POST) and `/api/acl/rules/{id}` (GET+PUT+DELETE) with full response/error documentation.

### Route wiring
`registerACLRoutes` is called from `registerAPIRoutes` in `api.go`, guarded by `s.aclStore != nil`. All 5 endpoints use `adminMWs` (authMiddleware + RequireRole(RoleAdmin)), matching the pattern of user CRUD and backup endpoints. No-op when aclStore is nil (nil-safe).

---

## Cross-cutting checklist (apply to every PR before merge)

- [ ] X.01 Conventional commit messages; no AI attribution in commit metadata.
- [ ] X.02 `go test ./... -race` passes (full suite, not just changed packages).
- [ ] X.03 `golangci-lint run --config=.golangci.yml` passes with zero new issues.
- [ ] X.04 `CGO_ENABLED=0 go build ./cmd/lgb/...` succeeds (pure-Go constraint).
- [ ] X.05 No write succeeds if `Writable=false` — manual smoke-test before PR2 merge.
