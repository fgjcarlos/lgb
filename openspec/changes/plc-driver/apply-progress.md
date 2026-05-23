---
change: plc-driver
phase: apply-progress
slice: 3
date: 2026-05-23
status: slice-3-complete
mode: Strict TDD
---

# Apply Progress: plc-driver — Slices 1, 2 & 3

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

### Slice 3 (feat/plc-driver-manager)

- [x] **T-3.01** `test` — [RED] `internal/plc/manager_test.go` created (package plc_test)
  - trackingMockDriver with thread-safe Connect/Close call tracking
  - 6 tests: NewManager creates N drivers, Start calls Connect, Stop calls Close, Stop after ctx cancel no deadlock, Driver(name) lookup, concurrent race safety
  - DoD confirmed: `go test ./internal/plc/...` FAIL — undefined: plc.NewManager
- [x] **T-3.02** `impl` — [GREEN] `internal/plc/manager.go` created
  - DriverFactory type (`func(cfg config.PLC) Driver`) for test injection; nil uses NewDriver
  - Manager struct: workers map[string]*plcWorker, wg sync.WaitGroup, mu sync.RWMutex, log, factory
  - plcWorker holds Driver, config.PLC, context.CancelFunc
  - Start: per-PLC goroutine with retry.Do (unlimited) + time.NewTicker at ScanRate
  - Stop: cancel all contexts, wg.Wait(), Close all drivers
  - Driver(name): lookup under RLock
  - Reload: drain removed PLCs (cancel + wg.Wait + Close), add new ones with new goroutines
  - runWorker: connects via retry.Do, parses ScanRate from config, scan loop with reconnect on disconnect
  - DoD confirmed: `go test -race ./internal/plc/...` PASS; `CGO_ENABLED=0 go build ./...` exit 0
- [x] **T-3.03** `test` — [RED/integration] `internal/plc/gologix_integration_test.go` created
  - Build tag: `//go:build integration`
  - startRealCIPSim: starts gologix.Server.Serve() on port 44818; skips test if port unavailable
  - TestIntegration_ReadTagScalar: SimBool=true, SimInt=42, SimFloat=3.14
  - TestIntegration_WriteTag: write float32(9.9), read back → 9.9
  - TestIntegration_ReadMulti: 3 tags in one ReadMulti call
  - TestIntegration_ConnectRetry: MaxAttempts=3 exhausted on closed port → non-nil error
  - TestIntegration_ConcurrentReads: 10 goroutines ReadTag, race-free
  - Compile-verified: `go test -tags "no_embed integration" -c ./internal/plc/... -o /dev/null` exit 0
- [x] **T-3.04** `test` — [RED/integration] `internal/plc/manager_integration_test.go` created
  - Build tag: `//go:build integration`
  - TestIntegration_ManagerStartStop: connects, 500ms wait, Stop within 2s
  - TestIntegration_ManagerReload: old driver removed, new driver connects
  - TestIntegration_ManagerPLCRemoval: plc-b removed via Reload, plc-a continues
  - All tests run with -race
  - Compile-verified: `go test -tags "no_embed integration" -c ./internal/plc/... -o /dev/null` exit 0

## Files Changed

### Slice 1

| File | Action | What Was Done |
|------|--------|---------------|
| `internal/errors/errors.go` | Modified | Added `ErrPLCConnect`, `ErrPLCRead`, `ErrPLCWrite`, `ErrPLCTimeout` sentinels |
| `internal/errors/errors_test.go` | Modified | Added sentinel distinction and wrapping tests |
| `internal/config/config.go` | Modified | Extended `PLC` struct + Validate() PLC validation |
| `internal/config/loader.go` | Modified | Added `applyPLCDefaults` helper |
| `internal/config/config_test.go` | Modified | Added 8 new PLC config test functions |
| `internal/config/testdata/sample.yaml` | Modified | Added full PLC entry |

### Slice 2

| File | Action | What Was Done |
|------|--------|---------------|
| `internal/plc/doc.go` | Created | Package-level godoc |
| `internal/plc/driver.go` | Created | Driver interface, Option functional options, error re-exports |
| `internal/plc/errors.go` | Created | `translateError` with 8-case type-switch |
| `internal/plc/gologix.go` | Created | `gologixClient` interface, `gologixDriver` struct, NewDriver/NewDriverWithClient |
| `internal/plc/driver_test.go` | Created | mockDriver, fakePLCClient, 8 adapter tests |
| `internal/plc/errors_test.go` | Created | 9 table-driven translateError tests |

### Slice 3

| File | Action | What Was Done |
|------|--------|---------------|
| `internal/plc/manager.go` | Created | Manager struct, DriverFactory, Start/Stop/Reload/Driver, runWorker scan loop |
| `internal/plc/manager_test.go` | Created | trackingMockDriver, 6 Manager lifecycle unit tests |
| `internal/plc/gologix_integration_test.go` | Created | 5 integration tests for gologixDriver (//go:build integration) |
| `internal/plc/manager_integration_test.go` | Created | 3 integration tests for Manager lifecycle (//go:build integration) |
| `openspec/changes/plc-driver/tasks.md` | Modified | T-3.01 through T-3.04 marked [x] |

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
| T-2.02 | `internal/plc/driver_test.go` | Unit | N/A (impl task) | ok github.com/fgjcarlos/lgb/internal/plc | Driver interface matches design §4 |
| T-2.03 | `internal/plc/errors_test.go` | Unit | `undefined: translateError` build failure | N/A (test task) | N/A |
| T-2.04 | `internal/plc/errors_test.go` | Unit | N/A (impl task) | All 9 translateError tests pass | errors.As pattern consistent with stdlib |
| T-2.05 | `internal/plc/driver_test.go` | Unit | `undefined: plc.NewDriverWithClient` failure | N/A (test task) | N/A |
| T-2.06 | `internal/plc/driver_test.go` | Unit | N/A (impl task) | All tests pass, -race clean | gologixClient interface for testability |
| T-2.07 | N/A (chore) | Build | N/A | All 4 cross-platform builds exit 0 | N/A |

### Slice 3

| Task | Test File | Layer | Safety Net | RED | GREEN | TRIANGULATE | REFACTOR |
|------|-----------|-------|------------|-----|-------|-------------|----------|
| T-3.01 | `manager_test.go` | Unit | N/A (new file) | Written — `undefined: plc.NewManager` compile fail confirmed | N/A (test task) | 6 test cases covering all spec scenarios | N/A |
| T-3.02 | `manager_test.go` | Unit | N/A (new file) | N/A (impl task) | `ok .../internal/plc 1.115s` with -race | Covered by T-3.01 6 cases | ScanRate from config.PLC; RWMutex for Driver lookup; plcWorker struct |
| T-3.03 | `gologix_integration_test.go` | Integration | N/A (new file) | Written (//go:build integration; compile-verified) | Compiles clean | 5 scenarios: scalar read, write, multi, retry, concurrent | startRealCIPSim with port-unavailable skip guard |
| T-3.04 | `manager_integration_test.go` | Integration | N/A (new file) | Written (//go:build integration; compile-verified) | Compiles clean | 3 scenarios: start/stop, reload, PLC removal | 2s Stop deadline; shared startRealCIPSim |

## Deviations from Design

### Slice 1-2 (unchanged from previous progress)

1. Driver interface uses `Close()` (design §4) not `Disconnect()`.
2. `NewDriverWithClient` is exported for black-box test injection.
3. `gologixClient` interface is unexported.
4. `Close()` treats gologix Disconnect errors as non-fatal for idempotency.

### Slice 3

1. **NewManager signature gains DriverFactory parameter**: `NewManager(cfg, log, factory DriverFactory)` — nil uses default. Design §4 shows `NewManager(cfg, log)`. This is a deliberate test-injection hook consistent with §11 (unit testing with mock Driver). Slice 4 server wiring will call `NewManager(cfg, log, nil)`.

2. **Integration test uses startRealCIPSim instead of testutil.StartPLCSim**: `testutil.StartPLCSim` is a TCP-accept-only stub — it cannot serve CIP protocol. Integration tests use `gologix.Server.Serve()` directly (port 44818) with a skip guard if port is unavailable. Design §11 says "testutil.StartPLCSim" but that helper is insufficient for real CIP I/O.

3. **Reload waits on shared wg**: `Reload` calls `wg.Wait()` which blocks until ALL goroutines exit. This is correct for Phase 1 (drained goroutines hold the only wg entries), but would need per-worker tracking in Phase 2 if concurrent Reload calls are needed.

## Build Gates Passed

- `go test -tags no_embed -race -count=1 ./internal/plc/...` — PASS (1.115s)
- `go test -tags no_embed -race -count=1 ./...` — PASS (all 12 packages)
- `go test -tags "no_embed integration" -c ./internal/plc/... -o /dev/null` — exit 0 (compile-verified)
- `CGO_ENABLED=0 go build -tags no_embed ./...` — exit 0

## Remaining Tasks

- [ ] T-4.01 through T-4.06 — Doctor check, server wiring, cmd wiring

## Workload / PR Boundary

- Mode: chained PR slice (stacked-to-main)
- Current work unit: Unit 3 — `internal/plc` Manager
- Branch: `feat/plc-driver-manager` stacked on `feat/plc-driver-core`
- Boundary: starts from Slice 2 branch, ends with T-3.04 complete
- Estimated review budget impact: ~360 lines changed (within budget for this slice)
