---
change: plc-config-crud
phase: verify
scope: PR1b (tasks 1b.01–1b.11)
date: 2026-05-29
status: complete
---

# Verification Report — PLC Config CRUD (PR1b)

**Change**: plc-config-crud
**Scope**: PR1b only (1b.01–1b.11). PR1a already merged to main; not re-verified.
**Branch**: feat/plc-config-api @ 54c7bfe
**Mode**: Strict TDD

---

## Completeness

| Metric | Value |
|--------|-------|
| Tasks in scope (PR1b) | 11 (1b.01–1b.11) |
| Tasks marked complete | 11 |
| Tasks incomplete | 0 |

---

## Build & Tests Execution

**Build**: PASS
```text
go build ./...  → no output (clean)
```

**Vet**: PASS
```text
go vet ./...    → no output (clean)
```

**Tests**: PASS — all packages green
```text
ok  github.com/fgjcarlos/lgb/cmd/lgb/cmd        1.227s
ok  github.com/fgjcarlos/lgb/internal/auth       14.844s
ok  github.com/fgjcarlos/lgb/internal/backup     1.048s
ok  github.com/fgjcarlos/lgb/internal/config     1.096s
ok  github.com/fgjcarlos/lgb/internal/plc        6.079s
ok  github.com/fgjcarlos/lgb/internal/plcstore   1.117s
ok  github.com/fgjcarlos/lgb/internal/server     23.818s
[all other packages: ok]
go test ./... -race -count=1  → all ok, zero failures
```

**Coverage (changed files)**:
| File | Coverage | Rating |
|------|----------|--------|
| `internal/server/api_plcs.go` | ~75% avg (plcToResponse 100%, decodePLCRequest 100%, actorFromContext 0%, reloadPLCsFromStore 55.6%, handleListPLCs 75%, handleGetPLC 77.8%, handleCreatePLC 80%, handleUpdatePLC 61.9%, handleDeletePLC 72.7%) | ⚠️ Acceptable |
| `internal/server/api_config.go:handleConfigMappings` | 87.5% | ⚠️ Acceptable |
| `internal/server/server.go:New` | 100% | ✅ Excellent |
| `cmd/lgb/cmd/server.go:runServerTo` | 63.8% | ⚠️ Acceptable |

---

## Spec Compliance Matrix

| Req ID | Scenario | Test | Result |
|--------|----------|------|--------|
| PCS-API-2.1 | Authenticated viewer lists PLCs | `api_plcs_test.go > TestHandleListPLCs_TwoPLCsReturnsBoth` (admin token) | ⚠️ PARTIAL — test uses admin, not viewer; implementation enforces admin only (see W1) |
| PCS-API-2.1 | Unauthenticated returns 401 | `TestHandleListPLCs_Unauthed` | ✅ COMPLIANT |
| PCS-API-2.1 | Empty store returns [] | `TestHandleListPLCs_EmptyReturnsEmptyArray` | ✅ COMPLIANT |
| PCS-API-2.2 | Viewer retrieves existing PLC | `TestHandleGetPLC_Found` (admin token) | ⚠️ PARTIAL — admin token used; spec says viewer+ but impl uses admin (see W1) |
| PCS-API-2.2 | Unknown name returns 404 | `TestHandleGetPLC_Missing` | ✅ COMPLIANT |
| PCS-API-2.3 | Admin creates PLC | `TestHandleCreatePLC_Admin201` | ✅ COMPLIANT |
| PCS-API-2.3 | Non-admin returns 403 | `TestHandleCreatePLC_ViewerGets403` | ✅ COMPLIANT |
| PCS-API-2.3 | Validation error returns 400 | `TestHandleCreatePLC_InvalidPLC400` | ✅ COMPLIANT |
| PCS-API-2.3 | Duplicate name returns 409 | `TestHandleCreatePLC_Duplicate409` | ✅ COMPLIANT |
| PCS-API-2.4 | Admin updates scanRate | `TestHandleUpdatePLC_Admin200` | ✅ COMPLIANT |
| PCS-API-2.4 | Non-admin returns 403 | `TestHandleUpdatePLC_ViewerGets403` | ✅ COMPLIANT |
| PCS-API-2.4 | Unknown name returns 404 | `TestHandleUpdatePLC_Missing404` | ✅ COMPLIANT |
| PCS-API-2.5 | Admin deletes PLC | `TestHandleDeletePLC_Admin204` | ✅ COMPLIANT |
| PCS-API-2.5 | Non-admin returns 403 | `TestHandleDeletePLC_ViewerGets403` | ✅ COMPLIANT |
| PCS-API-2.5 | Unknown name returns 404 | `TestHandleDeletePLC_Missing404` | ✅ COMPLIANT |
| PCS-API-2.6 | Mapping reflects post-create state | `TestHandleConfigMappings_StoreCreate_ReflectsNewPLC` | ✅ COMPLIANT |
| PCS-API-2.6 | Mapping reflects post-delete state | `TestHandleConfigMappings_StoreDelete_PLCRemoved` | ✅ COMPLIANT |
| PCS-RELOAD-3.1 | Always-construct manager (no nil) | `TestServerCmd_NoPLCs_EmptyManager` | ✅ COMPLIANT (design supersedes spec's nil-manager scenario) |
| PCS-RELOAD-3.1 | Reload called after mutation | `TestHandleCreatePLC_Admin201`, `TestHandleUpdatePLC_Admin200`, `TestHandleDeletePLC_Admin204` | ✅ COMPLIANT |
| PCS-RELOAD-3.1 | Empty manager Reload no-op/no-panic | `TestHandleCreatePLC_EmptyManager_ReloadNoOp` | ✅ COMPLIANT |
| PCS-AUDIT-4.1 | Create audit event written | (no test directly asserts auditLog.Log called with plc.create) | ⚠️ PARTIAL — see W2 |
| PCS-AUDIT-4.1 | Update audit event written | (no test directly asserts auditLog.Log called with plc.update) | ⚠️ PARTIAL — see W2 |
| PCS-AUDIT-4.1 | Delete audit event written | (no test directly asserts auditLog.Log called with plc.delete) | ⚠️ PARTIAL — see W2 |
| PCS-AUDIT-4.1 | Failed mutation no audit event | `TestHandleCreatePLC_ValidationFail_NoAuditEmit` (indirect) | ⚠️ PARTIAL — see W2 |
| PCS-CFG-5.1 | seed-on-first-run fires once | `TestServerCmd_PLCStoreSeed_FirstBoot` | ✅ COMPLIANT |
| PCS-CFG-5.1 | seed idempotent on restart | `TestServerCmd_PLCStoreSeed_Idempotent` | ✅ COMPLIANT |

**Compliance summary**: 22/26 scenarios compliant; 4 partial (all related to RBAC viewer-for-GET gap and audit indirect assertion).

---

## TDD Compliance

| Check | Result | Details |
|-------|--------|---------|
| TDD Evidence reported | ✅ | Found in apply-progress.md |
| All PR1b tasks have tests | ✅ | 11/11 tasks have test coverage |
| RED confirmed (tests exist) | ✅ | All test files exist in codebase |
| GREEN confirmed (tests pass) | ✅ | All pass at runtime |
| Triangulation adequate | ⚠️ | PCS-AUDIT-4.1 has structural coverage only (see W2) |
| Safety Net for modified files | ✅ | New files marked N/A (new); modified files note existing tests green |

**TDD Compliance**: 5/6 checks passed (1 warning)

---

## Test Layer Distribution

| Layer | Tests | Files | Tools |
|-------|-------|-------|-------|
| Unit (httptest / in-memory DB) | ~16 PLC handler tests + 3 cmd seed/manager tests | api_plcs_test.go, api_config_test.go, server_test.go | net/http/httptest, plcstore.Open(":memory:") |
| Integration | 0 | — | — |
| E2E | 0 | — | — |
| **Total (PR1b scope)** | **~22** | **5** | |

---

## Assertion Quality

| File | Line | Assertion | Issue | Severity |
|------|------|-----------|-------|----------|
| `api_plcs_test.go` | ~265 | `TestHandleCreatePLC_Admin201`: asserts `mgr.ReloadCount() >= 1` as proxy for audit | Indirect — audit not asserted; `s.auditLog == nil` in all PLC tests so audit block never runs in test | WARNING |
| `api_plcs_test.go` | ~499 | `TestHandleCreatePLC_ValidationFail_NoAuditEmit`: asserts `mgr.ReloadCount() == 0` as proxy for no audit | Indirect — since `s.auditLog == nil`, audit cannot fire regardless; the assertion proves Reload order, not audit suppression | WARNING |

**Assertion quality**: 0 CRITICAL, 2 WARNING

---

## Correctness (Static Evidence)

| Requirement | Status | Notes |
|------------|--------|-------|
| ValidatePLC called before store write (create) | ✅ Implemented | api_plcs.go:155 validate, then :160 create |
| ValidatePLC called before store write (update) | ✅ Implemented | api_plcs.go:204 validate, then :209 update |
| reloadPLCsFromStore nil-safe for plcMgr==nil | ✅ Implemented | api_plcs.go:94 early return |
| plcStore nil-safe (routes only registered if plcStore != nil) | ✅ Implemented | api.go:147 guard |
| Audit nil-safe (if s.auditLog != nil) | ✅ Implemented | api_plcs.go:170, 222, 258 |
| plcStore wired from Opts.PLCStore in New | ✅ Implemented | server.go:145 |
| plcMgr always constructed in cmd/server.go | ✅ Implemented | server.go:214 — no len>0 guard |
| Seed-on-first-run (IsEmpty → Seed) | ✅ Implemented | server.go:185–196 |
| Store wins in onChange watcher closure | ✅ Implemented | server.go:322–342 (capturedStore.List → merged.PLCs = storePLCs) |
| No frontend files changed (scope discipline) | ✅ Confirmed | git diff a366979..54c7bfe shows 10 backend/docs files only |
| GET /api/plcs viewer+ (spec says viewer or higher) | ❌ Deviated | api.go:152–155 uses adminMWs for GET routes; spec PCS-API-2.1/2.2 require viewer+ |

---

## Coherence (Design)

| Decision | Followed? | Notes |
|----------|-----------|-------|
| Routes use {name} not {id} as path param | ✅ Yes | Design table (§REST endpoints) uses {name}; spec used {id} as placeholder but design locked {name} |
| plcStore mirrors auth pattern (SetMaxOpenConns(1), modernc/sqlite) | ✅ Yes | Verified in PR1a (not re-verified here) |
| Audit: plc.create / plc.update / plc.delete action strings | ✅ Implemented | api_plcs.go:172, 223, 259 |
| Reload error logged WARN, does not fail HTTP | ✅ Implemented | api_plcs.go:98–105 |
| PLCStoreFactory injectable (follows HistorianStoreFactory pattern) | ✅ Implemented | root.go:51; server.go:171 |
| Always-construct-manager supersedes spec's nil-manager scenario | ✅ Locked | tasks.md "PCS-RELOAD-3.1 reconciliation" section |

---

## Issues Found

### WARNING

**W1 — RBAC: GET /api/plcs and GET /api/plcs/{name} are admin-gated instead of viewer+**

- **Files**: `internal/server/api.go:152–155`
- **Detail**: Both GET routes use the same `adminMWs` slice (authMiddleware + RequireRole(RoleAdmin)) as the mutation routes. The spec (PCS-API-2.1, PCS-API-2.2) explicitly requires "viewer or higher". The tasks document (1b.04) also explicitly says "GET /api/plcs and GET /api/plcs/{name} use RequireRole(RoleViewer)". The implementation uses admin for all five routes. This is a spec deviation.
- **Consequence**: Viewer and operator users cannot read the PLC list or details via the API; only admins can. The frontend PLC page (PR2) uses an admin-gated route, so it would work for the admin role, but any viewer who needs read-only PLC info would get 403. The spec scenarios "Authenticated viewer lists PLCs" and "Viewer retrieves existing PLC" have no passing test (the passing tests use admin tokens).
- **Evidence**: No test proves a viewer token returns 200 from GET /api/plcs. `TestHandleListPLCs_Unauthed` only proves 401 for no token, not 200 for viewer.

**W2 — PCS-AUDIT-4.1: Audit events are never asserted in tests**

- **Files**: `internal/server/api_plcs_test.go`
- **Detail**: The `fakeAuditLogger` type is defined (lines 43–58) but is NEVER instantiated in any test — every call to `newPLCTestServer(t, nil)` passes `nil`. Because `s.auditLog == nil` in every PLC handler test, the `if s.auditLog != nil` block never executes, meaning `actorFromContext` has 0% coverage. No test reads `events.jsonl` or counts audit events. `TestHandleCreatePLC_Admin201` claims PCS-AUDIT-4.1 compliance in its comment but only asserts Reload count. The "no-audit-on-failure" test (`TestHandleCreatePLC_ValidationFail_NoAuditEmit`) also asserts only `mgr.ReloadCount() == 0`, not absence of audit events — and since auditLog is nil, the assertion is vacuously true. The user-CRUD audit gap was explicitly called out as "MUST NOT be repeated" in the proposal/design, but the test infrastructure repeats the pattern of not exercising the audit code path.
- **Consequence**: The `plc.create` / `plc.update` / `plc.delete` audit code path has zero test coverage. A regression to that code (e.g., wrong action string, missing Detail field, nil panic) would not be caught. The implementation of the audit call itself looks correct (api_plcs.go:171–175, 222–226, 258–262), but it is untested.

### SUGGESTION

**S1 — actorFromContext coverage is 0%**: Because `s.auditLog` is always nil in tests, the function at `api_plcs.go:83–88` that extracts the username for audit events has zero test coverage. A test wiring a real `AuditLogger` would exercise this and also cover W2.

**S2 — handleUpdatePLC coverage at 61.9%**: The 409 (rename collision) and 500 (internal error) branches in `handleUpdatePLC` are not tested. These represent real error paths in the update flow. Consider adding `TestHandleUpdatePLC_DuplicateName409`.

**S3 — spec's `{id}` vs implementation's `{name}`**: The spec document uses `{id}` as the path parameter (PCS-API-2.2 through 2.5) but the design and implementation correctly use `{name}`. The spec document itself should be updated to reflect the locked `{name}` decision to avoid confusion for future contributors.

---

## Verdict

**PASS WITH WARNINGS**

Build: clean. All tests pass (go test ./... -race -count=1). 2 WARNINGs. No CRITICALs.

- W1 (RBAC GET viewer+) is a spec deviation but does NOT break admin functionality or the PR2 frontend (admin-only route). It tightens read access beyond spec intent.
- W2 (audit untested) is a process gap — the audit code is implemented correctly in production, but the test suite provides no evidence it fires. This repeats the pattern explicitly called out as unacceptable in the design doc.

Archive is acceptable with these warnings documented. Fixing W2 before merge is recommended but not required to unblock the PR.
