---
change: plc-driver
phase: design
date: 2026-05-23
status: draft
inputs:
  - openspec/changes/plc-driver/proposal.md
---

# Design: PLC Driver -- CIP Communication via gologix

## 1. Technical Approach

Thin adapter layer in `internal/plc/` wrapping `danomagnum/gologix` v0.41.0-beta. One `*gologix.Client` per configured PLC, managed by a `Manager` that owns lifecycle (start/stop/hot-reload). The `Driver` interface isolates all gologix types at the package boundary so the rest of the gateway never imports gologix directly. Reconnection delegates to `internal/retry.Do`. Error translation happens at the adapter boundary: gologix errors (including `*gologix.CIPError`) are mapped to project sentinels in `internal/errors`. Phase 1 scope: scalars and 1-D arrays only -- no UDTs, no tag browsing.

---

## 2. Package Layout

```
internal/plc/
  driver.go      -- Driver interface, Options, TagValue types
  gologix.go     -- gologixDriver concrete adapter (unexported)
  manager.go     -- Manager: start/stop/hot-reload all PLCs
  errors.go      -- translateError helper (CIPError -> sentinels)
  doc.go         -- package doc
  driver_test.go -- unit tests with mock Driver
  gologix_integration_test.go -- //go:build integration; uses testutil.StartPLCSim
  manager_test.go -- unit tests for Manager lifecycle
```

---

## 3. Component Diagram

```
  cmd/lgb/cmd/server.go
        |
        | composes
        v
  internal/server.Server
        |
        | .Run(ctx) starts/stops
        v
  internal/plc.Manager -----> []plc.Driver (one per PLC)
        |                          |
        | uses                     | wraps
        v                          v
  internal/retry.Do          gologixDriver
  internal/config.PLC            |
  internal/errors                | delegates to
                                 v
                          *gologix.Client
                            .Connect()
                            .Read(tag, &dest)
                            .Write(tag, val)
                            .Disconnect()

  internal/doctor.Registry
        |
        | registers
        v
  plcReachableCheck (TCP dial per PLC addr)
```

Import direction (enforced):

| From | To | Allowed? |
|------|----|----------|
| `plc` | `config`, `errors`, `retry` | Yes |
| `plc` | `server`, `cmd` | **No** |
| `server` | `plc` | Yes (Manager as dependency) |
| `doctor/checks` | `config` | Yes (reads PLC addrs) |
| `plc` | `gologix` | Yes (adapter only) |
| any non-plc `internal/` | `gologix` | **No** (except `testutil`) |

---

## 4. Interface Definitions

```go
package plc

// Driver is the boundary interface for PLC tag I/O.
// Implementations MUST be safe for sequential use from a single goroutine.
// Concurrent use is NOT required (gologix serializes internally).
type Driver interface {
    Connect(ctx context.Context) error
    Close() error
    ReadTag(tag string, dest any) error
    WriteTag(tag string, val any) error
    Connected() bool
}

// Options configures a single PLC connection.
type Options struct {
    Address       string        // host:port or host (default port 44818)
    Slot          int           // backplane slot (default 0)
    Timeout       time.Duration // SocketTimeout on gologix client
    AutoConnect   bool          // MUST be false -- enforced in constructor
}

// Manager owns the lifecycle of all PLC drivers.
type Manager struct { /* unexported */ }

func NewManager(cfg *config.Config, log *slog.Logger) *Manager
func (m *Manager) Start(ctx context.Context) error   // connects all PLCs via retry.Do
func (m *Manager) Stop() error                        // disconnects all PLCs
func (m *Manager) Driver(name string) (Driver, bool)  // lookup by PLC name
func (m *Manager) Reload(cfg *config.Config) error    // hot-reload: drain old, start new
```

---

## 5. Architecture Decisions

| # | Decision | Choice | Rejected alternatives | Rationale |
|---|----------|--------|-----------------------|-----------|
| 1 | AutoConnect disabled | Set `client.AutoConnect = false` in constructor; panic if caller passes `true` | Leave AutoConnect on | Prevents double-reconnect race inside gologix; our `retry.Do` controls reconnection explicitly |
| 2 | One client per PLC | `gologixDriver` holds exactly one `*gologix.Client` | Connection pool, shared client | gologix serializes all I/O through an internal mutex; multiple clients per PLC waste CIP sessions |
| 3 | Manager owns lifecycle | `Manager.Start`/`Stop` called from `server.Run` | Drivers self-manage; driver pool | Consistent with `server.Server` pattern; clean shutdown ordering |
| 4 | retry.Do for reconnection | `retry.Do(ctx, opts, driver.Connect)` with Initial=1s, Max=30s | Custom reconnect loop; gologix KeepAlive | Reuses existing `internal/retry`; exponential backoff prevents CIP session slot exhaustion |
| 5 | Error sentinel translation | `translateError()` maps `*gologix.CIPError` to `ErrPLCRead`/`ErrPLCWrite`; net errors to `ErrPLCConnect` | Pass gologix errors raw | Isolates beta library errors from domain; `errors.Is` works uniformly |
| 6 | SocketTimeout as context substitute | Set `client.SocketTimeout` = configured timeout | Pass `context.Context` to Read/Write | gologix `Read`/`Write` do not accept context; SocketTimeout is the only per-operation deadline mechanism |
| 7 | Phase 1: scalars/arrays only | `ReadTag`/`WriteTag` accept scalar pointers and pre-allocated slices | Support UDTs now | UDT support requires `ListAllTags` + struct reflection; deferred to Phase 2 per proposal |
| 8 | Slot via ParsePath | Build `client.Controller.Path` from `gologix.ParsePath(fmt.Sprintf("1,%d", slot))` | Hardcode "1,0" | Supports multi-slot racks; default 0 matches existing plcsim |
| 9 | Manager.Reload drains then creates | On config change: `Close()` old drivers, create new ones; in-flight ops see connection drop | Atomic swap with sync.RWMutex | Simpler; in-flight reads fail fast and retry naturally; avoids complex locking |
| 10 | Doctor check is TCP dial only | `net.DialTimeout("tcp", addr, 3s)` -- no CIP handshake | Full CIP Connect probe | Doctor checks must be fast and side-effect-free; CIP connect allocates a session slot |

---

## 6. Sequence Diagrams

### 6.1 Connection Lifecycle (Manager.Start)

```
server.Run       Manager        retry.Do       gologixDriver   gologix.Client
    |               |               |               |               |
    | Start(ctx) -->|               |               |               |
    |               | for each PLC: |               |               |
    |               | Do(ctx,opts,  |               |               |
    |               |   connect) -->|               |               |
    |               |               | connect() -->|               |
    |               |               |               | NewClient() ->|
    |               |               |               | .AutoConnect=false
    |               |               |               | .SocketTimeout=cfg
    |               |               |               | .Path=ParsePath
    |               |               |               | Connect() --->|
    |               |               |               |<-- err/nil ---|
    |               |               |<-- err/nil ---|               |
    |               |               | (retry on err)|               |
    |               |<-- nil -------|               |               |
    |<-- nil -------|               |               |               |
```

### 6.2 Tag Read with Error Translation

```
caller        gologixDriver    gologix.Client    translateError
  |               |                  |                  |
  | ReadTag() --->|                  |                  |
  |               | Read(tag,dest)-->|                  |
  |               |<-- err ----------|                  |
  |               | translateError(err,"read") -------->|
  |               |                  |    *CIPError? -->ErrPLCRead
  |               |                  |    net error? -->ErrPLCConnect
  |               |                  |    nil?       -->nil
  |               |<--- wrapped sentinel ---------------|
  |<-- error -----|                  |                  |
```

### 6.3 Hot-Reload

```
config.Watcher    server.onChange    Manager         old Driver    new Driver
     |                  |               |               |             |
     | config change -->|               |               |             |
     |                  | Reload(cfg) ->|               |             |
     |                  |               | Stop old:     |             |
     |                  |               | Close() ----->|             |
     |                  |               |<-- nil -------|             |
     |                  |               | create new drivers          |
     |                  |               | Start(ctx):   |             |
     |                  |               | Connect() ----|------------>|
     |                  |               |<-- nil -------|-------------|
     |                  |<-- nil -------|               |             |
```

### 6.4 Reconnect on Failure

```
scanLoop       Driver.ReadTag    retry.Do     gologixDriver   gologix.Client
  |                 |               |               |               |
  | ReadTag() ----->|               |               |               |
  |                 | Read() ------>|               |               |
  |                 |<-- ErrPLCConnect (net.OpError) |               |
  |<-- err ---------|               |               |               |
  | (caller decides to reconnect)   |               |               |
  | retry.Do(ctx, reconnect) ------>|               |               |
  |                 |               | Disconnect -->|               |
  |                 |               | Connect() --->|  Connect() -->|
  |                 |               |<-- nil -------|<-- nil -------|
  |<-- nil ---------|---------------|               |               |
```

---

## 7. Config Schema Additions

```go
// PLC holds CIP/gologix PLC settings (extended from Phase 0).
type PLC struct {
    Name       string `koanf:"name"`
    Address    string `koanf:"address"`
    ScanRateMs int    `koanf:"scanRateMs"`  // polling interval (ms); default 1000; validated > 0
    Timeout    string `koanf:"timeout"`     // Go duration string; default "10s"; parsed via time.ParseDuration
    SlotNo     int    `koanf:"slotNo"`      // backplane slot; default 0; validated >= 0
}
```

Validation additions to `Config.Validate()`:

- `plcs[i].name`: MUST be non-empty
- `plcs[i].address`: MUST be non-empty
- `plcs[i].scanRateMs`: MUST be > 0 when set (0 uses default 1000)
- `plcs[i].timeout`: MUST parse as valid `time.Duration` when non-empty
- `plcs[i].slotNo`: MUST be >= 0
- `plcs[i].name`: MUST be unique across all PLCs

---

## 8. Error Model

New sentinels in `internal/errors/errors.go`:

```go
var (
    ErrPLCConnect = errors.New("plc connect failed")
    ErrPLCRead    = errors.New("plc read failed")
    ErrPLCWrite   = errors.New("plc write failed")
    ErrPLCTimeout = errors.New("plc timeout")
)
```

Translation rules in `internal/plc/errors.go`:

| gologix error | Sentinel | Wrapping |
|---------------|----------|----------|
| `*gologix.CIPError` on Read | `ErrPLCRead` | `fmt.Errorf("plc: read %q: %w: %w", tag, ErrPLCRead, origErr)` |
| `*gologix.CIPError` on Write | `ErrPLCWrite` | same pattern |
| `net.OpError`, connection refused, EOF | `ErrPLCConnect` | wraps original |
| `SocketTimeout` exceeded (detected via net timeout) | `ErrPLCTimeout` | wraps original |
| "not connected and AutoConnect not enabled" | `ErrPLCConnect` | wraps original |

All sentinels support `errors.Is` traversal. Re-exported in `internal/plc` for ergonomic use: `var ErrPLCConnect = errs.ErrPLCConnect`.

---

## 9. Doctor Integration

New check registered in `doctor.Default()` when `len(cfg.PLCs) > 0`:

```go
type plcReachableCheck struct {
    plcs []config.PLC
}

func (c *plcReachableCheck) Name() string { return "plc-reachable" }

func (c *plcReachableCheck) Run(ctx context.Context) Result {
    // TCP dial each PLC address with 3s timeout.
    // StatusPass if all reachable; StatusFail listing unreachable addresses.
}
```

The check does NOT establish a CIP session -- pure TCP probe. This avoids allocating PLC connection slots during diagnostics.

---

## 10. Server Wiring

`server.New` gains an optional `*plc.Manager` parameter:

```go
func New(cfg *config.Config, log *slog.Logger, checks []doctor.Check, plcMgr *plc.Manager) *Server
```

In `Server.Run(ctx)`:
1. If `plcMgr != nil`, call `plcMgr.Start(ctx)` before serving HTTP.
2. On ctx cancellation, call `plcMgr.Stop()` before `httpx.Shutdown`.
3. Config watcher's `onChange` calls `plcMgr.Reload(newCfg)` when PLC config changes.

This maintains the existing pattern where `server.Run` orchestrates all subsystem lifecycles.

---

## 11. Testing Strategy

| Layer | What | Approach |
|-------|------|----------|
| Unit | `Driver` interface contract, `Options` validation, error translation, Manager state machine | Mock `Driver` implementation in test file; no gologix import |
| Unit | Config validation for new PLC fields | Table-driven tests in `config_test.go` |
| Unit | Doctor `plc-reachable` check | Fake TCP listener for pass; closed port for fail |
| Integration | `gologixDriver.ReadTag`/`WriteTag` against plcsim | `//go:build integration`; `testutil.StartPLCSim(t)` |
| Integration | Manager start/stop/reload lifecycle | `//go:build integration`; real plcsim |
| Integration | Reconnect via retry after simulated disconnect | `//go:build integration`; stop/restart plcsim mid-test |

Strict TDD: first commit in each work-unit is a failing test. Test runner: `go test ./... -race -count=1`.

---

## 12. File Changes

| Path | Action | Description |
|------|--------|-------------|
| `internal/plc/driver.go` | Create | Driver interface, Options, TagValue types |
| `internal/plc/gologix.go` | Create | gologixDriver adapter wrapping `*gologix.Client` |
| `internal/plc/manager.go` | Create | Manager: lifecycle for all PLC drivers |
| `internal/plc/errors.go` | Create | `translateError` helper |
| `internal/plc/doc.go` | Create | Package documentation |
| `internal/plc/driver_test.go` | Create | Unit tests with mock Driver |
| `internal/plc/gologix_integration_test.go` | Create | Integration tests with plcsim |
| `internal/plc/manager_test.go` | Create | Manager lifecycle unit tests |
| `internal/errors/errors.go` | Modify | Add `ErrPLCConnect`, `ErrPLCRead`, `ErrPLCWrite`, `ErrPLCTimeout` |
| `internal/config/config.go` | Modify | Add `ScanRateMs`, `Timeout`, `SlotNo` fields to `PLC` struct; add validation |
| `internal/doctor/checks.go` | Modify | Add `plcReachableCheck` |
| `internal/doctor/doctor.go` | Modify | Register `plcReachableCheck` in `Default()` when PLCs configured |
| `internal/server/server.go` | Modify | Accept `*plc.Manager`; start/stop in `Run()` |

---

## 13. Non-Functional Considerations

| Concern | Bound | Rationale |
|---------|-------|-----------|
| Max PLCs per gateway | Soft limit: 16 (validated in config) | Each PLC = 1 goroutine + 1 TCP connection; 16 is generous for edge |
| Memory per PLC connection | ~2 KB (gologix Client + buffers) | Measured from gologix source; negligible |
| Scan rate floor | 100ms minimum (config validation) | Prevents tight-loop CPU burn on slow PLCs |
| SocketTimeout default | 10s (matches gologix `socketTimeoutDefault`) | Balances responsiveness vs. transient network glitches |
| Reconnect backoff | Initial=1s, Max=30s, Jitter=0.25 | Prevents CIP session slot exhaustion on rapid reconnect |

---

## 14. Migration / Rollout

Additive change. No data migration. No feature flags. Rollback: delete `internal/plc/`, revert sentinel additions in `internal/errors/errors.go`, revert PLC struct fields in `internal/config/config.go`, remove `plc-reachable` doctor check registration, revert `server.New` signature. All reversions are safe because new fields have zero-value defaults.

---

## 15. Open Questions

None -- all decisions needed for `sdd-tasks` are locked in this document.
