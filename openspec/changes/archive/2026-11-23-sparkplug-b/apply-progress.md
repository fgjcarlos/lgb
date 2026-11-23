---
change: sparkplug-b
phase: apply-progress
slice: 1
date: 2026-05-23
status: slice-1-complete
mode: Strict TDD
---

# Apply Progress: sparkplug-b — Slice 1

## Completed Tasks

### Slice 1 (feat/spb-proto-errors-config)

- [x] **T-1.01** `chore` — Vendored `sparkplug_b.proto` to `proto/`, generated Go types to `internal/sparkplug/pb/`, updated Makefile generate target, added `google.golang.org/protobuf` dep
- [x] **T-1.02** `test` — [RED] Extended `internal/errors/errors_test.go` with 2 new test functions for MQTT/Sparkplug sentinels (distinct, non-nil, wrapping)
- [x] **T-1.03** `impl` — [GREEN] Added 4 sentinels: `ErrMQTTConnect`, `ErrMQTTPublish`, `ErrMQTTSubscribe`, `ErrSparkplugEncode`
- [x] **T-1.04** `test` — [RED] Extended `internal/config/config_test.go` with 8 new test functions for MQTT Sparkplug fields and PLC tags validation
- [x] **T-1.05** `impl` — [GREEN] Extended `MQTTSection` with 5 Sparkplug fields, added `TagDef` struct, extended `PLC` with `Tags`, added validation rules, added defaults in loader

## Files Changed

| File | Action | What Was Done |
|------|--------|---------------|
| `proto/sparkplug_b.proto` | Created | Vendored Sparkplug B v3.0.0 proto schema |
| `internal/sparkplug/pb/sparkplug_b.pb.go` | Created | Generated protobuf Go types (committed) |
| `Makefile` | Modified | Updated `generate` target for sparkplug proto path |
| `go.mod` | Modified | Added `google.golang.org/protobuf v1.36.11` |
| `go.sum` | Modified | Updated checksums |
| `internal/errors/errors.go` | Modified | Added MQTT + Sparkplug sentinel blocks |
| `internal/errors/errors_test.go` | Modified | Added 2 test functions (distinct/wrapping) |
| `internal/config/config.go` | Modified | Extended MQTTSection, added TagDef, PLC.Tags, validation rules, validSparkplugType |
| `internal/config/loader.go` | Modified | Added MQTT defaults (QoS=1, KeepAlive=30s, CleanSession=true) |
| `internal/config/config_test.go` | Modified | Added 8 test functions for Sparkplug config |
| `internal/config/testdata/sample.yaml` | Modified | Added Sparkplug MQTT fields + PLC tags |
| `internal/testutil/config.go` | Modified | Added GroupID/EdgeNodeID/QoS/KeepAlive/CleanSession to MinimalConfig |
| `cmd/lgb/cmd/testdata/sample.yaml` | Modified | Added groupID/edgeNodeID for validation |

## TDD Cycle Evidence

| Task | Test File | Layer | RED | GREEN | REFACTOR |
|------|-----------|-------|-----|-------|----------|
| T-1.01 | N/A (chore) | Build | N/A | `CGO_ENABLED=0 go build ./internal/sparkplug/pb/...` exit 0 | N/A |
| T-1.02 | `internal/errors/errors_test.go` | Unit | Compile error — sentinels undefined | N/A (test task) | N/A |
| T-1.03 | `internal/errors/errors_test.go` | Unit | N/A (impl task) | All errors tests pass | N/A |
| T-1.04 | `internal/config/config_test.go` | Unit | Compile error — struct fields undefined | N/A (test task) | N/A |
| T-1.05 | `internal/config/config_test.go` | Unit | N/A (impl task) | All config tests pass; also fixed testutil + cmd testdata | Extracted `validSparkplugType` helper |

## Build Gates Passed

- `go test -tags no_embed -race -count=1 ./...` — PASS (all 11 test packages)
- `CGO_ENABLED=0 go build -tags no_embed ./...` — exit 0

## Deviations from Design

1. **Design §7 shows `CleanSession *bool`** (pointer for default-true detection), but implementation uses `CleanSession bool` with a confmap default of `true`. This is simpler and matches the pattern used by all other boolean config fields in the project.

## Remaining Tasks

22 tasks remaining across Slices 2–6.

## Workload / PR Boundary

- Mode: chained PR slice (stacked-to-main)
- Current work unit: Unit 1 — Proto + errors + config
- Branch: `feat/spb-proto-errors-config` based on `main`
- Estimated review budget impact: ~150 lines changed (within budget)
