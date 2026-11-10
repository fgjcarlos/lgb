---
change: opcua-write-surface
phase: tasks
date: 2026-11-10
status: draft
---

# Tasks: OPC UA write surface

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | ~700 (additions + deletions) |
| 400-line budget risk | Medium |
| Chained PRs recommended | No — two reviewable commits on one branch |
| Suggested split | commit 1: guard + config + store → commit 2: OPC UA surface + wiring → commit 3: docs + frontend |
| Delivery strategy | ask-on-risk |
| Chain strategy | single branch |

Decision needed before apply: No
Chained PRs recommended: No
Chain strategy: single branch
400-line budget risk: Medium

## Work units

| Unit | Goal | Likely commit | ~Lines | Self-mergeable |
|------|------|---------------|--------|----------------|
| 1 | `writeguard.AuthorizeOPCUA`, `config.TagDef.OPCUAEnabled`, `plc_tags.opcua_write_enabled` + migration | `feat(writeguard): add OPC UA write gate` | ~250 | Yes — additive; existing tests stay green |
| 2 | OPC UA Write handler, node map, value conversion, audit, AccessLevel, cmd wiring, API DTO | `feat(opcua): add guarded write surface` | ~350 | Yes — new behaviour behind the guard; HTTP/DCMD untouched |
| 3 | OpenAPI schema, frontend parity, ADR index | `feat(ui): expose the OPC UA write opt-in` | ~100 | Yes — docs/UI only |

## Tasks

### Unit 1 — the gate and its flag

- [ ] 1.1 `internal/writeguard`: add `SourceOPCUA`, `TagMeta.OPCUAEnabled`, `AuthorizeOPCUA(ctx, plc, tag)`.
      RED: unit tests for master-switch deny, opt-in deny, allow, and "ACL is never consulted".
- [ ] 1.2 `internal/config`: `TagDef.OPCUAEnabled bool` with `koanf:"opcua_write_enabled"`.
      RED: YAML load test (explicit `true`, and default `false` when omitted).
- [ ] 1.3 `internal/plcstore`: `opcua_write_enabled` column, idempotent `ALTER TABLE`, round-trip.
      RED: fresh-DB test, legacy-DB migration test, round-trip test.

### Unit 2 — the surface

- [ ] 2.1 `internal/opcua`: node→(plc, tag) map built during address-space population.
- [ ] 2.2 `internal/opcua`: gateway Write handler registered **before** `srv.Start(ctx)`; unknown node →
      `StatusBadNodeIDUnknown`; non-Value attribute → `StatusBadAttributeIDInvalid`.
- [ ] 2.3 `internal/opcua`: value conversion to the tag's declared type; mismatch → `StatusBadTypeMismatch`.
- [ ] 2.4 `internal/opcua`: guard call, PLC write, audit (allow and deny), `source=opcua`.
- [ ] 2.5 `internal/opcua`: node `AccessLevel` reflects the effective opt-in.
- [ ] 2.6 `internal/opcua`: nil guard / nil writer → every write denied and audited.
- [ ] 2.7 `internal/server`: export the store→`TagReadable` adapter.
- [ ] 2.8 `cmd/lgb/cmd/server.go`: build one shared guard; pass it to the HTTP server and the OPC UA server.
- [ ] 2.9 `internal/server/api_plcs.go`: `opcua_write_enabled` in the response DTO and the request mapping.

### Unit 3 — surface parity

- [ ] 3.1 `docs/api/openapi.yaml`: `opcua_write_enabled` in the `PLCTag` schema.
- [ ] 3.2 `frontend/src`: schema, form default and per-tag checkbox for `opcua_write_enabled`.
- [ ] 3.3 `docs/adr/README.md`: index row for ADR-0010.

## Verification

- [ ] `go test ./... -race -count=1`
- [ ] `go vet ./...`
- [ ] `CGO_ENABLED=0 go build ./...`
