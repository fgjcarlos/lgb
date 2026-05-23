---
change: plc-driver
phase: proposal
date: 2026-05-23
status: draft
---

# Proposal: PLC Driver — CIP Communication via gologix

## Intent

The LGB gateway cannot read or write PLC tags. Without a driver layer, the gateway is disconnected from its primary data source: Rockwell ControlLogix/CompactLogix PLCs over CIP/EtherNet-IP. This change introduces a thin adapter around `danomagnum/gologix` that owns connection lifecycle, tag I/O, error translation, and retry integration so the rest of the gateway depends on a stable `Driver` interface, not a beta library. Ref: GitHub issue #8, ADR-0004.

## Scope

### In Scope

- `internal/plc` package: `Driver` interface, `Options` struct, concrete `gologixDriver` adapter
- Connection lifecycle: `Connect`, `Close`, reconnect via `internal/retry`
- Tag read/write: `ReadTag`, `WriteTag` with type-safe results
- Error sentinels: `ErrPLCConnect`, `ErrPLCRead`, `ErrPLCWrite`, `ErrPLCTimeout` in `internal/errors`
- Config: extend `PLC` struct with `ScanRateMs`, `Timeout`, `SlotNo` fields
- Doctor: `plc-reachable` check (TCP dial to each configured PLC address)
- Server lifecycle: `Start`/`Stop` PLC drivers alongside gateway startup/shutdown
- Hot-reload: re-create driver on PLC config change (drain old, start new)
- Integration tests against `internal/testutil.StartPLCSim`

### Out of Scope

- UDT / structured types (Phase 2 — restrict to scalars and arrays)
- Tag browsing / discovery
- Multi-client fan-out or connection pooling
- MQTT publish pipeline (separate change)
- OPC UA driver
- Scan-loop / polling scheduler (separate change after driver lands)

## Capabilities

### New Capabilities

- `plc-driver`: CIP tag read/write adapter with retry, error sentinels, and lifecycle management

### Modified Capabilities

- `config`: add PLC-specific fields (`scanRateMs`, `timeout`, `slotNo`) to existing `PLC` struct; add PLC config validation rules
- `errors`: add PLC-domain sentinels
- `doctor`: add `plc-reachable` check

## Approach

**Thin adapter** wrapping one `*gologix.Client` per configured PLC.

1. `Driver` interface isolates gologix behind `ReadTag(tag string, dest any) error` / `WriteTag(tag string, val any) error` / `Connect(ctx) error` / `Close() error`.
2. `gologixDriver` creates a client with `AutoConnect: false` to prevent the double-reconnect race. Our `retry.Do` loop calls `Connect()` on failure.
3. `SocketTimeout` on the gologix client serves as the per-operation deadline (gologix has no `context.Context` on Read/Write).
4. CIP errors (`*gologix.CIPError`) are translated to project sentinels at the adapter boundary.
5. One goroutine per PLC is the practical ceiling (gologix serializes via internal mutex).
6. Hot-reload drains the old driver (`Close`) and creates a new one; in-flight operations see the context cancel.

## Affected Areas

| Area | Impact | Description |
|------|--------|-------------|
| `internal/plc/` | New | Driver interface + gologix adapter |
| `internal/plc/plc_test.go` | New | Unit tests with mock; integration tests with plcsim |
| `internal/errors/errors.go` | Modified | Add PLC-domain sentinels |
| `internal/config/config.go` | Modified | Extend `PLC` struct fields + validation |
| `internal/doctor/` | Modified | Add `plc-reachable` check |
| `cmd/lgb/` | Modified | Wire PLC driver start/stop in server lifecycle |

## Risks

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| gologix beta API breaks on upgrade | Med | Pin v0.41.0-beta; `Driver` interface absorbs churn |
| Double-reconnect race | High if misconfigured | Set `AutoConnect: false`; enforce in constructor |
| No context on gologix Read/Write | Certain | Use `SocketTimeout`; document limitation |
| CIP session slot exhaustion under rapid reconnect | Low | Retry backoff (initial 1s, max 30s); `Close` before reconnect |
| `[]bool` length must be multiple of 32 | Low | Validate in adapter; return `ErrPLCRead` on violation |
| Hot-reload mid-connection race | Med | Drain with context cancel + wait; atomic swap |

## Rollback Plan

1. Revert the `internal/plc/` package (single directory delete).
2. Revert sentinel additions in `internal/errors/errors.go` (additive-only; safe to leave).
3. Revert `PLC` struct field additions in `internal/config/config.go` (YAML is backward-compatible; new fields have defaults).
4. Remove `plc-reachable` doctor check registration.
5. No data migration required. No database changes. No external service dependency beyond PLC hardware (which is simulated in CI).

## Dependencies

- `danomagnum/gologix` v0.41.0-beta (already in go.mod as indirect; promote to direct)
- `internal/retry` (existing, no changes needed)
- `internal/errors` (existing, additive sentinels only)
- `internal/testutil.StartPLCSim` (existing test helper)

## Cross-Platform Considerations

- gologix is pure-Go (`CGO_ENABLED=0`); compiles on all four targets (linux/amd64, linux/arm64, darwin/arm64, windows/amd64).
- TCP dial in doctor check uses `net.DialTimeout` — platform-portable.
- No filesystem, signal, or OS-specific code in `internal/plc/`.

## Success Criteria

- [ ] `Driver` interface defined; `gologixDriver` implements it
- [ ] `ReadTag` / `WriteTag` succeed against `StartPLCSim` in integration tests
- [ ] `retry.Do` reconnects after simulated disconnect
- [ ] PLC error sentinels are testable with `errors.Is`
- [ ] Config validation rejects invalid PLC entries (empty address, negative timeout)
- [ ] `plc-reachable` doctor check passes against plcsim, fails against unreachable address
- [ ] `go build ./...` succeeds on all four target platforms with `CGO_ENABLED=0`
- [ ] All tests pass with `-race`
