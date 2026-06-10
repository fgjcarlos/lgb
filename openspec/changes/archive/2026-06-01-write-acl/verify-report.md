---
change: write-acl
phase: verify
date: 2026-06-01
verdict: PASS-WITH-WARNINGS
---

# Verification Report: write-acl

## Overall Verdict: PASS WITH WARNINGS

**1 CRITICAL, 3 WARNINGS, 2 SUGGESTIONS**

The implementation is structurally correct and all safety-critical invariants hold. The single CRITICAL is a documentation gap in the OpenAPI spec that shipped in the same change. All 5 safety invariants PASS with named passing tests. Every gate is clean.

---

## Gate Results

| Gate | Result |
|------|--------|
| `go test -race ./...` | PASS — 22 packages (20 with tests), 0 failures |
| `go vet ./...` | PASS — 0 issues |
| `golangci-lint run --config=.golangci.yml` | 6 issues — ALL pre-existing SA5011 (4 in `internal/plc/manager_test.go`, 2 in `internal/config/config_test.go`); 0 new issues |
| `CGO_ENABLED=0 go build ./cmd/lgb/...` | PASS — pure-Go constraint confirmed |
| `npm run lint` (frontend) | PASS — 0 TypeScript errors |
| `npm run build` (frontend) | PASS — pre-existing chunk-size warning only, not an error |

---

## Safety Invariant Checklist

**Invariant 1 — Writable=false denies every role including admin on BOTH surfaces**

STATUS: PASS

- HTTP: `TestAuthorizeHTTP_MasterSwitchOff_DeniesAdmin` (internal/writeguard/guard_test.go) — Writable=false, ACL would allow admin, result is deny; `acl.calls == 0` asserted.
- HTTP integration: `TestHandleWriteTag_MasterSwitchOff_403` (internal/server/api_write_test.go) — admin token + Writable=false tag → 403.
- DCMD: `TestDCMDHandler_DenyWhenWritableFalse` (internal/server/dcmd_handler_test.go) — Writable=false + DCMDEnabled=true → WriteTag NOT called, audit `outcome=deny reason=tag not writable source=dcmd`.
- Code path: `AuthorizeHTTP` line 109: `if !ok || !meta.Writable { return Decision{Allowed: false, Reason: "tag not writable"} }` — no role check before this gate.

**Invariant 2 — DCMD NEVER consults the role×tag ACL; structural proof AND test**

STATUS: PASS

- Structural: `AuthorizeDCMD` (internal/writeguard/guard.go lines 131-142) calls only `g.tags.TagMeta(...)` — there is NO call to `g.acl.CanWrite`. The `acl` field is unused in this function.
- Test: `TestAuthorizeDCMD_DCMDEnabledFalse_Denies` asserts `acl.calls == 0` for DCMD path even when `fakeACLReader.allow = true`.
- Integration test: `TestDCMDHandler_DenyWhenDCMDEnabledFalse` seeds an explicit operator ACL allow-row for Feed.Rate, then sends a DCMD for NoCommand.Tag (DCMDEnabled=false); WriteTag is NOT called — proving the ACL row has no bearing on DCMD outcomes.

**Invariant 3 — Deny-by-default: empty ACL denies all HTTP writes; no dcmd_enabled denies DCMD**

STATUS: PASS

- HTTP empty ACL: `TestAuthorizeHTTP_EmptyACL_Denies` (writeguard unit) and `TestHandleWriteTag_ACLDeny_403` (server integration — no ACL rule seeded).
- DCMD deny-by-default: `dcmd_enabled INTEGER NOT NULL DEFAULT 0` in plcstore DDL; `TestDCMDEnabled_FreshDB` confirms fresh DB has dcmd_enabled=false; DCMD gate requires both flags explicitly true.

**Invariant 4 — Every write attempt (allow AND deny) is audited on BOTH surfaces with source recorded**

STATUS: PASS

- HTTP deny: `TestHandleWriteTag_Deny_EmitsAuditEventWithSourceHTTP` — asserts `action=tag.write`, `source=http`, `outcome=deny`.
- HTTP allow: `TestHandleWriteTag_Allow_EmitsAuditEventWithSourceHTTP` — asserts `action=tag.write`, `source=http`, `outcome=allow`, `Username=op1`.
- DCMD allow: `TestDCMDHandler_AllowWhenBothFlagsTrue` — asserts `outcome=allow`, `source=dcmd`, `Username=""`.
- DCMD deny: `TestDCMDHandler_DenyAuditHasNoActor` — asserts `Username=""`, `source=dcmd`.
- Code: `emitWriteAuditDetail` (api_write.go:140) is nil-safe; called on BOTH allow and deny paths in both handlers before returning.

**Invariant 5 — Idempotent ALTER TABLE migration handles legacy DBs and is safe to run twice**

STATUS: PASS

- `TestDCMDEnabled_LegacyDB` (internal/plcstore/store_test.go:475) — creates a DB WITHOUT dcmd_enabled column using raw SQL, then calls `plcstore.Open` and asserts no error; verifies column exists and existing rows default to 0.
- Code: `plcstore.migrate` (store.go:102-106) runs `ALTER TABLE plc_tags ADD COLUMN dcmd_enabled INTEGER NOT NULL DEFAULT 0` and treats "duplicate column name" as a no-op success — idempotent by design.
- Both `TestDCMDEnabled_FreshDB` and `TestDCMDEnabled_LegacyDB` PASS with -race.

---

## CRITICAL Findings

### CRITICAL-1: POST write endpoint missing from OpenAPI spec (TWA-HTTP-3.1)

**File:** `docs/api/openapi.yaml`
**Evidence:** The paths section contains `GET /api/plcs`, `GET/PUT/DELETE /api/plcs/{name}`, and `GET+POST /api/acl/rules` — but `POST /api/plcs/{plc}/tags/{tag}/write` is absent. The spec tasks note said "OpenAPI" in PR4 scope but only ACL paths were documented.
**Impact:** External consumers (any client generated from the spec, integration teams) cannot discover the write endpoint. The spec description on line 10 still says `operator: viewer + write/control endpoints (reserved)`, which is stale — the endpoint is now live.
**Spec reference:** TWA-HTTP-3.1 specifies the endpoint exists; task 2.08 states the handler was implemented; no task explicitly called out adding the OpenAPI path for the write endpoint.
**Risk:** Documentation-only gap; the endpoint functions correctly.
**RESOLUTION:** Fixed by PR #56 before archive. OpenAPI bumped to v0.5.0; write endpoint path added; `writable` description updated; `dcmd_enabled` added to PLCTag schema.

---

## WARNING Findings

### WARNING-1: PLCTag OpenAPI schema — `writable` description is stale; `dcmd_enabled` absent

**File:** `docs/api/openapi.yaml` lines 239-242
**Evidence:** `PLCTag.writable` is described as "Stored but not enforced; reserved for future per-tag write gating." This was true BEFORE this change. After this change, `writable` IS enforced by `AuthorizeHTTP` and `AuthorizeDCMD` as Layer 1 master switch. Also, the `dcmd_enabled` field added in PR3-pre is absent from the `PLCTag` schema — the frontend and backend both support it but the OpenAPI spec has no knowledge of it.
**Spec reference:** PCS-CFG-5.1, PCS-CFG-5.2 both require round-trip in the API shape; the schema document doesn't reflect the current wire format.
**RESOLUTION:** Fixed by PR #56 before archive.

### WARNING-2: SetCommandHandler timing relies on undocumented ordering guarantee between server wiring and srv.Run

**File:** `cmd/lgb/cmd/server.go` lines 339-378
**Evidence:** `SetCommandHandler` (line 344) is called AFTER `server.New` (line 312) but BEFORE `srv.Run` (line 378). `srv.Run` internally calls `spNode.Start` which subscribes to DCMD topics. The `SetCommandHandler` docstring explicitly states "MUST be called before Start". The current ordering is correct and the wiring works. However, there is no compile-time or runtime assertion guarding against future refactoring that could move the `SetCommandHandler` call after `srv.Run`.
**Spec reference:** TWA-DCMD-3.2.
**Risk:** LOW — test coverage (`TestDCMDHandler_AllowWhenBothFlagsTrue`) would catch a regression, but there is no structural safety net.
**Status:** ACCEPTED — no structural fix at this time.

### WARNING-3: Audit `TargetID` not set on tag.write events; spec says "unused (0)" which is correct but surprising

**File:** `internal/server/api_write.go` line 148
**Evidence:** The spec (TWA-AUDIT-4.1) says `TargetID` is `int64`, unused (0), and PLC identity is carried in `Detail` as `plc=<plc>`. The implementation sets `TargetID` implicitly to 0 (zero value) since the `AuditEvent` is constructed without setting that field. This is COMPLIANT with the spec. The WARNING is that the audit log contains `"target_id":0` (due to `json:"target_id,omitempty"` — actually this field is omitempty, so 0 is omitted). The spec says "TargetID: unused (0)" but does not say whether it should be omitted or set to 0 explicitly. With `omitempty`, the field is absent in the JSON; spec says "0". This is a minor ambiguity, not a defect.
**Risk:** Minimal — the distinction matters only for log parsers that require the field to be present.
**Status:** ACCEPTED — spec updated to clarify "zero value, omitted via omitempty."

---

## SUGGESTION Findings

### SUGGESTION-1: No test for TWA-ENFORCE-2.3 — "no ACL lookup when tag not found"

**File:** `internal/server/api_write_test.go`
**Evidence:** `TestHandleWriteTag_UnknownTag_404` confirms 404 for unknown tag but does NOT assert that no ACL lookup was performed. The spec says "AND no ACL lookup is performed AND no audit event is emitted." The implementation IS correct (the tag lookup happens at step 2, ACL at step 3 — handler returns early at step 2), but there is no assertion that `aclStore.CanWrite` was never called nor that the audit log is empty. This is a triangulation gap, not a defect.

### SUGGESTION-2: TWA-DCMD-3.2 "drop silently — no MQTT error response" is not testable

**Evidence:** The spec says DCMD deny should "drop the command silently (no MQTT error response published)." The DCMD handler returns without publishing anything on deny, which is correct. However, there is no test asserting that the MQTT client's Publish method was NOT called on a DCMD deny path — the test environment uses a fake manager that records writes but does not intercept MQTT publishes. This is an inherent limitation of the test seam, not a defect.

---

## Spec Compliance Matrix (selected requirements)

| Req ID | Scenario | Status | Test |
|--------|----------|--------|------|
| TWA-STORE-1.1 | Table created, idempotent open, :memory: seam | PASS | TestOpen_CreatesTables, TestOpen_Idempotent, TestOpen_InMemory |
| TWA-STORE-1.2 | IsEmpty, Seed idempotent | PASS | aclstore_test.go |
| TWA-STORE-1.3 | CreateRule: valid, duplicate, invalid role | PASS | aclstore_test.go |
| TWA-STORE-1.4 | ListRules, GetRule, GetRule unknown → ErrNotFound | PASS | aclstore_test.go |
| TWA-STORE-1.5 | UpdateRule, DeleteRule | PASS | aclstore_test.go |
| TWA-STORE-1.6 | CanWrite: allow, no row, allow_write=0 | PASS | aclstore_test.go |
| TWA-ENFORCE-2.1 | HTTP master-switch off → admin denied, no ACL call | PASS | TestAuthorizeHTTP_MasterSwitchOff_DeniesAdmin |
| TWA-ENFORCE-2.1 | HTTP Writable+no-ACL → deny | PASS | TestAuthorizeHTTP_WritableTrue_NoACLRow_Denies |
| TWA-ENFORCE-2.1 | HTTP both layers pass → allow | PASS | TestAuthorizeHTTP_BothLayersPass_Allows |
| TWA-ENFORCE-2.1 | DCMD Writable=false → deny, no ACL call | PASS | TestAuthorizeDCMD_WritableFalse_Denies |
| TWA-ENFORCE-2.1 | DCMD DCMDEnabled=false → deny, no ACL call | PASS | TestAuthorizeDCMD_DCMDEnabledFalse_Denies |
| TWA-ENFORCE-2.1 | DCMD both-true → allow | PASS | TestAuthorizeDCMD_BothTrue_Allows |
| TWA-ENFORCE-2.2 | Empty ACL denies all HTTP writes | PASS | TestAuthorizeHTTP_EmptyACL_Denies |
| TWA-ENFORCE-2.3 | Unknown tag → 404 tag_not_found | PASS | TestHandleWriteTag_UnknownTag_404 |
| TWA-HTTP-3.1 | Authorized write → 200 | PASS | TestHandleWriteTag_AuthorizedOperator_200 |
| TWA-HTTP-3.1 | ACL deny → 403 write_denied | PASS | TestHandleWriteTag_ACLDeny_403 |
| TWA-HTTP-3.1 | Master-switch off → 403 write_denied | PASS | TestHandleWriteTag_MasterSwitchOff_403 |
| TWA-HTTP-3.1 | No token → 401 | PASS | TestHandleWriteTag_NoToken_401 |
| TWA-HTTP-3.1 | Bad body → 400 | PASS | TestHandleWriteTag_BadBody_400 |
| TWA-DCMD-3.2 | (a) both flags true → WriteTag called, audit allow/dcmd | PASS | TestDCMDHandler_AllowWhenBothFlagsTrue |
| TWA-DCMD-3.2 | (b) DCMDEnabled=false → no write, ACL irrelevant, audit deny/dcmd | PASS | TestDCMDHandler_DenyWhenDCMDEnabledFalse |
| TWA-DCMD-3.2 | (c) Writable=false → no write, audit deny/dcmd | PASS | TestDCMDHandler_DenyWhenWritableFalse |
| TWA-DCMD-3.2 | (d) deny audit Username="" source=dcmd | PASS | TestDCMDHandler_DenyAuditHasNoActor |
| TWA-AUDIT-4.1 | HTTP deny audit source=http | PASS | TestHandleWriteTag_Deny_EmitsAuditEventWithSourceHTTP |
| TWA-AUDIT-4.1 | HTTP allow audit source=http | PASS | TestHandleWriteTag_Allow_EmitsAuditEventWithSourceHTTP |
| TWA-AUDIT-4.1 | DCMD deny audit source=dcmd, Username="" | PASS | TestDCMDHandler_DenyAuditHasNoActor |
| TWA-API-5.1 | Admin lists rules | PASS | TestHandleListACLRules_TwoRulesReturnsBoth |
| TWA-API-5.1 | Non-admin → 403 | PASS | TestHandleListACLRules_NonAdminGets403 |
| TWA-API-5.1 | Admin creates rule + audit | PASS | TestHandleCreateACLRule_Admin201AndAudit |
| TWA-API-5.1 | Duplicate → 409 | PASS | TestHandleCreateACLRule_Duplicate409 |
| TWA-API-5.1 | Invalid role → 400 | PASS | TestHandleCreateACLRule_InvalidRole400 |
| TWA-API-5.1 | Admin deletes rule + audit | PASS | TestHandleDeleteACLRule_Admin204AndAudit |
| PCS-CFG-5.1 | writable field loads from YAML | PASS | config_test.go |
| PCS-CFG-5.2 | dcmd_enabled loads from YAML, defaults false | PASS | config_test.go |
| PCS-CFG-5.2 | dcmd_enabled round-tripped via API | PASS | api_plcs_test.go |
| PCS-CFG-5.2 | ALTER TABLE migration on legacy DB | PASS | TestDCMDEnabled_LegacyDB |

---

## Design Coherence

| Decision | Code State | Match? |
|----------|-----------|--------|
| writeguard package above plc/aclstore (no cycle) | `internal/writeguard/guard.go` imports only `context` + `internal/auth` | MATCH |
| Source enum is load-bearing (selects gate, not just label) | `AuthorizeHTTP` / `AuthorizeDCMD` are distinct functions; `Source` concept embedded in function selection | MATCH (note: design sketched single `Authorize(req)` but implementation uses two separate functions — functionally equivalent and arguably cleaner) |
| DCMD NEVER touches aclStore | `AuthorizeDCMD` has zero calls to `g.acl` | MATCH |
| Audit format: flat key=value string, NOT JSON inside Detail | `emitWriteAuditDetail` formats `plc=%s tag=%s value=%v outcome=%s source=%s reason=%s` | MATCH |
| dcmd_enabled on plc_tags (NOT separate aclstore table) | `plcstore` carries the column; `writeguard` reads it via `TagReadable.TagMeta` | MATCH |
| deny-by-default: empty ACL → HTTP deny; dcmd_enabled=false → DCMD deny | Confirmed by code and tests | MATCH |
| SetCommandHandler before spNode.Start | cmd/lgb/cmd/server.go lines 339-345 (before srv.Run at 378, which calls spNode.Start) | MATCH |
| deviceID IS the PLC name | `plcName := deviceID` in SparkplugCommandHandler; `DeviceID: p.Name` in defaultSparkplugNodeFactory | MATCH |

### Notable Design Deviation (accepted, documented in apply-progress)

The design specified a single `Authorize(ctx, WriteRequest) Decision` function branching on `req.Source`. The implementation instead exposes two separate top-level functions: `AuthorizeHTTP` and `AuthorizeDCMD`. This is a BETTER design than the sketch — the type signatures make it structurally impossible to call the wrong gate for a given source, and the Source enum is no longer needed as a runtime switch. The spec's behavioral requirements (TWA-ENFORCE-2.1) are fully satisfied.

### Notable Implementation Note (design said `koanf:"dcmdEnabled"`, spec/code uses `koanf:"dcmd_enabled"`)

Design §4b mentioned `koanf:"dcmdEnabled"` (camelCase). Implementation uses `koanf:"dcmd_enabled"` (snake_case, matching the YAML key and all other koanf tags in config.go). The spec stated YAML key `dcmd_enabled` and tests confirm it. Implementation is CORRECT; design note was in error.

---

## Task Completeness

All tasks across PR1, PR2, PR3-pre, PR3, PR4, and PR5 are marked `[x]` in tasks.md. Cross-cutting checklist items X.01-X.05 are marked `[ ]` (not checked off) — this is expected as they are pre-merge manual gates, not automated tasks. Code state confirms all are met: conventional commits used, -race passes, lint has 0 new issues, CGO_ENABLED=0 build clean, Writable=false enforcement verified.
