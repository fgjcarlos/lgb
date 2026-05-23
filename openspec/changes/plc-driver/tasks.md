---
change: plc-driver
phase: tasks
date: 2026-05-23
status: draft
---

# Tasks: PLC Driver — CIP Communication via gologix

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | ~900 (additions + deletions) |
| 400-line budget risk | High |
| Chained PRs recommended | Yes |
| Suggested split | PR 1 errors+config → PR 2 plc-core → PR 3 plc-manager → PR 4 wiring |
| Delivery strategy | ask-on-risk |
| Chain strategy | stacked-to-main |

Decision needed before apply: Yes
Chained PRs recommended: Yes
Chain strategy: stacked-to-main
400-line budget risk: High

### Suggested Work Units

| Unit | Goal | Likely PR | ~Lines | Base | Self-mergeable |
|------|------|-----------|--------|------|----------------|
| 1 | Error sentinels + PLC config struct extensions + validation | `feat/plc-driver-errors-config` | ~140 | `main` | Yes — additive to existing pkgs; all existing tests stay green |
| 2 | `internal/plc` package: Driver interface, gologix adapter, error translation, doc, unit tests | `feat/plc-driver-core` | ~360 | PR 1 | Yes — no external wiring yet; compiles and unit-tests pass |
| 3 | `internal/plc` Manager: lifecycle, scan loop, hot-reload, integration tests | `feat/plc-driver-manager` | ~360 | PR 2 | Yes — Manager self-contained; integration tests run with plcsim |
| 4 | Doctor `plc-reachable` check + `server.New` wiring + `cmd/lgb/cmd/server.go` wiring | `feat/plc-driver-wiring` | ~130 | PR 3 | Yes — completes feature end-to-end |

Each PR targets the immediate previous branch. Only PR 1 targets `main`. Each diff shows only that slice's changes.

---

## Slice 1 — `feat/plc-driver-errors-config` (~140 lines)

Additive changes to `internal/errors` and `internal/config`. No new packages. All existing tests must stay green after this slice.

### Group A — PLC error sentinels

- [ ] **T-1.01** `test` — **[RED]** Extend `internal/errors/errors_test.go`: add assertions that `ErrPLCConnect`, `ErrPLCRead`, `ErrPLCWrite`, and `ErrPLCTimeout` are distinct non-nil errors; assert each is identifiable via `errors.Is` when wrapped with `fmt.Errorf("%w: %w", sentinel, underlying)`; assert none is equal to any existing sentinel.
  - **Files**: `internal/errors/errors_test.go`
  - **Reqs**: PLC-ERR-1.1
  - **Design**: §8 (error model)
  - **Deps**: none
  - **DoD**: `go test ./internal/errors/...` FAILS (sentinels do not exist yet).

- [ ] **T-1.02** `impl` — **[GREEN]** Add PLC-domain sentinels to `internal/errors/errors.go` in a labeled block `// PLC-domain sentinels (PLC-DRV-1.*)`: `ErrPLCConnect = errors.New("plc connect failed")`, `ErrPLCRead = errors.New("plc read failed")`, `ErrPLCWrite = errors.New("plc write failed")`, `ErrPLCTimeout = errors.New("plc operation timeout")`.
  - **Files**: `internal/errors/errors.go`
  - **Reqs**: PLC-ERR-1.1
  - **Design**: §8
  - **Deps**: T-1.01
  - **DoD**: `go test ./internal/errors/...` passes; all four sentinels exported.

### Group B — Config PLC struct extensions

- [ ] **T-1.03** `test` — **[RED]** Extend `internal/config/config_test.go`: add table-driven cases covering (a) YAML with only `name`+`address` → `Slot=0`, `SocketTimeout="5s"`, `ScanRate="1s"`, `KeepAlive=true`, `Path=""`; (b) explicit `slot: 2`, `socketTimeout: "10s"`, `scanRate: "500ms"`, `keepAlive: false`, `path: "1,0"` → all five fields match; (c) `address: ""` → `Validate()` returns error wrapping `ErrConfigInvalid` with message `plcs[0].address: must not be empty`; (d) `socketTimeout: "not-a-duration"` → `Validate()` error wrapping `ErrConfigInvalid`; (e) `socketTimeout: "-1s"` → error containing `must be positive`; (f) `scanRate: "0s"` → error containing `scanRate: must be positive`; (g) `slot: 16` → error containing `slot: must be between 0 and 15`; (h) two PLC entries each with two violations → four-error aggregate, `errors.Is(err, ErrConfigInvalid)` true. Update `internal/config/testdata/sample.yaml` to include a PLC entry with `name`, `address`, `slot`, `socketTimeout`, `scanRate`, `keepAlive`, `path`.
  - **Files**: `internal/config/config_test.go`, `internal/config/testdata/sample.yaml`
  - **Reqs**: PLC-CFG-1.1–PLC-CFG-1.7
  - **Design**: §7 (config schema)
  - **Deps**: T-1.02
  - **DoD**: `go test ./internal/config/...` FAILS (new fields absent from struct).

- [ ] **T-1.04** `impl` — **[GREEN]** Extend `PLC` struct in `internal/config/config.go` with five new fields: `Slot int \`koanf:"slot"\``, `SocketTimeout string \`koanf:"socketTimeout"\``, `ScanRate string \`koanf:"scanRate"\``, `KeepAlive bool \`koanf:"keepAlive"\``, `Path string \`koanf:"path"\``. Extend `(*Config).Validate()` to iterate `cfg.PLCs` with index `i` and accumulate violations: (a) address empty → `plcs[i].address: must not be empty: %w`; (b) socketTimeout non-empty and parse fails or ≤ 0 → `plcs[i].socketTimeout: ...: %w`; (c) scanRate non-empty and parse fails or ≤ 0 → `plcs[i].scanRate: must be positive: %w`; (d) slot < 0 or > 15 → `plcs[i].slot: must be between 0 and 15: %w`. Extend `internal/config/loader.go` defaults confmap to set `SocketTimeout: "5s"`, `ScanRate: "1s"`, `KeepAlive: true`, `Slot: 0` for each PLC entry (use koanf confmap defaults or post-load defaulting loop).
  - **Files**: `internal/config/config.go`, `internal/config/loader.go`
  - **Reqs**: PLC-CFG-1.1–PLC-CFG-1.7
  - **Design**: §7
  - **Deps**: T-1.03
  - **DoD**: `go test ./internal/config/...` passes including all new cases; `CGO_ENABLED=0 go build ./...` exits 0.

---

## Slice 2 — `feat/plc-driver-core` (~360 lines)

New `internal/plc` package: Driver interface, gologix adapter, error translation, package doc, and unit tests. Manager is NOT in this slice. After this slice the package compiles and unit tests pass; no server wiring yet.

### Group A — Package skeleton and error re-exports

- [ ] **T-2.01** `test` — **[RED]** Create `internal/plc/driver_test.go`: define `mockDriver` struct implementing `Driver` interface (Connect, Disconnect, ReadTag, WriteTag, ReadMulti, Connected); write compile-time assertion `var _ Driver = (*mockDriver)(nil)`; write test `TestMockDriverSatisfiesInterface` (always passes once interface exists). Also write `TestErrReExports`: assert `plc.ErrPLCConnect == errs.ErrPLCConnect`, same for Read/Write/Timeout. All tests in `package plc_test`.
  - **Files**: `internal/plc/driver_test.go`
  - **Reqs**: PLC-DRV-1.1, PLC-ERR-1.2
  - **Design**: §4 (interface definitions), §8
  - **Deps**: T-1.02
  - **DoD**: `go test ./internal/plc/...` FAILS (package does not exist).

- [ ] **T-2.02** `impl` — **[GREEN]** Create `internal/plc/doc.go`: package-level godoc including SocketTimeout limitation note and Phase 1 UDT exclusion warning. Create `internal/plc/driver.go`: export `Driver` interface (Connect, Disconnect, ReadTag, WriteTag, ReadMulti, Connected); export `Options` struct (RetryInitial, RetryMax, MaxAttempts time.Duration/int; zero values resolve to defaults 1s/30s/0); re-export four error sentinels from `internal/errors`.
  - **Files**: `internal/plc/doc.go`, `internal/plc/driver.go`
  - **Reqs**: PLC-DRV-1.1, PLC-DRV-1.2, PLC-ERR-1.2
  - **Design**: §4, §8
  - **Deps**: T-2.01
  - **DoD**: `go test ./internal/plc/...` passes (interface + re-export tests green).

### Group B — Error translation helper

- [ ] **T-2.03** `test` — **[RED]** Create `internal/plc/errors_test.go` (package `plc`): table-driven tests for `translateError`: (a) `*gologix.CIPError` on read op → wraps `ErrPLCRead`; (b) `*gologix.CIPError` on write op → wraps `ErrPLCWrite`; (c) `net.OpError` → wraps `ErrPLCConnect`; (d) `io.EOF` → wraps `ErrPLCConnect`; (e) timeout error (implements `net.Error` with `Timeout() == true`) → wraps `ErrPLCTimeout`; (f) `nil` → returns `nil`; (g) unknown error type → wraps `ErrPLCRead` (no panic per PLC-ERR-1.5).
  - **Files**: `internal/plc/errors_test.go`
  - **Reqs**: PLC-ERR-1.3, PLC-ERR-1.5
  - **Design**: §8 (error model, translation rules)
  - **Deps**: T-2.02
  - **DoD**: `go test ./internal/plc/...` FAILS (translateError not implemented).

- [ ] **T-2.04** `impl` — **[GREEN]** Create `internal/plc/errors.go`: unexported `translateError(err error, op string, tag string) error` that type-switches on `*gologix.CIPError` (→ `ErrPLCRead`/`ErrPLCWrite` per op), `net.Error` with `Timeout()==true` (→ `ErrPLCTimeout`), `net.OpError` / `io.EOF` / "not connected" string (→ `ErrPLCConnect`), nil (→ nil), default (→ `ErrPLCRead`). Use `fmt.Errorf` with double `%w` per PLC-ERR-1.3 convention.
  - **Files**: `internal/plc/errors.go`
  - **Reqs**: PLC-ERR-1.3, PLC-ERR-1.5
  - **Design**: §8
  - **Deps**: T-2.03
  - **DoD**: `go test ./internal/plc/...` passes (all translateError cases).

### Group C — gologix adapter

- [ ] **T-2.05** `test` — **[RED]** Extend `internal/plc/driver_test.go` with unit tests for `gologixDriver` using a fake gologix client interface (injected via constructor option or unexported field): (a) `NewDriver` returns value assignable to `Driver`; (b) `Connected()` returns false before Connect, true after successful Connect; (c) `Connect(ctx)` with cancelled context returns `ctx.Err()`, `Connected()` false; (d) `Disconnect()` after Connect → Connected() false; (e) `Disconnect()` twice → no panic, returns nil; (f) `ReadTag` with `[]bool` of length 10 (not multiple of 32) → error wrapping `ErrPLCRead` with message "length must be a multiple of 32" before calling client; (g) `ReadMulti` with len(tags) != len(dests) → error wrapping `ErrPLCRead`; (h) `go test -race` passes (concurrent Connected() calls). Use `mockGologixClient` interface with `Connect/Disconnect/Read/Write` methods.
  - **Files**: `internal/plc/driver_test.go`
  - **Reqs**: PLC-DRV-1.3, PLC-DRV-1.4, PLC-DRV-1.5, PLC-DRV-1.6, PLC-DRV-1.8, PLC-DRV-1.10
  - **Design**: §4, §5 (decisions 1–7)
  - **Deps**: T-2.04
  - **DoD**: `go test ./internal/plc/...` FAILS (gologixDriver not implemented).

- [ ] **T-2.06** `impl` — **[GREEN]** Create `internal/plc/gologix.go`: unexported `gologixDriver` struct holding `*gologix.Client`, `connected atomic.Bool`, `mu sync.Mutex`, `cfg config.PLC`, `opts Options`. Implement all six `Driver` methods: `Connect` delegates to `internal/retry.Do` with opts; sets `AutoConnect=false` in constructor; `SocketTimeout` set from `cfg.SocketTimeout`; `Disconnect` is idempotent; `ReadTag` validates `[]bool` length before calling client; `WriteTag` calls client write; `ReadMulti` checks length parity then loops `ReadTag`; `Connected` reads atomic. Constructor: `func NewDriver(cfg config.PLC, opts ...Option) Driver`. Apply `translateError` on all client error paths.
  - **Files**: `internal/plc/gologix.go`
  - **Reqs**: PLC-DRV-1.1, PLC-DRV-1.3, PLC-DRV-1.4, PLC-DRV-1.5, PLC-DRV-1.6, PLC-DRV-1.7, PLC-DRV-1.8, PLC-DRV-1.9, PLC-DRV-1.10
  - **Design**: §4, §5, §6.1, §6.2
  - **Deps**: T-2.05
  - **DoD**: `go test -race ./internal/plc/...` passes; `CGO_ENABLED=0 go build ./internal/plc/...` exits 0 on host.

### Group D — Cross-platform build check

- [ ] **T-2.07** `chore` — Verify `CGO_ENABLED=0 go build ./internal/plc/...` passes for all four target platforms (`GOOS=linux GOARCH=amd64`, `GOOS=linux GOARCH=arm64`, `GOOS=darwin GOARCH=arm64`, `GOOS=windows GOARCH=amd64`). Record results as a comment in the PR description. No code change expected; this task is a gate.
  - **Files**: (none — verification only)
  - **Reqs**: PLC-DRV-2.4
  - **Design**: §5 decision #7, §13 (non-functional)
  - **Deps**: T-2.06
  - **DoD**: All four cross-builds exit 0 with `CGO_ENABLED=0`.

---

## Slice 3 — `feat/plc-driver-manager` (~360 lines)

`Manager` type, per-PLC goroutines, scan loop, hot-reload, and integration tests. Depends on Slice 2. No server wiring in this slice.

### Group A — Manager unit tests

- [ ] **T-3.01** `test` — **[RED]** Create `internal/plc/manager_test.go` (package `plc_test`): use `mockDriver` from driver_test.go (move to `testhelpers_test.go` if needed for sharing); write: (a) `NewManager` with one-PLC config creates one driver; (b) `Manager.Start(ctx)` calls `Connect` on all drivers; (c) `Manager.Stop()` calls `Disconnect` on all drivers and blocks until goroutines exit; (d) `Manager.Stop()` after context cancel does not deadlock (2 s deadline via `t.Deadline()`); (e) `Manager.Driver(name)` returns correct driver or (nil, false); (f) `go test -race` passes (concurrent Start + Stop).
  - **Files**: `internal/plc/manager_test.go`
  - **Reqs**: PLC-DRV-2.1, PLC-DRV-2.3
  - **Design**: §4 (Manager API), §6.3 (hot-reload seq)
  - **Deps**: T-2.06
  - **DoD**: `go test ./internal/plc/...` FAILS (Manager type does not exist).

- [ ] **T-3.02** `impl` — **[GREEN]** Create `internal/plc/manager.go`: `Manager` struct with `drivers map[string]Driver`, `cancels map[string]context.CancelFunc`, `wg sync.WaitGroup`, `mu sync.Mutex`, `log *slog.Logger`. Implement `NewManager(cfg *config.Config, log *slog.Logger) *Manager`; `Start(ctx)` — for each PLC creates driver via `NewDriver`, stores it, starts goroutine calling `driver.Connect(ctx)` via `retry.Do` then enters scan loop at `ScanRate` using `time.NewTicker`; `Stop()` — cancels all per-PLC contexts, waits on WaitGroup; `Driver(name)` lookup; `Reload(cfg)` — drain removed/changed PLCs (Close + cancel + WaitGroup wait), add new PLCs (Start). Scan loop: on ReadTag error log WARN and attempt reconnect via Connect with retry; exit on ctx cancel.
  - **Files**: `internal/plc/manager.go`
  - **Reqs**: PLC-DRV-2.1, PLC-DRV-2.2, PLC-DRV-2.3
  - **Design**: §4, §6.3, §6.4
  - **Deps**: T-3.01
  - **DoD**: `go test -race ./internal/plc/...` passes; `CGO_ENABLED=0 go build ./...` exits 0.

### Group B — Integration tests with plcsim

- [ ] **T-3.03** `test` — **[RED/integration]** Create `internal/plc/gologix_integration_test.go` (`//go:build integration`; prefix `TestIntegration_`): (a) `TestIntegration_ReadTagScalar` — `StartPLCSim(t)`, Connect, `ReadTag("SimBool", &b)` → `b == true`; `ReadTag("SimInt", &i)` → `i == int16(42)`; `ReadTag("SimFloat", &f)` → `f == float32(3.14)`; (b) `TestIntegration_WriteTag` — WriteTag("SimFloat", float32(9.9)), ReadTag → float32(9.9); (c) `TestIntegration_ReadMulti` — read SimBool+SimInt+SimFloat in one call → all correct; (d) `TestIntegration_ConnectRetry` — stop plcsim mid-connect, verify retry occurs; (e) `TestIntegration_ConcurrentReads` — 10 goroutines ReadTag concurrently, `go test -race` no data race.
  - **Files**: `internal/plc/gologix_integration_test.go`
  - **Reqs**: PLC-DRV-2.5
  - **Design**: §11 (testing strategy, integration layer)
  - **Deps**: T-3.02
  - **DoD**: `go test -tags=integration -race ./internal/plc/...` passes.

- [ ] **T-3.04** `test` — **[RED/integration]** Create `internal/plc/manager_integration_test.go` (`//go:build integration`): (a) `TestIntegration_ManagerStartStop` — `StartPLCSim(t)`, `NewManager`, `Start`, wait 500 ms, `Stop` returns within 2 s, no goroutine leak; (b) `TestIntegration_ManagerReload` — start with PLC A, Reload with PLC A address changed, old driver disconnected, new driver connects; (c) `TestIntegration_ManagerPLCRemoval` — start with PLCs A+B, Reload removing B, only A goroutine continues; (d) all run with `-race`.
  - **Files**: `internal/plc/manager_integration_test.go`
  - **Reqs**: PLC-DRV-2.1, PLC-DRV-2.2, PLC-DRV-2.3
  - **Design**: §6.3 (hot-reload seq)
  - **Deps**: T-3.03
  - **DoD**: `go test -tags=integration -race ./internal/plc/...` passes; `Stop()` returns within 2 s.

---

## Slice 4 — `feat/plc-driver-wiring` (~130 lines)

Doctor `plc-reachable` check, `server.New` signature update, and `cmd/lgb/cmd/server.go` wiring. Depends on Slice 3.

### Group A — Doctor plc-reachable check

- [ ] **T-4.01** `test` — **[RED]** Extend `internal/doctor/checks_test.go`: (a) `TestPLCReachableCheck_Pass` — start real TCP listener on random port, config PLC with that address → result status `Pass`, message contains address; (b) `TestPLCReachableCheck_Fail` — config PLC with `127.0.0.1:19999` (not listening) → status `Fail`, message contains address; (c) `TestPLCReachableCheck_NoPort_DefaultsTo44818` — config PLC address `192.168.1.10` (no port) → check dials `192.168.1.10:44818` (verify via `net.Listen` spy or address assertion); (d) `TestPLCReachableCheck_Timeout` — config `socketTimeout: "50ms"`, address is a black-hole → result within ~200 ms, status `Fail`; (e) `TestDefault_WithPLCs_RegistersCheck` — `Default(cfg)` with one PLC → `r.Checks()` length 6, sixth check name is `"plc-reachable/<name>"`; (f) `TestDefault_NoPLCs_NoCheckRegistered` — `Default(cfg)` with empty PLCs → length 5.
  - **Files**: `internal/doctor/checks_test.go`
  - **Reqs**: PLC-DOC-1.1, PLC-DOC-1.2, PLC-DOC-1.3, PLC-DOC-1.4, PLC-DOC-1.5
  - **Design**: §9 (doctor integration)
  - **Deps**: T-1.04
  - **DoD**: `go test ./internal/doctor/...` FAILS (plcReachableCheck not implemented).

- [ ] **T-4.02** `impl` — **[GREEN]** Add `plcReachableCheck` to `internal/doctor/checks.go`: struct with `plc config.PLC`; `Name()` returns `"plc-reachable/<name-or-address>"`; `Run(ctx)` calls `net.DialTimeout("tcp", addr, timeout)` where addr defaults port to `:44818` if no port present, timeout from `time.ParseDuration(plc.SocketTimeout)` defaulting to 5 s; closes conn on success; returns `StatusPass` or `StatusFail`. Update `Default(cfg *config.Config)` in `internal/doctor/doctor.go` to iterate `cfg.PLCs` and register one `plcReachableCheck` per entry after the existing five checks.
  - **Files**: `internal/doctor/checks.go`, `internal/doctor/doctor.go`
  - **Reqs**: PLC-DOC-1.1, PLC-DOC-1.2, PLC-DOC-1.4, PLC-DOC-1.5
  - **Design**: §9
  - **Deps**: T-4.01
  - **DoD**: `go test ./internal/doctor/...` passes; `Default(cfg)` with one PLC returns registry of length 6.

### Group B — Server wiring

- [ ] **T-4.03** `test` — **[RED]** Extend `internal/server/server_test.go`: (a) `TestServer_WithPLCManager_StartStop` — create `MockPLCManager` (unexported interface with `Start(ctx) error` and `Stop() error`); pass to `server.New`; `Run(ctx)` calls `Start` before serving, `Stop` after ctx cancel; (b) `TestServer_NilPLCManager_NoOp` — nil manager → Run still works (backward compatible).
  - **Files**: `internal/server/server_test.go`
  - **Reqs**: PLC-DRV-2.1 (manager lifecycle via server)
  - **Design**: §10 (server wiring)
  - **Deps**: T-3.02
  - **DoD**: `go test ./internal/server/...` FAILS (server.New does not accept manager yet).

- [ ] **T-4.04** `impl` — **[GREEN]** Update `internal/server/server.go`: change `New` signature to `New(cfg *config.Config, log *slog.Logger, checks []doctor.Check, plcMgr PLCManager) *Server` where `PLCManager` is a local unexported interface `{ Start(context.Context) error; Stop() error }`. In `Run(ctx)`: if `plcMgr != nil`, call `plcMgr.Start(ctx)` before `srv.Serve`; on ctx cancel call `plcMgr.Stop()` before `httpx.Shutdown`. Accepting `nil` is valid (no-op path).
  - **Files**: `internal/server/server.go`
  - **Reqs**: PLC-DRV-2.1
  - **Design**: §10
  - **Deps**: T-4.03
  - **DoD**: `go test ./internal/server/...` passes.

### Group C — cmd/lgb wiring

- [ ] **T-4.05** `test` — **[RED]** Extend `cmd/lgb/cmd/server_test.go`: (a) valid config with one PLC entry → `NewServerCmd` creates a `plc.Manager`, passes it to `server.New`; spy via injectable factory in `Deps.PLCManagerFactory func(cfg) PLCManager`; (b) config with no PLCs → `plcMgr` passed as nil (no crash).
  - **Files**: `cmd/lgb/cmd/server_test.go`
  - **Reqs**: PLC-DRV-2.1
  - **Design**: §10
  - **Deps**: T-4.04
  - **DoD**: `go test ./cmd/lgb/cmd/...` FAILS (server cmd does not create Manager yet).

- [ ] **T-4.06** `impl` — **[GREEN]** Update `cmd/lgb/cmd/server.go`: create `plc.NewManager(d.Config, d.Logger)` when `len(d.Config.PLCs) > 0`, otherwise nil; pass manager to `server.New(d.Config, d.Logger, doctor.Default(d.Config).Checks(), plcMgr)`. Update all call-sites of `server.New` that now need the fourth parameter (add nil for non-server-cmd callers). Log INFO `component="plc-manager"` when manager is created.
  - **Files**: `cmd/lgb/cmd/server.go`, `internal/server/server.go` (nil-safe callsite if any), `cmd/lgb/cmd/server_test.go` (Deps extension)
  - **Reqs**: PLC-DRV-2.1
  - **Design**: §10
  - **Deps**: T-4.05
  - **DoD**: `go test ./cmd/lgb/...` passes; `CGO_ENABLED=0 go build ./cmd/lgb` exits 0.

---

## Cross-slice dependency summary

```
T-1.01 -> T-1.02 (error sentinels)
T-1.02 -> T-1.03 -> T-1.04 (config struct + validation)

T-1.02 -> T-2.01 -> T-2.02 (driver interface + re-exports)
T-2.02 -> T-2.03 -> T-2.04 (error translation)
T-2.04 -> T-2.05 -> T-2.06 (gologix adapter)
T-2.06 -> T-2.07 (cross-platform build gate)

T-2.06 -> T-3.01 -> T-3.02 (manager)
T-3.02 -> T-3.03 (gologix integration tests)
T-3.03 -> T-3.04 (manager integration tests)

T-1.04 -> T-4.01 -> T-4.02 (doctor check)
T-3.02 -> T-4.03 -> T-4.04 (server wiring)
T-4.04 -> T-4.05 -> T-4.06 (cmd wiring)
```

Parallel opportunities within a slice:
- Slice 1: T-1.01/T-1.02 can start immediately; T-1.03 requires T-1.02 only.
- Slice 4: T-4.01 (doctor) and T-4.03 (server) can start in parallel once Slice 3 is merged; T-4.05 requires T-4.04.

---

## Risk Acknowledgements

| Risk | Mitigation |
|------|------------|
| gologix `*Client` fields `AutoConnect`, `SocketTimeout`, `Controller.Path` are internal; API may differ from design §4 | Verify field names against gologix v0.41.0-beta source before T-2.06; adjust if needed |
| `parseFloat`/bool-array alignment rules may differ from spec if gologix encodes differently | Validate via integration tests in T-3.03; adjust `ReadTag` guard accordingly |
| Config defaulting for `KeepAlive: true` requires post-load loop since koanf confmap does not handle per-slice defaults automatically | Implement a `applyPLCDefaults(cfg *Config)` helper called inside `Load` after unmarshal |
| `server.New` signature change is a breaking change for any call-site (currently only cmd/server.go) | T-4.04 updates all call-sites atomically in the same commit |
| Manager hot-reload race: in-flight ReadTag on old driver sees disconnect | Documented in spec PLC-DRV-2.3 §4; callers handle error naturally via reconnect loop |
