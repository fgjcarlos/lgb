---
change: plc-driver
phase: spec
domain: plc
date: 2026-05-23
status: draft
type: new
---

# PLC Driver Specification

## Purpose

Defines the `internal/plc` package: the `Driver` interface contract, the `gologixDriver` concrete adapter, connection lifecycle, tag I/O, manager lifecycle, hot-reload behaviour, thread-safety guarantees, and Phase 1 scope restrictions. This package is the sole gateway between the rest of the system and the `danomagnum/gologix` library.

---

## Requirements

### [PLC-DRV-1.1] Driver interface

The `internal/plc` package MUST define a `Driver` interface with the following methods:

```
Connect(ctx context.Context) error
Disconnect() error
ReadTag(tag string, dest any) error
WriteTag(tag string, val any) error
ReadMulti(tags []string, dests []any) error
Connected() bool
```

All other packages MUST depend on `Driver`, never on the concrete `gologixDriver` type or on `danomagnum/gologix` types directly.

#### Scenario: Interface satisfied by adapter

- GIVEN `gologixDriver` is instantiated via `NewDriver(cfg config.PLC, opts ...Option) Driver`
- WHEN the returned value is assigned to a `Driver`-typed variable
- THEN it compiles without error and all six methods are available

---

### [PLC-DRV-1.2] Options struct

The `internal/plc` package MUST expose an `Options` struct (or functional-option pattern) that carries:

| Field | Type | Meaning |
|-------|------|---------|
| `RetryInitial` | `time.Duration` | First reconnect backoff; default 1 s |
| `RetryMax` | `time.Duration` | Maximum reconnect backoff; default 30 s |
| `MaxAttempts` | `int` | Max connect attempts before giving up (0 = unlimited) |

Zero values MUST resolve to the stated defaults.

---

### [PLC-DRV-1.3] Constructor enforces AutoConnect=false

`NewDriver` MUST create the underlying `gologix.Client` with `AutoConnect` set to `false`. This is non-negotiable: the library's built-in reconnect races with the driver's own retry loop and causes double-connection attempts.

#### Scenario: AutoConnect is disabled

- GIVEN `NewDriver` is called
- WHEN the returned `Driver.Connect(ctx)` has not yet been called
- THEN no outbound TCP connection exists to the PLC address

---

### [PLC-DRV-1.4] Connect — context-aware with retry

`Connect(ctx context.Context) error` MUST:

1. Delegate to `internal/retry.Do` with `RetryInitial`, `RetryMax`, and `MaxAttempts` from `Options`.
2. Return `ctx.Err()` immediately if the context is cancelled before or during any retry.
3. Wrap any underlying gologix / CIP connection error as `ErrPLCConnect` using `fmt.Errorf("plc connect %s: %w: %w", addr, ErrPLCConnect, underlying)`.
4. Mark the driver as connected (affect `Connected()` return value) only after a successful dial.

#### Scenario: Connect succeeds on first attempt

- GIVEN a plcsim is reachable at the configured address
- WHEN `Connect(ctx)` is called
- THEN it returns nil
- AND `Connected()` returns `true`

#### Scenario: Connect retries after transient failure

- GIVEN the first two attempts fail with a connection-refused error
- AND the third attempt succeeds
- WHEN `Connect(ctx)` is called
- THEN it returns nil after the third attempt
- AND the retry backoff is applied between attempts

#### Scenario: Connect respects context cancellation

- GIVEN `Connect(ctx)` is in a retry loop
- WHEN the context is cancelled
- THEN `Connect` returns `ctx.Err()` without further attempts
- AND `Connected()` returns `false`

#### Scenario: Connect exhausts attempts

- GIVEN `MaxAttempts` is set to 3 and all three attempts fail
- WHEN `Connect(ctx)` is called
- THEN it returns an error wrapping both `ErrPLCConnect` and `retry.ErrMaxAttempts`
- AND `errors.Is(err, ErrPLCConnect)` returns `true`
- AND `errors.Is(err, retry.ErrMaxAttempts)` returns `true`

---

### [PLC-DRV-1.5] Disconnect

`Disconnect() error` MUST:

1. Close the underlying gologix client.
2. Mark the driver as disconnected (affect `Connected()` return value).
3. Be idempotent: calling it on an already-disconnected driver MUST return nil without panicking.

#### Scenario: Disconnect from connected state

- GIVEN `Connect(ctx)` has succeeded
- WHEN `Disconnect()` is called
- THEN it returns nil
- AND `Connected()` returns `false`

#### Scenario: Disconnect is idempotent

- GIVEN `Disconnect()` has already been called
- WHEN `Disconnect()` is called a second time
- THEN it returns nil without panicking

---

### [PLC-DRV-1.6] ReadTag — scalar and array tags (Phase 1 scope)

`ReadTag(tag string, dest any) error` MUST:

1. Call gologix's tag-read primitive with `tag` and `dest`.
2. Translate any returned `*gologix.CIPError` to `ErrPLCRead` using `fmt.Errorf("read %s: %w: %w", tag, ErrPLCRead, cipErr)`.
3. Return `ErrPLCTimeout` (wrapping the underlying timeout error) when the operation exceeds `SocketTimeout`.
4. MUST NOT accept UDT / struct pointer destinations in Phase 1 — pass-through is fine but undefined behaviour for UDTs is explicitly out of scope.

Phase 1 restriction: only `bool`, `int8`, `int16`, `int32`, `int64`, `uint8`, `uint16`, `uint32`, `uint64`, `float32`, `float64`, and their slice forms (`[]bool` with length multiple of 32, `[]int16`, etc.) are supported destinations. All other types MAY produce undefined results and SHOULD NOT be used.

#### Scenario: ReadTag succeeds for scalar

- GIVEN the PLC has tag `SimInt` of type INT (int16)
- AND the driver is connected
- WHEN `ReadTag("SimInt", &v)` is called where `v` is `int16`
- THEN it returns nil
- AND `v` equals the PLC tag value

#### Scenario: ReadTag translates CIP error to ErrPLCRead

- GIVEN the PLC returns a CIP error for an unknown tag
- WHEN `ReadTag("NoSuchTag", &v)` is called
- THEN it returns a non-nil error
- AND `errors.Is(err, ErrPLCRead)` returns `true`

#### Scenario: ReadTag times out

- GIVEN `SocketTimeout` is set to 10 ms
- AND the PLC is unreachable (no response)
- WHEN `ReadTag("SimBool", &v)` is called
- THEN it returns a non-nil error within ~10 ms
- AND `errors.Is(err, ErrPLCTimeout)` returns `true`

#### Scenario: bool array length must be multiple of 32

- GIVEN `dest` is `[]bool` of length 10 (not a multiple of 32)
- WHEN `ReadTag("SomeArray", &dest)` is called
- THEN it returns an error wrapping `ErrPLCRead` before calling gologix
- AND the error message mentions "length must be a multiple of 32"

---

### [PLC-DRV-1.7] WriteTag

`WriteTag(tag string, val any) error` MUST:

1. Call gologix's tag-write primitive with `tag` and `val`.
2. Translate any returned `*gologix.CIPError` to `ErrPLCWrite`.
3. Return `ErrPLCTimeout` when the operation exceeds `SocketTimeout`.

#### Scenario: WriteTag succeeds

- GIVEN the PLC has a writable tag `SimFloat`
- AND the driver is connected
- WHEN `WriteTag("SimFloat", float32(1.5))` is called
- THEN it returns nil
- AND a subsequent `ReadTag("SimFloat", &v)` returns `float32(1.5)`

#### Scenario: WriteTag translates CIP error to ErrPLCWrite

- GIVEN the PLC returns a CIP error for a read-only tag
- WHEN `WriteTag("ReadOnlyTag", 0)` is called
- THEN `errors.Is(err, ErrPLCWrite)` returns `true`

---

### [PLC-DRV-1.8] ReadMulti — batch read

`ReadMulti(tags []string, dests []any) error` MUST:

1. Require `len(tags) == len(dests)` — return `ErrPLCRead` immediately if lengths differ.
2. Execute a single multi-tag read if gologix supports it, or fall back to sequential `ReadTag` calls.
3. Return the first error encountered; partial results in `dests` for completed reads before the error are valid.

#### Scenario: ReadMulti returns error on length mismatch

- GIVEN `tags` has 3 elements and `dests` has 2 elements
- WHEN `ReadMulti(tags, dests)` is called
- THEN it returns a non-nil error wrapping `ErrPLCRead`
- AND `errors.Is(err, ErrPLCRead)` returns `true`

#### Scenario: ReadMulti reads all tags in one call

- GIVEN `tags = ["SimBool", "SimInt", "SimFloat"]` and correctly typed `dests`
- AND the driver is connected to plcsim
- WHEN `ReadMulti(tags, dests)` is called
- THEN it returns nil
- AND all three `dests` are populated with the expected values

---

### [PLC-DRV-1.9] SocketTimeout as operation deadline

Because gologix does not propagate `context.Context` on Read/Write, the driver MUST set the `SocketTimeout` field on the `gologix.Client` to `config.PLC.SocketTimeout` at construction time. This is the sole per-operation deadline mechanism for Phase 1.

The driver MUST document this limitation in its package-level godoc comment.

---

### [PLC-DRV-1.10] Thread safety

The `Driver` interface implementation MUST be safe for concurrent calls from multiple goroutines. The gologix client serialises internally via a mutex; the driver MUST NOT add a second lock that could cause deadlock, but MUST protect its own state fields (`connected` flag, etc.) with a `sync.Mutex` or `sync/atomic`.

#### Scenario: Concurrent reads do not race

- GIVEN 10 goroutines each call `ReadTag("SimInt", &v)` concurrently
- WHEN `go test -race` is run
- THEN no data race is reported

---

### [PLC-DRV-2.1] Manager — one goroutine per PLC

The `internal/plc` package MUST expose a `Manager` type that:

1. Accepts a `[]config.PLC` slice and creates one `Driver` per entry.
2. Starts one goroutine per PLC on `Manager.Start(ctx)`.
3. Each goroutine calls `Connect` (with retry per PLC-DRV-1.4), then runs a scan loop at `config.PLC.ScanRate` interval.
4. Stops all goroutines and calls `Disconnect` on each driver when `Manager.Stop()` is called.
5. `Stop()` MUST block until all goroutines exit (use a `sync.WaitGroup`).

#### Scenario: Manager starts and stops cleanly

- GIVEN a `Manager` with one PLC pointed at plcsim
- WHEN `Start(ctx)` followed by `Stop()` is called
- THEN `Stop()` returns within 2 s with no goroutine leak
- AND `go test -race` reports no data races

#### Scenario: Manager stops all drivers on context cancel

- GIVEN `Start(ctx)` is running with a cancellable context
- WHEN the context is cancelled
- THEN each PLC goroutine exits
- AND `Stop()` returns without deadlock

---

### [PLC-DRV-2.2] Scan loop

Each per-PLC goroutine MUST implement a scan loop that:

1. Reads the tags configured under the PLC entry at every `ScanRate` interval.
2. Logs failures at WARN level and continues (does not exit the goroutine on a read error).
3. Attempts reconnect via `Connect` (with retry) when the driver detects disconnection.
4. Respects the parent context: exits when `ctx` is cancelled.

Scan rate precision: the loop MUST use `time.Ticker` (not `time.Sleep`) to maintain consistent intervals. Drift caused by read latency is acceptable; the ticker MUST NOT be recreated on each iteration.

#### Scenario: Scan loop continues after transient read error

- GIVEN the PLC is connected and scan is running
- WHEN one `ReadTag` call returns an error
- THEN the goroutine logs the error at WARN
- AND the next tick proceeds normally without exiting

#### Scenario: Scan loop reconnects after disconnection

- GIVEN the PLC connection drops mid-scan
- WHEN the next ReadTag returns an error indicating disconnection
- THEN the goroutine calls `Connect` with retry
- AND resumes scanning once reconnected

---

### [PLC-DRV-2.3] Hot-reload — drain-and-swap

When the gateway receives a config reload event (via `internal/config.Watch`), the `Manager` MUST:

1. Detect which PLCs were added, removed, or changed.
2. For removed or changed PLCs: call `Disconnect` on the old driver, signal the goroutine to stop, wait for it to exit.
3. For added or changed PLCs: create a new driver, start a new goroutine.
4. In-flight `ReadTag`/`WriteTag` operations on the old driver see the disconnect and return an error; callers MUST handle this.
5. The swap MUST complete without data races. Use context cancellation to signal goroutines, then wait via `WaitGroup`.

#### Scenario: PLC address change triggers drain-and-swap

- GIVEN a `Manager` running one PLC
- WHEN the config reload changes the PLC's address
- THEN the old driver is disconnected
- AND a new driver is started with the updated address
- AND `go test -race` reports no data races

#### Scenario: PLC removal stops its goroutine

- GIVEN a `Manager` running PLCs A and B
- WHEN a config reload removes PLC B
- THEN PLC B's goroutine stops and `Disconnect` is called for B
- AND PLC A's goroutine continues unaffected

---

### [PLC-DRV-2.4] CGO_ENABLED=0 compliance

The `internal/plc` package and all its transitive dependencies MUST compile with `CGO_ENABLED=0` on all four target platforms:

- `linux/amd64`
- `linux/arm64`
- `darwin/arm64`
- `windows/amd64`

`gologix` v0.41.0-beta is pure-Go; no C bindings are used. The CI matrix MUST include a build check for each platform. No `import "C"` statement MAY appear anywhere in the package tree.

#### Scenario: Cross-platform build succeeds

- GIVEN `GOARCH` and `GOOS` are set for each target platform
- WHEN `CGO_ENABLED=0 go build ./internal/plc/...` is run
- THEN the build succeeds with exit code 0 on all four targets

---

### [PLC-DRV-2.5] Integration tests with plcsim

The `internal/plc` package MUST contain integration tests that:

1. Call `testutil.StartPLCSim(t)` to obtain a real in-process CIP server.
2. Instantiate `gologixDriver` with the returned address.
3. Call `Connect`, `ReadTag` (for `SimBool`, `SimInt`, `SimFloat`), `WriteTag`, and `Disconnect`.
4. Assert all operations return nil error and correct values.
5. Run with `-race` without data race reports.
6. Are tagged or named to distinguish from unit tests (e.g. function name prefix `TestIntegration_`).

Unit tests MUST use a `MockDriver` implementing the `Driver` interface — no plcsim dependency.

#### Scenario: Integration test reads canonical fixture tags

- GIVEN `StartPLCSim(t)` returns a reachable address
- WHEN `ReadTag("SimBool", &b)`, `ReadTag("SimInt", &i)`, `ReadTag("SimFloat", &f)` are called
- THEN `b == true`, `i == int16(42)`, `f == float32(3.14)`

---

### [PLC-DRV-2.6] Phase 1 scope exclusions (explicit)

The following are explicitly OUT OF SCOPE for this change and MUST NOT be implemented:

- UDT / structured-type tag reads or writes
- Tag browsing or discovery
- Multi-client fan-out or connection pooling
- MQTT publish pipeline
- OPC UA driver

Any attempt to pass a UDT pointer as `dest` to `ReadTag` results in undefined behaviour. The package godoc MUST state this explicitly.
