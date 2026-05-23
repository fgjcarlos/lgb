# ADR-0003: Logging — `log/slog` (stdlib)

**Date**: 2026-05-23
**Status**: Proposed

## Decision

Use `log/slog` from the Go standard library (Go 1.21+) as the structured logging framework. `log.New()` returns a `*slog.Logger` configured for JSON (production) or text (development) output. Source attachment (`AddSource: true`) is enabled only at DEBUG level to keep INFO logs grep-friendly. A thin `redactingHandler` wrapper (in `internal/log/redact.go`) scrubs secret-tagged fields before any handler processes them.

## Context

LGB runs on resource-constrained edge hardware. A third-party logging library adds binary size, CGo risk, and a maintenance dependency. As of Go 1.21, `log/slog` provides structured, levelled, JSON-capable logging in the standard library — eliminating the need for zerolog or zap for Phase 0. The redaction requirement (spec MVP-FND-4.5) needs a handler wrapper regardless of which library is chosen.

## Options Considered

| Option | Pros | Cons |
|--------|------|------|
| `log/slog` (stdlib) | Zero external dep; JSON + text out of box; composable `Handler` interface; no CGo | Slightly verbose API vs zerolog |
| `rs/zerolog` | Fast; tiny allocations; fluent API | External dep; no stdlib conformance; custom JSON format |
| `uber-go/zap` | Very fast; widely used | External dep; complex setup; overkill for Phase 0 throughput |

## Rationale

Phase 0 does not yet have Prometheus instrumentation to measure log throughput, so optimising allocations ahead of data is premature. `log/slog`'s `Handler` interface is the ideal extension point for the `redactingHandler` — wrapping is one struct and one method override. Zero external dependencies aligns with ADR-0009.

## Consequences

- **Accepted**: `log/slog` is stable since Go 1.21 and is the direction of the Go standard library. No external dep.
- **Monitor**: If throughput profiling (Phase 1) reveals logging is a bottleneck, evaluate zerolog as a `slog.Handler` drop-in.
- **Revisit**: Source-attachment default may change if structured log aggregation requires call-site data in production.

## References

- Spec: MVP-FND-4.1–4.6
- Design: §7, decisions #5, #6, #7
- Go proposal: https://pkg.go.dev/log/slog
