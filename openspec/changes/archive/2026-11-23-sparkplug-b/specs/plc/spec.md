---
change: sparkplug-b
phase: spec
domain: plc
date: 2026-05-23
status: draft
type: delta
---

# Delta for PLC

## ADDED Requirements

### [SPK-PLC-3.1] TagUpdate struct

The `internal/plc` package MUST define a `TagUpdate` struct that carries a single tag read result:

| Field | Type | Description |
|-------|------|-------------|
| `PLC` | `string` | PLC name from config |
| `Tag` | `string` | Tag name as configured |
| `Value` | `any` | Decoded scalar value (one of Phase 1 types) |
| `Timestamp` | `time.Time` | Wall-clock time of the read operation |

`TagUpdate` MUST be exported and MUST NOT carry any MQTT or Sparkplug B types (no import of `internal/mqtt` or `internal/sparkplug` from `internal/plc`).

#### Scenario: TagUpdate carries correct fields

- GIVEN a scan loop reads tag "Motor.Speed" = float32(1200.5) at time T
- WHEN a TagUpdate is constructed
- THEN `TagUpdate.PLC` is the PLC name
- AND `TagUpdate.Tag` is `"Motor.Speed"`
- AND `TagUpdate.Value` is `float32(1200.5)`
- AND `TagUpdate.Timestamp` equals T

---

### [SPK-PLC-3.2] Tag publish callback in Manager

The `Manager` MUST accept an optional tag publish callback at construction time:

```
type TagCallback func(update TagUpdate)
```

When a non-nil callback is provided and a tag read succeeds in the scan loop, the Manager MUST call `callback(TagUpdate{...})` for each tag read. The callback MUST be called synchronously within the scan loop goroutine. Callers that need non-blocking behaviour MUST handle it in their callback implementation (e.g. send to a buffered channel).

#### Scenario: Callback is invoked on successful tag read

- GIVEN the Manager is constructed with a non-nil TagCallback
- AND the PLC is connected with one configured tag
- WHEN the scan loop ticks
- THEN the callback is called once with a TagUpdate for that tag

#### Scenario: No callback is a no-op

- GIVEN the Manager is constructed with a nil TagCallback
- WHEN the scan loop ticks
- THEN no panic occurs and the loop continues normally

#### Scenario: Callback is not invoked on read error

- GIVEN a tag read returns an error
- WHEN the scan loop handles the error (logs at WARN)
- THEN the callback is NOT called for that tag

---

### [SPK-PLC-3.3] Scan loop reads configured tags

The scan loop in `runWorker` MUST be updated to read all tags defined in `config.PLC.Tags` on each tick. For each tag:

1. Call `Driver.ReadTag(tag.Name, &dest)` with a destination typed according to `tag.Type`.
2. On success: call the TagCallback (if set) with the TagUpdate.
3. On error: log at WARN and continue to the next tag (do not abort the scan).

(Previously: `// Phase 1: no tag store yet` — scan loop was a no-op heartbeat. This replaces that placeholder.)

#### Scenario: All configured tags are read each tick

- GIVEN a PLC config with tags ["Motor.Speed", "Motor.Running"]
- AND the driver is connected
- WHEN one scan tick fires
- THEN ReadTag is called once for each tag name

#### Scenario: One tag failure does not skip remaining tags

- GIVEN a PLC with three tags where the second read fails
- WHEN the scan tick fires
- THEN tag 1 and tag 3 are read and the callback is invoked for them
- AND only tag 2's error is logged at WARN

---

## MODIFIED Requirements

### [PLC-DRV-2.1] Manager — one goroutine per PLC

The `internal/plc` package MUST expose a `Manager` type that:

1. Accepts a `[]config.PLC` slice and creates one `Driver` per entry.
2. Accepts an optional `TagCallback` at construction time (nil is valid).
3. Starts one goroutine per PLC on `Manager.Start(ctx)`.
4. Each goroutine calls `Connect` (with retry per PLC-DRV-1.4), then runs a scan loop at `config.PLC.ScanRate` interval reading configured tags and emitting TagUpdates via the callback.
5. Stops all goroutines and calls `Disconnect` on each driver when `Manager.Stop()` is called.
6. `Stop()` MUST block until all goroutines exit (use a `sync.WaitGroup`).

(Previously: Manager accepted no callback; scan loop was a no-op heartbeat with no tag reads.)

#### Scenario: Manager starts and stops cleanly

- GIVEN a `Manager` with one PLC and a nil callback pointed at plcsim
- WHEN `Start(ctx)` followed by `Stop()` is called
- THEN `Stop()` returns within 2 s with no goroutine leak
- AND `go test -race` reports no data races

#### Scenario: Manager stops all drivers on context cancel

- GIVEN `Start(ctx)` is running with a cancellable context
- WHEN the context is cancelled
- THEN each PLC goroutine exits
- AND `Stop()` returns without deadlock

#### Scenario: Manager with callback emits TagUpdates

- GIVEN a Manager constructed with a non-nil TagCallback
- AND one PLC with two configured tags
- WHEN the scan loop runs one tick
- THEN the callback is called twice (once per tag)

---

### [PLC-DRV-2.2] Scan loop

Each per-PLC goroutine MUST implement a scan loop that:

1. Reads the tags configured under `config.PLC.Tags` at every `ScanRate` interval.
2. Calls `TagCallback` for each successful read.
3. Logs failures at WARN level and continues (does not exit the goroutine on a read error).
4. Attempts reconnect via `Connect` (with retry) when the driver detects disconnection.
5. Respects the parent context: exits when `ctx` is cancelled.

Scan rate precision: the loop MUST use `time.Ticker` (not `time.Sleep`) to maintain consistent intervals. Drift caused by read latency is acceptable; the ticker MUST NOT be recreated on each iteration.

(Previously: scan loop was a heartbeat with no tag reads or callback invocations.)

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
