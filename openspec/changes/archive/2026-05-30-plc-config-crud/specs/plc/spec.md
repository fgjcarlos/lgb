---
change: plc-config-crud
phase: spec
domain: plc
date: 2026-05-29
status: draft
type: delta
---

# Delta for PLC

## MODIFIED Requirements

### [PLC-DRV-2.3] Hot-reload — drain-and-swap (field-level + store-driven)

When the gateway receives a config reload event OR when the store mutation path calls `Reload` directly, the `Manager` MUST:

1. Detect which PLCs were added, removed, or changed (comparing by name AND by all config fields including tags).
2. For removed or changed PLCs: call `Disconnect` on the old driver, signal the goroutine to stop, wait for it to exit.
3. For added or changed PLCs: create a new driver, start a new goroutine.
4. In-flight `ReadTag`/`WriteTag` operations on the old driver see the disconnect and return an error; callers MUST handle this.
5. The swap MUST complete without data races. Use context cancellation to signal goroutines, then wait via `WaitGroup`.
6. PLCs whose config is identical (name, address, slot, socketTimeout, scanRate, keepAlive, path, and all tags) MUST NOT be restarted.
7. `Reload` MUST accept a `[]config.PLC` slice directly (sourced from the store, not from a file) so it can be called from the mutation path without the file watcher.

(Previously: Reload compared PLCs by name only — added/removed workers were handled but workers whose config changed in-place were NOT restarted. Reload was only invoked via the file watcher callback.)

#### Scenario: PLC address change triggers drain-and-swap

- GIVEN a Manager running one PLC
- WHEN `Reload` is called with the same PLC name but a different `address`
- THEN the old driver is disconnected and its goroutine stops
- AND a new driver is started with the updated address
- AND `go test -race` reports no data races

#### Scenario: PLC scanRate change triggers drain-and-swap

- GIVEN a Manager running PLC "Silo-1" with `scanRate="1s"`
- WHEN `Reload` is called with "Silo-1" having `scanRate="500ms"`
- THEN the old worker for "Silo-1" is stopped
- AND a new worker is started with the updated scanRate

#### Scenario: PLC tag list change triggers drain-and-swap

- GIVEN a Manager running PLC "Silo-1" with one configured tag
- WHEN `Reload` is called with "Silo-1" having a different tag set
- THEN the old worker is stopped and a new worker is started
- AND `go test -race` reports no data races

#### Scenario: Unchanged PLC is not restarted

- GIVEN a Manager running PLCs "A" and "B"
- WHEN `Reload` is called and only PLC "A"'s scanRate changes
- THEN PLC "A"'s worker is restarted
- AND PLC "B"'s worker continues running without interruption

#### Scenario: PLC removal stops its goroutine

- GIVEN a Manager running PLCs A and B
- WHEN `Reload` is called with PLC B absent from the list
- THEN PLC B's goroutine stops and `Disconnect` is called for B
- AND PLC A's goroutine continues unaffected

#### Scenario: Reload called directly from mutation path

- GIVEN a Manager is running (not nil)
- WHEN the store mutation handler calls `plcMgr.Reload(ctx, updatedList)` directly (no file watcher involved)
- THEN Reload applies the same drain-and-swap logic as a file-watcher-triggered reload
