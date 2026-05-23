# ADR-0008: Backups — `restic` v0.18.0 as subprocess with `--json`

**Date**: 2026-05-23
**Status**: Proposed

## Decision

Use `restic` v0.18.0 as the backup engine, invoked as a subprocess by LGB via `os/exec`. The `--json` flag is used on all restic invocations so outputs are machine-parseable. The `restic` binary is included in the production Docker image via a multi-stage build (`COPY --from=restic/restic:0.18.0`). No Go binding to restic's internals is used — restic is treated as an opaque CLI tool.

## Context

LGB must back up historian data (SQLite databases) and configuration files. The backup engine must support deduplication, encryption, and multiple backend targets (local filesystem, S3, SFTP). Implementing these capabilities from scratch in Go is not feasible for Phase 0.

## Options Considered

| Option | Pros | Cons |
|--------|------|------|
| `restic` v0.18.0 as subprocess | Mature; deduplication; encryption; S3/SFTP/local backends; `--json` output; pinned version in Docker | Subprocess model; not directly importable as a Go library |
| `borgbackup` | Deduplication + encryption | Python runtime required in final image; not pure-Go compatible |
| `rclone` | Many cloud backends | No deduplication; encryption is addon; larger binary |
| Custom backup using `go-cloud` | Pure-Go; cloud-agnostic | No deduplication; significant development effort |

## Rationale

restic is the best-in-class open-source backup tool with deduplication and encryption. The subprocess model is preferred over importing restic as a Go library because: (a) restic's public API is not stable, (b) importing it would pull its entire dependency tree into LGB's `go.sum`, and (c) the `--json` flag provides a stable, version-insensitive interface. Pinning the restic version in the multi-stage Docker build (design decision #22) ensures reproducibility.

## Consequences

- **Accepted**: restic binary adds ~25 MB to the production image. LGB is not responsible for restic security patches — the Docker build must be rebuilt when restic releases a security fix.
- **Monitor**: restic upstream security advisories. Automate Docker rebuild on restic releases.
- **Revisit**: If LGB needs to backup to a backend restic does not support, evaluate `rclone` as a supplementary sync tool (not a replacement for deduplication).

## References

- Spec: MVP-FND-9.4 (multi-stage Dockerfile with restic)
- Design: §13.2, decision #22
- Upstream: https://restic.net / https://github.com/restic/restic
- Docker image: https://hub.docker.com/r/restic/restic
