# ADR-0007: Historian — `modernc.org/sqlite` v1.50.1 (pure-Go)

**Date**: 2026-05-23
**Status**: Proposed

## Decision

Use `modernc.org/sqlite` v1.50.1 as the embedded database for the historian (time-series tag value storage). This library is a pure-Go transpilation of the SQLite C source — no CGo required. Phase 0 does NOT import it; this ADR is a forward decision for the `historian` change.

## Context

LGB must buffer PLC tag readings locally for resilience (network outages, broker downtime) and for historical query. The database must be embeddable (no separate process), must survive power cycles, and must compile with `CGO_ENABLED=0`. SQLite is the industry standard for embedded databases.

## Options Considered

| Option | Pros | Cons |
|--------|------|------|
| `modernc.org/sqlite` v1.50.1 | Pure-Go SQLite transpilation; no CGo; active maintenance; full SQLite feature set | Slightly larger binary than CGo SQLite; performance gap vs CGo on write-heavy loads |
| `mattn/go-sqlite3` | Most widely used Go SQLite binding | Requires CGo — violates ADR-0009 |
| `etcd/bbolt` | Pure-Go embedded KV store | Not relational; no SQL queries; limited aggregation capabilities |
| `cznic/ql` | Pure-Go embedded SQL | Unmaintained; limited SQL compatibility |
| `hazelcast/hazelcast-go-client` | Distributed | Overkill for edge; external process |

## Rationale

`modernc.org/sqlite` is the only full-featured, production-ready, pure-Go SQLite implementation. The performance gap versus `mattn/go-sqlite3` (CGo) is acceptable for Phase 0 historian workloads (sub-100Hz tag update rates on edge hardware). Using SQLite enables familiar SQL queries for historical data without a schema migration tool for Phase 0 (`migrate` integration is deferred to the `historian` change).

## Consequences

- **Accepted**: `modernc.org/sqlite` binary size overhead (~10 MB). Acceptable for LGB's distribution targets.
- **Monitor**: `modernc.org/sqlite` tracks SQLite upstream releases — verify compatibility on each SQLite major version bump.
- **Revisit**: If write throughput exceeds SQLite WAL limits at higher tag frequencies (Phase 2+), evaluate `etcd/bbolt` as a secondary time-series store with SQLite for metadata only.

## References

- Upstream: https://pkg.go.dev/modernc.org/sqlite
- SQLite documentation: https://www.sqlite.org/docs.html
- Pure-Go verification: Design §16
