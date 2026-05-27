# ADR-0005: OPC UA library — `gopcua/opcua` v0.8.0 + mandatory Phase 1 spike; `awcullen/opcua` named contingency

**Date**: 2026-05-23
**Status**: Accepted (spike passed 2026-05-27)

## Decision

Use `github.com/gopcua/opcua` v0.8.0 as the OPC UA client library for the `opcua-bridge` change (Phase 1). Before that change begins, a mandatory spike MUST verify that `gopcua/opcua` compiles with `CGO_ENABLED=0` on all four target platforms and that its session handling meets the LGB reliability requirements. `github.com/awcullen/opcua` is named as the contingency library if the spike fails.

## Context

LGB will act as an OPC UA server (or client bridge) exposing PLC tag data. The library must be pure-Go (`CGO_ENABLED=0`) per ADR-0009, must handle reconnections gracefully, and must not impose a CGo dependency that breaks cross-compilation. Phase 0 does NOT import `gopcua/opcua` — it is listed here as a forward decision to avoid rework when the `opcua-bridge` change begins.

## Options Considered

| Option | Pros | Cons |
|--------|------|------|
| `gopcua/opcua` v0.8.0 | Most complete OPC UA implementation in Go; security policies; session handling | Historically had CGo issues in some transitive deps (to be verified in spike) |
| `awcullen/opcua` | Modern API; pure-Go confirmed; binary protocol | Smaller ecosystem; fewer contributors |
| `open62541` CGo bindings | Reference C implementation; battle-tested | Requires CGo — violates ADR-0009 |
| Custom OPC UA implementation | Zero external dep | OPC UA specification is 1200+ pages; not feasible |

## Rationale

`gopcua/opcua` is the most complete Go OPC UA library with active maintenance. The mandatory Phase 1 spike gates the final decision — if `CGO_ENABLED=0` verification fails, `awcullen/opcua` is the named fallback. Naming the contingency now prevents a Phase 1 discovery spiral.

## Spike Results (2026-05-27)

`gopcua/opcua` v0.8.0 passed `CGO_ENABLED=0` cross-compilation on all four targets:
- `GOOS=linux GOARCH=amd64` — PASS
- `GOOS=linux GOARCH=arm64` — PASS
- `GOOS=darwin GOARCH=arm64` — PASS
- `GOOS=windows GOARCH=amd64` — PASS

The server API (`server.New`, `server.Start`, `server.NewNodeNameSpace`, `AddNewVariableStringNode`) works correctly with dynamic value functions for tag reads.

## Consequences

- **Accepted**: `gopcua/opcua` v0.8.0 is confirmed pure-Go and used for production.
- **Monitor**: `gopcua/opcua` upstream releases for security fixes.
- **Contingency**: `awcullen/opcua` remains as fallback if gopcua develops CGo dependencies in future versions.

## References

- Design: §19 (no Phase-0 OPC UA import)
- Upstream gopcua: https://github.com/gopcua/opcua
- Upstream awcullen: https://github.com/awcullen/opcua
- OPC UA specification: https://opcfoundation.org/developer-tools/specifications-unified-architecture
