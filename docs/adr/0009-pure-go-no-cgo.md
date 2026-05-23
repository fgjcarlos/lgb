# ADR-0009: Pure-Go / no CGo — all direct deps must compile `CGO_ENABLED=0` on all four targets

**Date**: 2026-05-23
**Status**: Proposed

## Decision

All direct Go dependencies of the LGB module MUST compile with `CGO_ENABLED=0` on every supported target platform: `linux/amd64`, `linux/arm64`, `darwin/arm64`, `windows/amd64`. Any proposed dependency that requires CGo MUST be rejected unless a pure-Go alternative is unavailable AND the ADR for that dependency is updated to document the exception with explicit team approval.

## Context

LGB targets edge deployment on industrial hardware (Raspberry Pi 4, industrial PCs running RTOS-adjacent Linux). Cross-compilation with `CGO_ENABLED=0` is the only reliable way to produce static binaries for all four targets from a single CI runner without target toolchains, sysroots, or multi-arch Docker builds. A CGo dependency would: (a) require a C toolchain on every build machine, (b) break Windows cross-compilation, (c) complicate Docker multi-arch builds, and (d) introduce C runtime version coupling in the final image.

## Options Considered

| Option | Pros | Cons |
|--------|------|------|
| `CGO_ENABLED=0` always; reject CGo deps | Simple CI; static binaries; no toolchain coupling | Restricts library choice (e.g. `mattn/go-sqlite3` is excluded) |
| `CGO_ENABLED=0` default with opt-in exceptions | Flexible | Exception creep; breaks cross-compile guarantee |
| `CGO_ENABLED=1` allowed | No library restrictions | Requires C toolchain per target; breaks Windows cross-compile; musl/glibc coupling |

## Rationale

The cost of rejecting CGo libraries is manageable because pure-Go alternatives exist for all Phase 0 requirements (see ADR-0001 through ADR-0008 and design §16). The operational benefit — a single `go build` command producing a portable static binary for any target — outweighs the library restriction. This constraint is enforced at CI via `CGO_ENABLED=0 go build ./...` in the build matrix.

## Consequences

- **Accepted**: Stricter library selection. Every new dependency must be verified pure-Go before merging.
- **Monitor**: CI build matrix (`backend-build` job) runs `CGO_ENABLED=0 go build` on all four platforms — a failing build is the early-warning signal.
- **Revisit**: If a critical industrial protocol library (e.g., future CIP hardware acceleration) has no pure-Go implementation, open a new ADR documenting the exception and the cross-compilation plan.

## References

- Spec: MVP-FND-9.4 (CGO_ENABLED=0 in Dockerfile)
- Design: §16 (pure-Go dependency verification table), decision #1–22
- All other ADRs in this directory explicitly verify the `CGO_ENABLED=0` constraint for their library.
- Go cross-compilation docs: https://pkg.go.dev/cmd/go#hdr-Compile_and_run_Go_program
