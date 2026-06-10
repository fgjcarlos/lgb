---
change: write-acl
phase: archive-report
date: 2026-06-01
status: archived
---

# Archive Report: write-acl

## Change Archived

**Change**: write-acl
**Archived to**: `openspec/changes/archive/2026-06-01-write-acl/`
**Verification verdict**: PASS-WITH-WARNINGS (1 CRITICAL resolved by PR #56 before archive)
**Mode**: Strict TDD
**Artifact store**: openspec (file-based)

---

## Change Summary

Built the first live write capability for the LGB gateway. Before this change, `Driver.WriteTag` was orphaned (never called in production) and Sparkplug `OnCommand` was nil (every DCMD dropped). This change delivers two independent write surfaces — HTTP REST and Sparkplug DCMD — each with its own two-layer enforcement gate and full audit coverage. All writes are safe-by-default: both surfaces require `tag.Writable=true` (master switch), then apply a surface-specific second gate before any driver call.

---

## Implementation PRs (7 total, all merged to main)

| PR | GitHub | Scope |
|----|--------|-------|
| PR1 | #50 | `internal/aclstore`: SQLite role×tag ACL store, full CRUD, IsEmpty/Seed, `:memory:` seam |
| PR2 | #51 | `internal/writeguard` Guard (`AuthorizeHTTP`/`AuthorizeDCMD`); `Manager.WriteTag`; `POST /api/plcs/{plc}/tags/{tag}/write` handler; audit emitter |
| PR3-pre | #52 | `dcmd_enabled` schema precursor: `TagDef.DCMDEnabled`, `plc_tags.dcmd_enabled` column + idempotent ALTER TABLE migration, `api_plcs` field, `PLCs.tsx` checkbox |
| PR3 | #53 | Sparkplug DCMD `OnCommand` wiring via `SetCommandHandler`; `SparkplugCommandHandler()` closure; DCMD enforcement + audit |
| PR4 | #54 | `/api/acl/rules` admin CRUD API (5 endpoints, admin-only, audit on mutations); OpenAPI updated to v0.4.0 |
| PR5 | #55 | Frontend: ACL matrix page (`/acl` route), `useACLRules`/mutation hooks, write control on tag view (`WriteControl` component) |
| Doc fix | #56 | OpenAPI: added `POST /api/plcs/{plc}/tags/{tag}/write` path (CRITICAL-1 from verify); bumped to v0.5.0; updated `writable` description; added `dcmd_enabled` to PLCTag schema |

---

## As-Built Architecture (Two-Layer / Two-Surface Model)

```
HTTP POST /api/plcs/{plc}/tags/{tag}/write     DCMD (MQTT, anonymous)
         │ Claims.Role                               │ deviceID,tag,val
         ▼ AuthorizeHTTP                             ▼ AuthorizeDCMD
   Gate 1: tag.Writable (plcstore) ─────────── Gate 1: tag.Writable (plcstore)
   Gate 2: aclStore.CanWrite(role,plc,tag)      Gate 2: dcmd_enabled (plcstore)
                          allow ◄──── Decision ────► deny
                            │                         │
                  Manager.WriteTag (driver)            │
                            └──── emitWriteAuditDetail ◄──┘  (both paths, both surfaces)
```

Key structural facts verified against merged code:

| Fact | File | Confirmed |
|------|------|-----------|
| `tag_acl` schema: `allow_write INTEGER NOT NULL DEFAULT 0`, `UNIQUE(role,plc,tag)` | `internal/aclstore/store.go:86-97` | Yes |
| `AuthorizeHTTP` and `AuthorizeDCMD` are two separate functions | `internal/writeguard/guard.go:107,131` | Yes |
| `AuthorizeDCMD` has zero calls to `g.acl` (never consults aclStore) | `internal/writeguard/guard.go:131-142` | Yes |
| Audit `Detail` is flat key=value, NOT JSON | `internal/server/api_write.go:144` | Yes |
| `TargetID` is zero (omitted from JSON via omitempty) | `internal/server/api_write.go:148-152` | Yes |
| `dcmd_enabled` on `plc_tags`, not a separate table | `internal/plcstore/store.go:91,102-106` | Yes |
| `SetCommandHandler` called after `server.New`, before `srv.Run` | `cmd/lgb/cmd/server.go:312,343-346,378` | Yes |
| `deviceID` IS the PLC name in DCMD handler | `internal/server/api_write.go:179` | Yes |
| koanf tag is `dcmd_enabled` (snake_case), not `dcmdEnabled` | `internal/config/config.go` | Yes |

---

## Reconciliations Applied During the Change

These are the points where the original proposal/design drifted from what was built, documented and corrected as the change progressed:

| # | Original plan | As-built truth | Where corrected |
|---|---------------|----------------|-----------------|
| 1 | Single `Authorize(ctx, WriteRequest)` with `Source` enum | Two separate functions: `AuthorizeHTTP` and `AuthorizeDCMD`; no Source enum in callers | Design coherence section of verify-report; main spec Purpose updated |
| 2 | DCMD uses same ACL path as HTTP (via `sparkplug-dcmd → operator` mapping) | DCMD NEVER consults the ACL; per-tag `dcmd_enabled` IS the authorization | Design §6 revised during apply; verified structurally |
| 3 | `allow_write DEFAULT 1` (inferred from early design) | `allow_write INTEGER NOT NULL DEFAULT 0` (deny-by-default confirmed in code) | Spec TWA-STORE-1.1 correctly has DEFAULT 0; main spec reflects this |
| 4 | Audit `Detail` as JSON object | Flat `key=value` string to avoid double-encoding (AuditEvent itself is JSON-encoded to events.jsonl) | Design §5 decision; audit format in TWA-AUDIT-4.1 |
| 5 | `TargetID` carries plc_id | `TargetID` unused (0 / omitted); PLC name in Detail as `plc=<name>` | TWA-AUDIT-4.1 |
| 6 | koanf tag `dcmd_enabled` as `koanf:"dcmdEnabled"` (design note) | `koanf:"dcmd_enabled"` (snake_case, matching all other koanf tags in config.go) | Documented in apply-progress (3p.02) and verify-report |
| 7 | PR4 wires production guard + HTTP write endpoint + DCMD | PR3 wired the guard and activated HTTP endpoint; PR4 was purely the admin CRUD API | Main spec describes end behavior, not PR sequencing |
| 8 | Frontend `value` field coerced to target type | Frontend sends value as string; gateway passes as-is without coercion | Accepted limitation; noted in TWA-HTTP-3.1 |

---

## Verify Verdict and Residual Items

**Verdict**: PASS-WITH-WARNINGS at verify time. All safety invariants PASS. CRITICAL-1 was fixed by PR #56 before archive.

**CRITICAL-1** (resolved by PR #56): `POST /api/plcs/{plc}/tags/{tag}/write` was absent from OpenAPI spec. Fixed: path added, `writable` description updated, `dcmd_enabled` added to PLCTag schema, version bumped to v0.5.0.

**Accepted residual items** (recorded, not blocking):

| Item | Description | Risk |
|------|-------------|------|
| WARNING-2 | `SetCommandHandler` must be called before `srv.Run`; no compile-time guard. Current ordering correct; regression caught by `TestDCMDHandler_AllowWhenBothFlagsTrue`. | Low |
| WARNING-3 | `TargetID` is 0/omitted on tag.write audit events (per spec, field is unused); `omitempty` means field is absent from JSON rather than present as 0. Minor ambiguity only. | Minimal |
| SUGGESTION-1 | `TestHandleWriteTag_UnknownTag_404` does not assert that `aclStore.CanWrite` was never called (no audit emitted assertion). Implementation is correct; triangulation gap only. | None |
| SUGGESTION-2 | No test asserts MQTT Publish was NOT called on DCMD deny. Inherent test-seam limitation; DCMD handler returns without publishing — correct behavior. | None |
| Frontend write-value string | Frontend sends value as string; no coercion to numeric target type. Write may fail at driver level for typed PLC tags. Future work. | Low |

---

## Specs Synced

| Domain | Action | Req IDs merged | Details |
|--------|--------|----------------|---------|
| `tag-write-acl` | Created (new capability) | TWA-STORE-1.1–1.6, TWA-ENFORCE-2.1–2.3, TWA-HTTP-3.1, TWA-DCMD-3.2, TWA-AUDIT-4.1, TWA-API-5.1 | Full spec at `openspec/specs/tag-write-acl/spec.md` — Purpose updated to reflect two-function enforcement model; TWA-AUDIT-4.1 TargetID clarified (omitted via omitempty, per spec "unused 0"); TWA-HTTP-3.1 note on write-value string limitation added; TWA-DCMD-3.2 note on SetCommandHandler ordering added; TWA-API-5.1 note on free-form plc/tag (no existence check) added |
| `plc` | Modified | PCS-CFG-5.1 (modified), PCS-CFG-5.2 (added) | Appended to `openspec/specs/plc/spec.md`. PCS-CFG-5.1 updated from "stored but not enforced" to "enforced master switch" with historical note. PCS-CFG-5.2 is new (dcmd_enabled, deny-by-default, co-located with writable on plc_tags, koanf snake_case note). |
| `config` | Modified | PCS-CFG-5.1 | Updated in `openspec/specs/config/spec.md`. Description changed from "forward-compat write-permission marker; stored but NOT enforced" to "engineering master switch for writes" with enforcement note and historical note. |

---

## Source of Truth Updated

- `openspec/specs/tag-write-acl/spec.md` — NEW capability spec (status: active)
- `openspec/specs/plc/spec.md` — PCS-CFG-5.1 (modified to enforced), PCS-CFG-5.2 (added)
- `openspec/specs/config/spec.md` — PCS-CFG-5.1 description updated

---

## Archive Contents

- `proposal.md`
- `design.md`
- `tasks.md` (all tasks [x]; cross-cutting checklist met: -race, lint, CGO_ENABLED=0, conventional commits)
- `verify-report.md` (PASS-WITH-WARNINGS; CRITICAL-1 resolved by PR #56 before archive)
- `specs/tag-write-acl/spec.md` (new capability delta — full spec)
- `specs/plc/spec.md` (modified delta — PCS-CFG-5.1 enforced + PCS-CFG-5.2 new)
- `archive-report.md` (this file)

---

## SDD Cycle Complete

The `write-acl` change has been fully planned, implemented (PRs #50–#56, all merged to main), verified, and archived. The LGB gateway now has a live write path — both HTTP REST and Sparkplug DCMD — with enforced two-layer authorization and complete audit coverage.

Ready for the next change.
