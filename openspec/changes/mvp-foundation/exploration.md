---
change: mvp-foundation
phase: explore
date: 2026-05-22
status: complete
---

# Exploration: mvp-foundation — Project Foundation

## Current State

The repository is a clean scaffold: `go.mod` (module `github.com/fgjcarlos/lgb`, Go 1.24.0), a `Makefile` with build/test/vet/run/clean targets, a CI workflow that gates on multi-platform `CGO_ENABLED=0` builds across linux/amd64, linux/arm64, darwin/arm64, windows/amd64, and an `openspec/` directory bootstrapped by sdd-init. No Go source files exist yet. The working branch `sdd/mvp-foundation` is checked out. The CI workflow already handles the "no source files yet" case gracefully by skipping vet/test with a notice.

Phase 0 deliverables (this change): CLI entry point, internal package layout, config schema and loader, structured logger, error model, dev Docker Compose stack with fake PLC simulator, ADRs for all major library choices, and initial CLI smoke tests under strict TDD.

---

## Affected Areas

- `/home/composedof2/Dev/Codex/lgb/go.mod` — will gain all direct dependencies (Cobra, koanf, paho, gologix, gopcua, modernc/sqlite, golang-jwt/jwt, etc.)
- `/home/composedof2/Dev/Codex/lgb/Makefile` — gains `lint`, `generate`, `docker-up`, `docker-down`, cross-compile targets
- `/home/composedof2/Dev/Codex/lgb/.github/workflows/ci.yml` — gains golangci-lint step once source exists
- `cmd/lgb/` — new; CLI entry point
- `internal/` — new; all domain packages
- `docs/adr/` — new; Architecture Decision Records
- `docker/` — new; Dockerfile and docker-compose.dev.yml
- `cmd/plcsim/` — new; fake CIP server binary for dev and CI
- `frontend/` — skeleton placeholder only (actual UI is Phase 1)

---

## Library Validation

### 1. gologix (`github.com/danomagnum/gologix`)

**Status: VALIDATED with notes**

Last release: v0.41.0-beta (May 22, 2026). 471+ commits, actively maintained. Only 1 open issue.

Multi-Service Packet batching: **YES** — uses CIP service 0x4E (AddTags) which batches mixed-type tag reads into a single CIP packet. The `ReadMulti`, `ReadMap`, `ReadList`, and `DataTableBuffer` APIs all leverage this internally.

UDT / struct reads: **YES** — supported via Go struct mapping and `ListAllTags` enumeration.

PLC simulator / test harness: **YES** — built-in `Server` type + `MapTagProvider` (in-memory key-value store) + `PathRouter` can stand up a pure-Go CIP server on any TCP port. This eliminates the hardware dependency from CI and local development entirely.

PCCC families (MicroLogix, SLC, PLC-5): explicitly NOT supported — aligns with MVP non-goals.

Pure Go: YES (no CGo). CGO_ENABLED=0 safe.

**Risk**: v0.41.0 is still beta. API surface may have minor breaking changes before a stable v1. **Mitigation**: pin to exact version in `go.mod` and wrap gologix types behind a `PLCDriver` interface in `internal/plc/` so the library does not leak across domain boundaries.

---

### 2. gopcua/opcua (`github.com/gopcua/opcua`)

**Status: CONCERN — server side requires a Phase 1 spike before full implementation**

Last release: v0.8.0 (April 2025). 1k stars, 310 forks. New maintainers confirmed continued support (via discussion #831).

Client side: mature, production-proven at multiple enterprises.

Server side: substantially built out. The `server/` package includes `session_service.go`, `subscription_service.go`, `attribute_service.go`, `namespace_map.go`, and a flexible `MapNamespace` that supports writable variables via `SetAttribute()` with write-back through an `ExternalNotification` channel. Change notification to subscribed OPC UA clients is wired in.

Security modes: configurable via `ua.MessageSecurityMode`. Anonymous auth confirmed. Username/password and `Basic256Sha256` security policy are present but marked "Untested" in available documentation. `SignAndEncrypt` completeness is unconfirmed.

`MaxNodesPerRead` defaults to 32 — must be raised in server config for 200-tag namespaces.

**Decision**: Accept gopcua for Phase 0 ADR. Mandate a dedicated spike in Phase 1 (before the OPC UA server milestone) to validate security modes `None / Sign / SignAndEncrypt` and username/password auth against a real OPC UA client (e.g., UaExpert). If gaps emerge, the backup is `github.com/awcullen/opcua` (100% pure Go, 10x faster encoding, has client+server, v1.4.0 Dec 2024, 107 stars — less community validation but architecturally solid).

---

### 3. paho.mqtt.golang (`github.com/eclipse/paho.mqtt.golang`)

**Status: VALIDATED**

Last release: v1.5.1 (September 2025). 3.1k stars, 566 forks, actively maintained.

QoS 0/1/2: YES. Persistent sessions (CleanSession=false): YES. TLS: YES. Last Will & Testament: **YES** — required for Sparkplug NDEATH, fully supported.

MQTT version: **3.1 and 3.1.1 only**. Sparkplug B 3.0.0 specification explicitly supports MQTT 3.1.1 as a fully compliant transport. The spec's requirements for 3.1.1 clients (CleanSession=true per NBIRTH cycle) are met by this library.

**Known gotcha**: deadlock risk if message handlers block with default `SetOrderMatters(true)`. Must set `SetOrderMatters(false)` in all message handler setups. Must be documented in `internal/mqtt/`.

MQTT v5 is NOT required for Sparkplug 3.0.0 compliance with MQTT 3.1.1.

---

### 4. Sparkplug B layer (project-owned `internal/sparkplug`)

**Status: VALIDATED — project-owned implementation on top of paho is correct**

Official `.proto` source: `github.com/eclipse-sparkplug/sparkplug`. Sparkplug spec v3.0.0 is ratified and stable. The canonical `sparkplug_b.proto` is in that repository. Java is the primary reference implementation; Go is absent from official tahu/eclipse codebases.

No battle-tested pure-Go Sparkplug B library with production adoption exists as of May 2026.

**Approach**: project-owned `internal/sparkplug` package (~600-800 LOC). Generate Go protobuf code from the canonical `.proto` using `google.golang.org/protobuf` (`protoc-gen-go`). The protoc tool is a build-time dependency only; generated `.pb.go` is checked into the repository to avoid requiring protoc in dev/CI. A `make generate` target regenerates when the `.proto` changes.

---

### 5. modernc.org/sqlite

**Status: VALIDATED**

Version: v1.50.1 (May 2026). Actively maintained, pure-Go SQLite port. CGO_ENABLED=0 safe.

WAL mode: YES — `PRAGMA journal_mode=WAL` supported.

`VACUUM INTO`: YES — available via standard SQLite pragma through the driver.

Write throughput: ~75% of the CGo mattn driver. For 10 PLCs × 200 tags × 1Hz = 2,000 writes/second maximum. With batch inserts per scan cycle in WAL mode, this is well within the driver's capacity (~2,400 single-row ops/sec measured March 2026; batch transactions are faster by an order of magnitude).

**Concurrent write limitation**: SQLite allows one concurrent writer even in WAL mode. Design constraint: a dedicated historian goroutine with a buffered inbound channel — PLC goroutines enqueue tagged values, historian drains in batched transactions per PLC scan cycle. This is Phase 1 architecture, not Phase 0.

---

### 6. restic (subprocess)

**Status: VALIDATED**

v0.18.0 (March 2025). `--json` flag stable across `backup`, `snapshots`, `stats`, `check` commands. Exit codes are included in JSON output as of v0.18.0+.

Subprocess integration: invoke via `os/exec`, parse `--json` stdout, check exit code. Machine-readable JSON dispatch via `message_type` field.

**`VACUUM INTO` before restic**: create a consistent point-in-time SQLite snapshot at `/tmp/lgb-backup-snapshot.db` via `VACUUM INTO`, then pass that file to restic. This avoids WAL-related consistency issues.

**Docker image**: restic must be included in the final Docker image. Use a multi-stage Dockerfile: `FROM restic/restic:latest AS restic-bin`, then `COPY --from=restic-bin /usr/bin/restic /usr/local/bin/restic` in the final stage.

---

### 7. JWT (`github.com/golang-jwt/jwt/v5`)

**Status: VALIDATED**

v5.3.1 (January 2026). Stable, pure Go. Supports HMAC SHA, RSA, ECDSA, Ed25519.

For the MVP: HS256 with a random 256-bit signing key. Key must survive restarts (stored as secret, not in config YAML).

---

### 8. CLI framework: Cobra (`github.com/spf13/cobra`)

**Status: VALIDATED — Cobra is the correct choice**

v1.10.2 (December 2025). 44k stars, pure Go, used by Kubernetes, Hugo, GitHub CLI.

| Framework | Pros | Cons | Fit |
|-----------|------|------|-----|
| **Cobra v1.10** | Nested subcommands, shell completion (bash/zsh/fish/pwsh), man pages | More boilerplate vs stdlib | **Best** |
| urfave/cli v3 | Simpler fluent API | Smaller ecosystem, manual completion | Acceptable |
| stdlib `flag` | Zero deps | No nested subcommand support | Insufficient |

The `config validate` nested subcommand and the planned operator-facing CLI experience justify Cobra.

---

### 9. Config loader: koanf v2 (`github.com/knadh/koanf/v2`)

**Status: VALIDATED — koanf over viper**

v2.3.4 (March 2026). 99.6% pure Go, modular.

| Feature | koanf v2 | viper |
|---------|----------|-------|
| Key case preservation | YES | NO — lowercases all keys |
| Hot-reload (Watch) | YES — `file.Provider.Watch()` | YES but global mutex issues |
| Env var override | YES — prefix-filtered env provider | YES |
| YAML | YES | YES |
| Binary footprint | Small — modular, import only what you use | Larger — monolithic |
| CGo | None | None |

koanf's key-case preservation is critical for a YAML config with camelCase fields like `scanRateMs` and `brokerURL`. Viper's forced lowercasing would require renaming all fields to lowercase-only or snake_case and risking subtle bugs.

---

## Proposed Package Layout

```
lgb/
├── cmd/
│   └── lgb/
│       ├── main.go              ← build metadata via ldflags, cobra root init
│       └── cmd/
│           ├── root.go          ← root command, global flags (--config, --log-format, --log-level)
│           ├── server.go        ← 'server' subcommand (stub: starts empty HTTP server)
│           ├── doctor.go        ← 'doctor' subcommand (stub: checks config, dirs, binaries)
│           ├── status.go        ← 'status' subcommand (stub: prints JSON health)
│           ├── config.go        ← 'config' command group
│           ├── config_validate.go ← 'config validate' subcommand
│           └── version.go       ← 'version' subcommand
├── cmd/
│   └── plcsim/
│       └── main.go              ← gologix Server + MapTagProvider, CIP on :44818
├── internal/
│   ├── config/
│   │   ├── config.go            ← Config struct, schema, defaults
│   │   ├── loader.go            ← koanf-based loader, env overlay, validation
│   │   ├── watcher.go           ← hot-reload via koanf file Watch()
│   │   └── config_test.go
│   ├── log/
│   │   ├── log.go               ← slog setup: JSON/text handler, level from config
│   │   └── log_test.go
│   ├── errors/
│   │   ├── errors.go            ← sentinel errors (ErrConfigInvalid, ErrNotFound, etc.)
│   │   └── errors_test.go
│   ├── version/
│   │   └── version.go           ← build metadata from ldflags
│   ├── server/
│   │   └── server.go            ← HTTP/WS server stub, router with /health and /metrics reserved
│   ├── health/
│   │   └── handler.go           ← GET /health → {"status":"ok"}
│   └── testutil/
│       ├── plcsim.go            ← starts/stops gologix Server in-process for tests
│       └── config.go            ← test config builders
├── docs/
│   └── adr/
│       ├── ADR-001-cip-driver.md
│       ├── ADR-002-opcua-server.md
│       ├── ADR-003-sparkplug-transport.md
│       ├── ADR-004-historian.md
│       ├── ADR-005-backup-engine.md
│       ├── ADR-006-cli-framework.md
│       └── ADR-007-config-loader.md
├── docker/
│   ├── Dockerfile               ← multi-stage: build → final with restic binary
│   └── Dockerfile.dev           ← dev image with Go toolchain
├── docker-compose.dev.yml       ← lgb-dev + lgb-plcsim + mqtt-broker (mosquitto)
├── .goreleaser.yaml             ← skeleton for snapshot builds
├── .golangci.yml                ← minimal linter config
└── go.mod                       ← updated with all direct dependencies
```

---

## Configuration Schema (Canonical)

```yaml
# lgb.yaml — gateway configuration
# Secrets (jwtSecret, mqtt passwords, PLC credentials) MUST be provided via
# environment variables (LGB_AUTH_JWT_SECRET, LGB_MQTT_PASSWORD) or a
# secrets file. Never store plaintext secrets in this file.

gateway:
  id: "lgb-1"             # unique identifier for this gateway instance
  logLevel: "info"        # debug | info | warn | error
  logFormat: "text"       # text | json
  dataDir: "/var/lib/lgb" # historian DB, certs, audit log location

server:
  httpAddr: ":8080"
  tlsEnabled: false
  tlsCert: ""
  tlsKey: ""

auth:
  firstRunWizardDone: false
  jwtSecret: ""           # MUST be set via LGB_AUTH_JWT_SECRET env var
  sessionTTL: "8h"

plcs: []
# - id: "line-1"
#   name: "Production Line 1"
#   host: "192.168.1.10"
#   port: 44818
#   slot: 0
#   scanRateMs: 500
#   path: "1,0"
#   tags: []             # populated via UI or config after connection

opcua:
  enabled: false
  endpoint: "opc.tcp://0.0.0.0:4840"
  securityModes: ["None"] # None | Sign | SignAndEncrypt
  certPath: ""
  keyPath: ""

mqtt:
  enabled: false
  brokerURL: "tcp://localhost:1883"
  clientID: ""            # defaults to gateway.id + random suffix for uniqueness
  username: ""
  passwordFile: ""        # path to file containing the broker password
  tls: false
  caCert: ""
  sparkplug:
    groupID: "lgb"
    edgeNodeID: ""        # defaults to gateway.id

historian:
  retentionDays: 90
  maxDBSizeMB: 2048
  vacuumSchedule: "0 3 * * *"  # cron: nightly at 03:00

backup:
  resticPath: "restic"
  repos: []
  # - name: "local"
  #   url: "/mnt/backup/lgb"
  #   passwordFile: "/run/secrets/restic-password"
  schedule: ""            # cron expression; empty = manual only
  keepHourly: 24
  keepDaily: 7
  keepWeekly: 4
  keepMonthly: 6
```

---

## ADR Template (Canonical)

```markdown
# ADR-NNN: Title

**Date**: YYYY-MM-DD
**Status**: Proposed | Accepted | Superseded | Deprecated

## Decision

[One-paragraph statement of the decision. Lead with the choice, not the reasoning.]

## Context

[What problem does this decision address? What constraints apply (pure-Go,
cross-platform, industrial reliability, etc.)? Why does it matter for LGB?]

## Options Considered

| Option | Pros | Cons |
|--------|------|------|
| A: … | … | … |
| B: … | … | … |
| C: … | … | … |

## Rationale

[Why this option over the others, in terms of the constraints listed above.]

## Consequences

[Trade-offs accepted. What must be monitored or revisited in future phases?]

## References

[Links to relevant issues, specs, external docs, or benchmark data.]
```

---

## Gaps Surfaced

### GAP-1: Secret management (HIGH)

The config schema includes `jwtSecret`, MQTT broker passwords, and future PLC credentials. These must never live in plaintext YAML in production.

**Recommendation**: define the secret-loading convention in Phase 0. koanf's env provider overlays environment variables over file-based config. Named fields are `LGB_{SECTION}_{FIELD}` (e.g., `LGB_AUTH_JWT_SECRET`, `LGB_MQTT_PASSWORD`). Document which fields are "secret fields" in the schema. For Docker Compose deployments, use Compose `secrets:` mapped to env vars. No vault integration in Phase 0.

### GAP-2: Observability story (MEDIUM)

No metrics or tracing have been specified. For Phase 0: reserve the `/metrics` HTTP route in `internal/server/`. Add Prometheus instrumentation in Phase 2. OpenTelemetry tracing is a Phase 3 consideration. The `prometheus/client_golang` module is pure Go and CGO_ENABLED=0 safe.

### GAP-3: Retry / backoff policy (MEDIUM)

Three reconnect domains with different semantics:

| Domain | Approach |
|--------|----------|
| PLC disconnect | Per-PLC goroutine: exponential backoff with jitter, max 30s. Tag store holds last-known-good values with `BadCommunicationError` quality code during outage. |
| MQTT disconnect | paho auto-reconnect enabled; Sparkplug layer replays NDEATH → NBIRTH sequence on reconnect. paho does NOT do this automatically. |
| OPC UA session | gopcua server: sessions are per-client and transient. No special handling needed server-side. |

Phase 0 should scaffold `internal/retry/` with a shared exponential backoff primitive (no external library needed — ~30 LOC).

### GAP-4: Build and release tooling (MEDIUM)

GoReleaser is planned for Phase 3 but a skeleton `.goreleaser.yaml` should be added in Phase 0 to validate the release pipeline early. Multi-arch Docker via `dockers_v2` + `docker_manifests` supports all four targets. Restic binary must be bundled in the Docker image via multi-stage build. Binary signing (cosign) is Phase 3.

### GAP-5: Sparkplug protobuf code generation (LOW)

The canonical `sparkplug_b.proto` is in `github.com/eclipse-sparkplug/sparkplug`. Generated `.pb.go` must be checked in to `internal/sparkplug/pb/` to avoid requiring protoc in dev/CI. Add a `make generate` target. Add `buf.gen.yaml` or equivalent for regeneration docs.

### GAP-6: golangci-lint configuration (LOW)

Add `.golangci.yml` in Phase 0 with at minimum: `errcheck`, `staticcheck`, `gosimple`, `unused`. Add the lint step to `ci.yml` inside the same `has_go == true` guard.

### GAP-7: Frontend placeholder (LOW)

Create `frontend/` with a `package.json` stub and Vite config placeholder in Phase 0 to establish the monorepo structure. CI skips frontend steps until Phase 1.

### GAP-8: OPC UA server security mode validation (MEDIUM)

gopcua's server security modes are present but not fully tested per documentation. A mandatory spike task must be added to Phase 1: validate `None / Sign / SignAndEncrypt` and username/password auth against a real OPC UA client (UaExpert or the Python OPC UA client) before implementing the production OPC UA server. If blocking gaps are found, evaluate `github.com/awcullen/opcua` as a drop-in replacement.

### GAP-9: First-run and data directory behavior (MEDIUM)

Define the bootstrap sequence for a fresh installation:
1. `lgb server` with no config file → clear error message with instructions
2. `dataDir` does not exist → auto-create with correct permissions (0700)
3. `jwtSecret` not set → refuse to start `server`, instruct user to run first-run wizard
4. `lgb doctor` → checks: config valid, dataDir writable, restic binary present (if backup configured), HTTP port available

This is Phase 0 scaffolding (error paths + doctor checks), Phase 1 wizard implementation.

---

## Approaches Summary

| Decision | Option A | Option B | Option C | Recommended |
|----------|----------|----------|----------|-------------|
| CLI framework | Cobra | urfave/cli v3 | stdlib flag | **Cobra** |
| Config loader | koanf v2 | viper | stdlib + yaml.v3 | **koanf v2** |
| Logger | log/slog stdlib | zerolog | zap | **slog (stdlib)** |
| Error model | stdlib + sentinels | pkg/errors | cockroachdb/errors | **stdlib** |
| PLC dev simulator | gologix Server (Go) | FactoryTalk Logix Echo | Docker + real firmware | **gologix Server** |
| Secret strategy | Env vars + koanf overlay | Separate secrets file | Vault/AWS SM | **Env vars** |

---

## Recommendation

All library choices are validated for Phase 0. The critical flag is on **gopcua's server-side security modes** — this does not block Phase 0 but must be resolved as the first task of Phase 1's OPC UA milestone. Everything else is green. The package layout proposed above establishes clean domain boundaries that will survive the full MVP build without a major restructure.

The most impactful architectural decision for Phase 0 is the secret management convention: define it now so every subsequent phase builds on it rather than retrofitting it later.

### Ready for Proposal

YES. All Phase 0 scope is well-defined. The proposal should capture: CLI structure, package layout, config schema, dev Docker Compose stack, ADR list, and the secret loading convention. The gopcua server-side concern must appear as an explicit risk with a Phase 1 spike task.

---

## Risks

1. **gopcua server security modes unverified**: Phase 1 OPC UA server may require switching to `awcullen/opcua`. Mitigation: spike before implementation milestone.
2. **gologix beta status**: API breakage before v1.0. Mitigation: domain boundary interface in `internal/plc/`.
3. **paho deadlock gotcha**: blocking message handlers cause deadlock. Mitigation: enforce `SetOrderMatters(false)` by default, document in `internal/mqtt/`.
4. **Sparkplug NDEATH/NBIRTH on reconnect**: paho's auto-reconnect does not replay Sparkplug birth sequence. This is invisible until the MQTT broker disconnects under load. Mitigation: explicit reconnect hook in `internal/sparkplug/`.
5. **restic binary bundling in Docker**: forgotten at multi-stage build time causes backup to fail silently. Mitigation: `lgb doctor` checks `restic` is reachable on `$PATH`.
6. **SQLite concurrent writers**: 10 PLC goroutines writing simultaneously will serialize; if scan rates are fast, the historian goroutine's channel may back-pressure. Mitigation: historian goroutine pattern with buffered channel and batch insert per drain cycle.
