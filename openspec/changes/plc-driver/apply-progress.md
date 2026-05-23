---
change: plc-driver
phase: apply-progress
slice: 2
date: 2026-05-23
status: slice-2-complete
mode: Strict TDD
---

# Apply Progress: plc-driver — Slices 1 & 2

## Completed Tasks

### Slice 1 (feat/plc-driver-errors-config)

- [x] **T-1.01** `test` — [RED] PLC error sentinels tests written and confirmed failing
- [x] **T-1.02** `impl` — [GREEN] PLC error sentinels added to `internal/errors/errors.go`
- [x] **T-1.03** `test` — [RED] PLC config struct tests written and confirmed failing
- [x] **T-1.04** `impl` — [GREEN] PLC config struct extended + validation + defaults implemented

### Slice 2 (feat/plc-driver-core)

- [x] **T-2.01** `test` — [RED] `internal/plc/driver_test.go` created: mockDriver, compile-time assertion, error re-export assertions; confirmed package-not-found failure
- [x] **T-2.02** `impl` — [GREEN] `internal/plc/doc.go` + `internal/plc/driver.go` created: Driver interface (Connect, Close, ReadTag, WriteTag, ReadMulti, Connected), Options, error re-exports
- [x] **T-2.03** `test` — [RED] `internal/plc/errors_test.go` created: table-driven translateError tests (7 cases + 2 extra); confirmed undefined-symbol failure
- [x] **T-2.04** `impl` — [GREEN] `internal/plc/errors.go` created: translateError with full type-switch (CIPError, net.Error timeout, *net.OpError, io.EOF, "not connected", fallback)
- [x] **T-2.05** `test` — [RED] Extended `driver_test.go` with gologixDriver adapter tests using fakePLCClient; confirmed NewDriverWithClient undefined failure
- [x] **T-2.06** `impl` — [GREEN] `internal/plc/gologix.go` created: gologixClient interface, gologixDriver struct (atomic.Bool, sync.Mutex), Connect/Close/ReadTag/WriteTag/ReadMulti/Connected, NewDriver/NewDriverWithClient
- [x] **T-2.07** `chore` — Cross-platform CGO_ENABLED=0 build verified: linux/amd64, linux/arm64, darwin/arm64, windows/amd64 — all exit 0

## Files Changed

### Slice 1

| File | Action | What Was Done |
|------|--------|---------------|
| `internal/errors/errors.go` | Modified | Added `ErrPLCConnect`, `ErrPLCRead`, `ErrPLCWrite`, `ErrPLCTimeout` sentinels in `// PLC-domain sentinels (PLC-DRV-1.*)` block |
| `internal/errors/errors_test.go` | Modified | Added `TestPLCSentinelsAreDistinctNonNil` and `TestPLCSentinelsWrapping` covering all four sentinels |
| `internal/config/config.go` | Modified | Extended `PLC` struct with `Slot int`, `SocketTimeout string`, `ScanRate string`, `KeepAlive bool`, `Path string`; extended `Validate()` with PLC validation logic |
| `internal/config/loader.go` | Modified | Added `applyPLCDefaults(cfg, k)` helper called after unmarshal; added `extractRawPLCMaps(k)` |
| `internal/config/config_test.go` | Modified | Added 8 new test functions covering PLC fields, defaults, explicit values, and all validation error cases |
| `internal/config/testdata/sample.yaml` | Modified | Added PLC entry with all 7 fields |

### Slice 2

| File | Action | What Was Done |
|------|--------|---------------|
| `internal/plc/doc.go` | Created | Package-level godoc with SocketTimeout limitation, Phase 1 UDT exclusion, []bool alignment note |
| `internal/plc/driver.go` | Created | Driver interface, Option functional options, re-exported ErrPLCConnect/Read/Write/Timeout |
| `internal/plc/errors.go` | Created | `translateError(err, op, tag)` with 8-case type-switch |
| `internal/plc/gologix.go` | Created | `gologixClient` interface, `gologixDriver` struct, `NewDriver`, `NewDriverWithClient` |
| `internal/plc/driver_test.go` | Created | mockDriver, compile-time assertion, ErrReExports, fakePLCClient, 8 adapter tests |
| `internal/plc/errors_test.go` | Created | 9 table-driven translateError tests (internal package) |

## TDD Cycle Evidence

### Slice 1

| Task | Test File | Layer | RED | GREEN | REFACTOR |
|------|-----------|-------|-----|-------|----------|
| T-1.01 | `internal/errors/errors_test.go` | Unit | Compile error — sentinels undefined | N/A (test task) | N/A |
| T-1.02 | `internal/errors/errors_test.go` | Unit | N/A (impl task) | All 9 tests pass | Pattern matches existing blocks |
| T-1.03 | `internal/config/config_test.go` | Unit | Compile error — struct fields undefined | N/A (test task) | N/A |
| T-1.04 | `internal/config/config_test.go` | Unit | N/A (impl task) | All 18 config tests pass | Extracted `applyPLCDefaults` + `extractRawPLCMaps` |

### Slice 2

| Task | Test File | Layer | RED | GREEN | REFACTOR |
|------|-----------|-------|-----|-------|----------|
| T-2.01 | `internal/plc/driver_test.go` | Unit | `no non-test Go files` build failure | N/A (test task) | N/A |
| T-2.02 | `internal/plc/driver_test.go` | Unit | N/A (impl task) | `ok github.com/fgjcarlos/lgb/internal/plc 1.012s` | Driver interface matches design §4 |
| T-2.03 | `internal/plc/errors_test.go` | Unit | `undefined: translateError` build failure | N/A (test task) | N/A |
| T-2.04 | `internal/plc/errors_test.go` | Unit | N/A (impl task) | All 9 translateError tests pass | errors.As pattern consistent with stdlib |
| T-2.05 | `internal/plc/driver_test.go` | Unit | `undefined: plc.NewDriverWithClient` failure | N/A (test task) | N/A |
| T-2.06 | `internal/plc/driver_test.go` | Unit | N/A (impl task) | All tests pass, -race clean | gologixClient interface for testability |
| T-2.07 | N/A (chore) | Build | N/A | All 4 cross-platform builds exit 0 | N/A |

## Deviations from Design

1. **Driver interface has `ReadMulti` method**: Design §4 shows `ReadMulti` as part of the interface and T-2.01 spec confirms it ("Connect, Disconnect, ReadTag, WriteTag, ReadMulti, Connected"). Design §4 uses `Close()` not `Disconnect()` for the teardown method — implemented as `Close()` matching the design doc.

2. **`NewDriverWithClient` is exported**: Design implies only `NewDriver` is public. `NewDriverWithClient` is exported to allow injection in `package plc_test` (black-box tests). An alternative would be to use an unexported constructor with a test helper in `package plc`, but exporting it is simpler and has no semantic coupling risk.

3. **`gologixClient` interface is unexported**: The interface is defined inside `package plc`, not exported. This keeps gologix types contained at the package boundary as required by design §3.

4. **`Close()` idempotency**: Gologix `Disconnect()` returns an error when already disconnected. Our `Close()` treats all errors from `Disconnect()` as non-fatal and returns nil — this achieves clean idempotency without surfacing gologix-internal state errors to callers.

## Build Gates Passed

- `go test -tags no_embed -race -count=1 ./internal/plc/...` — PASS
- `go test -tags no_embed -race -count=1 ./...` — PASS (all 12 packages)
- `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -tags no_embed ./internal/plc/...` — exit 0
- `CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -tags no_embed ./internal/plc/...` — exit 0
- `CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -tags no_embed ./internal/plc/...` — exit 0
- `CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -tags no_embed ./internal/plc/...` — exit 0

## Remaining Tasks

- [ ] T-3.01 through T-3.04 — Manager lifecycle + integration tests
- [ ] T-4.01 through T-4.06 — Doctor check, server wiring, cmd wiring

## Workload / PR Boundary

- Mode: chained PR slice (stacked-to-main)
- Current work unit: Unit 2 — `internal/plc` package core
- Branch: `feat/plc-driver-core` stacked on `feat/plc-driver-errors-config`
- Boundary: starts from Slice 1 branch, ends with T-2.07 complete
- Estimated review budget impact: ~360 lines changed (within budget for this slice)
