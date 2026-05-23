# ADR-0004: PLC driver — `danomagnum/gologix` v0.41.0-beta

**Date**: 2026-05-23
**Status**: Proposed

## Decision

Use `github.com/danomagnum/gologix` v0.41.0-beta as the CIP/EtherNet-IP driver for communicating with Allen-Bradley / Rockwell Automation PLCs. In Phase 0 it is used exclusively by `cmd/plcsim` and `internal/testutil` to run an in-process CIP simulator — no production gateway code touches gologix directly yet.

## Context

LGB targets Rockwell PLCs (ControlLogix, CompactLogix) which use the CIP protocol over EtherNet-IP. The driver must be pure-Go (`CGO_ENABLED=0`) to satisfy ADR-0009. Phase 0 requires only an in-process simulator for CI smoke tests; the full gateway integration is deferred to the `plc-driver` change.

## Options Considered

| Option | Pros | Cons |
|--------|------|------|
| `danomagnum/gologix` v0.41.0-beta | Pure-Go; CIP read/write + MapTagProvider for simulation; active development | Beta API surface; breaking changes expected |
| `stellviaproject/ethernetip` | Lightweight | Incomplete CIP coverage; no simulator |
| `libplctag/libplctag-go` bindings | Mature C library underneath | Requires CGo — violates ADR-0009 |
| Custom CIP implementation | No external dep | ~10 kloc; not feasible for Phase 0 |

## Rationale

gologix is the only pure-Go CIP library with sufficient coverage (read/write tags, `MapTagProvider` for deterministic simulation) for the LGB use case. The beta status is acceptable in Phase 0 because the library is wrapped behind `internal/testutil` — the risk surface is isolated. The `plc-driver` change will add a stable adapter interface before gologix is exposed to production paths.

## Consequences

- **Accepted**: Beta API — pin at `v0.41.0-beta` and expect breaking changes on minor bumps. All changes are in `cmd/plcsim` and `internal/testutil`, so breakage is contained.
- **Monitor**: gologix releases and API stability. Track the upstream issue tracker for v1.0 roadmap.
- **Revisit**: At the `plc-driver` change, evaluate whether gologix has stabilised or whether a competing library has matured.

## References

- Spec: MVP-FND-9.2
- Design: §12, §16 (pure-Go verification)
- Upstream: https://github.com/danomagnum/gologix
