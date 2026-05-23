---
change: plc-driver
phase: apply-progress
slice: 4
date: 2026-05-23
status: all-slices-complete
mode: Strict TDD
---

# Apply Progress: plc-driver — Slices 1, 2, 3 & 4

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

### Slice 4 (feat/plc-driver-wiring)

- [x] **T-4.01** `test` — [RED] Extended `internal/doctor/checks_test.go` with 6 new test functions:
  - TestPLCReachableCheck_Pass: real TCP listener on random port → StatusPass, message contains address
  - TestPLCReachableCheck_Fail: 127.0.0.1:19999 (not listening) → StatusFail, message contains address
  - TestPLCReachableCheck_NoPort_DefaultsTo44818: address without port → dials :44818; skips if port unavailable
  - TestPLCReachableCheck_Timeout: socketTimeout=50ms, TEST-NET-1 black-hole → StatusFail within 200ms
  - TestDefault_WithPLCs_RegistersCheck: Default(cfg) with 1 PLC → 6 checks, last is "plc-reachable/plc-a"
  - TestDefault_NoPLCs_NoCheckRegistered: Default(cfg) no PLCs → 5 checks
  - DoD confirmed: `go test ./internal/doctor/...` FAIL — undefined: plcReachableCheck
- [x] **T-4.02** `impl` — [GREEN] Added `plcReachableCheck` to `internal/doctor/checks.go`:
  - struct with `plc config.PLC`; Name() = "plc-reachable/<name-or-address>"
  - Run(ctx): resolvedAddr() appends :44818 if no port; resolvedTimeout() defaults to 5s
  - net.DialTimeout probe; StatusPass on success (conn.Close()), StatusFail on error
  - Updated Default() in doctor.go to iterate cfg.PLCs and register one check per entry
  - DoD confirmed: `go test -race ./internal/doctor/...` PASS
- [x] **T-4.03** `test` — [RED] Extended `internal/server/server_test.go` with:
  - mockPLCManager struct with sync.Mutex-protected StartWasCalled/StopWasCalled accessors
  - TestServer_WithPLCManager_StartStop: Start called before serving, Stop called after cancel
  - TestServer_NilPLCManager_NoOp: nil manager → Run still works
  - Updated all existing New(cfg, logger, nil) calls to New(cfg, logger, nil, nil)
  - DoD confirmed: `go test ./internal/server/...` FAIL — too many arguments in call to New
- [x] **T-4.04** `impl` — [GREEN] Updated `internal/server/server.go`:
  - Added exported PLCManager interface: `{ Start(context.Context) error; Stop() error }`
  - Changed New signature to `New(cfg, log, checks, plcMgr PLCManager) *Server`
  - Run(ctx): calls plcMgr.Start(ctx) before Serve (if non-nil); calls plcMgr.Stop() after ctx.Done() before httpx.Shutdown
  - nil manager handled safely (no-op path)
  - DoD confirmed: `go test -race ./internal/server/...` PASS
- [x] **T-4.05** `test` — [RED] Extended `cmd/lgb/cmd/server_test.go` with:
  - mockServerPLCManager implementing server.PLCManager for cmd-level tests
  - TestServerCmd_WithPLCs_CreatesPLCManager: PLCManagerFactory called when PLCs configured
  - TestServerCmd_NoPLCs_NilManager: PLCManagerFactory NOT called when no PLCs
  - Added `config` and `server` imports
  - DoD confirmed: `go test ./cmd/lgb/cmd/...` FAIL — unknown field PLCManagerFactory
- [x] **T-4.06** `impl` — [GREEN] Updated `cmd/lgb/cmd/server.go` and `root.go`:
  - Added PLCManagerFactory `func(*config.Config) server.PLCManager` field to Deps
  - runServerTo: creates plcMgr via factory when len(cfg.PLCs)>0; passes to server.New
  - defaultPLCManagerFactory wraps plc.NewManager(cfg, slog.Default(), nil)
  - Logs INFO "plc manager created" with component="plc-manager" and plc_count
  - DoD confirmed: `go test -race ./cmd/lgb/cmd/...` PASS; CGO_ENABLED=0 go build ./... exit 0

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

### Slice 4

| File | Action | What Was Done |
|------|--------|---------------|
| `internal/doctor/checks.go` | Modified | Added `plcReachableCheck`, `resolvedAddr`, `resolvedTimeout` helpers |
| `internal/doctor/doctor.go` | Modified | Updated Default() to register plcReachableCheck per PLC |
| `internal/doctor/checks_test.go` | Modified | Added 6 new plc-reachable check tests |
| `internal/server/server.go` | Modified | Exported PLCManager interface; updated New() signature (4th param); Start/Stop in Run() |
| `internal/server/server_test.go` | Modified | Updated all New() calls (3→4 params); added mockPLCManager with sync.Mutex; 2 new tests |
| `cmd/lgb/cmd/root.go` | Modified | Added PLCManagerFactory field to Deps |
| `cmd/lgb/cmd/server.go` | Modified | Creates PLCManager when PLCs configured; passes to server.New; defaultPLCManagerFactory |
| `cmd/lgb/cmd/server_test.go` | Modified | Added mockServerPLCManager; 2 new cmd wiring tests; config+server imports |
| `openspec/changes/plc-driver/tasks.md` | Modified | T-4.01 through T-4.06 marked [x] |

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

### Slice 4

| Task | Test File | Layer | RED | GREEN | REFACTOR |
|------|-----------|-------|-----|-------|----------|
| T-4.01 | `internal/doctor/checks_test.go` | Unit | `undefined: plcReachableCheck` compile fail | N/A (test task) | N/A |
| T-4.02 | `internal/doctor/checks_test.go` | Unit | N/A (impl task) | `ok .../internal/doctor 1.066s` with -race | `resolvedAddr`/`resolvedTimeout` extracted as helpers |
| T-4.03 | `internal/server/server_test.go` | Unit | `too many arguments in call to New` compile fail | N/A (test task) | mockPLCManager uses sync.Mutex for race safety |
| T-4.04 | `internal/server/server_test.go` | Unit | N/A (impl task) | `ok .../internal/server 1.053s` with -race | PLCManager exported interface; nil-safe run path |
| T-4.05 | `cmd/lgb/cmd/server_test.go` | Unit | `unknown field PLCManagerFactory` compile fail | N/A (test task) | N/A |
| T-4.06 | `cmd/lgb/cmd/server_test.go` | Unit | N/A (impl task) | `ok .../cmd/lgb/cmd 1.023s` with -race | defaultPLCManagerFactory wraps plc.NewManager |

## Deviations from Design

### Slices 1–3 (unchanged from previous progress)

1. Driver interface uses `Close()` (design §4) not `Disconnect()`.
2. `NewDriverWithClient` is exported for black-box test injection.
3. `gologixClient` interface is unexported.
4. `Close()` treats gologix Disconnect errors as non-fatal for idempotency.
5. **NewManager signature gains DriverFactory parameter**: `NewManager(cfg, log, factory DriverFactory)` — nil uses default. Design §4 shows `NewManager(cfg, log)`. Deliberate test-injection hook consistent with §11.
6. **Integration test uses startRealCIPSim instead of testutil.StartPLCSim**.
7. **Reload waits on shared wg**.

### Slice 4

1. **PLCManager interface is EXPORTED** (not unexported as the task spec says): The design §10 shows `*plc.Manager` directly but the tasks correctly note a local interface. Making it exported (`server.PLCManager`) allows `cmd/lgb/cmd/root.go` to reference it in the `Deps.PLCManagerFactory` type without creating an import cycle. An unexported interface would not be referenceable by the `cmd` package.

2. **`server.New` 4th param uses exported `PLCManager` interface**: The task says "local unexported interface" but `cmd` package needs to reference `server.PLCManager` in the factory type. Exported is the correct choice. Tests in `package server` work identically.

3. **`defaultPLCManagerFactory` uses `slog.Default()` as logger**: The `runServerTo` function builds `logger` before calling this, but `defaultPLCManagerFactory` is a standalone function. In production the server-cmd's `runServerTo` builds the logger and passes it via `d.Logger`. The factory is only called after logger init in `runServerTo`, so `slog.Default()` picks up the configured logger (set by `slog.SetDefault(logger)` in `persistentPreRun`). Tests inject their own factory so this is only the production path.

## Build Gates Passed (Slice 4)

- `go test -tags no_embed -race -count=1 ./internal/doctor/...` — PASS
- `go test -tags no_embed -race -count=1 ./internal/server/...` — PASS
- `go test -tags no_embed -race -count=1 ./cmd/lgb/cmd/...` — PASS
- `go test -tags no_embed -race -count=1 ./...` — PASS (all 11 packages)
- `CGO_ENABLED=0 go build -tags no_embed ./...` — exit 0

## Remaining Tasks

None. All T-1.xx through T-4.xx tasks are complete.

## Workload / PR Boundary

- Mode: chained PR slice (stacked-to-main)
- Current work unit: Unit 4 — Doctor check + server wiring + cmd wiring
- Branch: `feat/plc-driver-wiring` stacked on `feat/plc-driver-manager`
- Boundary: starts from Slice 3 branch, ends with T-4.06 complete
- Estimated review budget impact: ~130 lines changed (within budget for this slice)
