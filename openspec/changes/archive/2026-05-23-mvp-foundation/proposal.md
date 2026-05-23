---
change: mvp-foundation
phase: propose
date: 2026-05-22
status: draft
roadmap_phase: 0
---

# Proposal: MVP Foundation — Phase 0 scaffolding

## Intent

Establish the entire technical foundation for LGB before any product-domain code is written: a Cobra-based CLI, the `internal/` package skeleton, a koanf-driven configuration system with environment-variable secret overlay, a structured logger, a project-wide error model, a retry primitive, a runnable Docker development stack with an in-process CIP PLC simulator, the linter / release / CI tooling, and nine ADRs that lock in every major library choice. This change unblocks every subsequent Phase 1 milestone (`plc-driver`, `opc-ua-server`, `sparkplug-edge-node`, `historian`, `backup-module`, `auth-and-users`, `frontend-mvp`) by giving them a stable, conventionally-shaped place to land code under strict TDD.

## Scope

### In Scope

- **CLI binary** (`cmd/lgb`) built on Cobra (`github.com/spf13/cobra` v1.10.2) with subcommands: `server`, `doctor`, `status`, `config validate`, `version`. Build metadata injected via `-ldflags`.
- **Package skeleton** under `internal/` — placeholder packages with clearly documented responsibilities. No business logic.
- **Configuration system**: koanf v2 (`github.com/knadh/koanf/v2`) loading YAML + environment overlay using the `LGB_{SECTION}_{FIELD}` naming convention, full schema validation, and hot-reload via `file.Provider.Watch()`. Key case preservation (camelCase fields like `scanRateMs`) is mandatory.
- **Secret management convention**: environment variables overlaying YAML via koanf. Documented secret fields: `auth.jwtSecret` ↔ `LGB_AUTH_JWT_SECRET`, `mqtt.password` ↔ `LGB_MQTT_PASSWORD`, future PLC credentials follow the same pattern. No separate `secrets.yaml` file.
- **Structured logger** built on `log/slog` (stdlib) with configurable level (`debug|info|warn|error`) and format (`text|json`).
- **Error model**: per-package sentinel errors, wrapping with `%w`, `errors.Is`/`errors.As` at boundaries, no panics in library code, typed errors at API boundaries.
- **Retry primitive** in `internal/retry/` — exponential backoff with jitter, context cancellation, configurable max attempts. Not wired into anything yet.
- **dataDir + first-run bootstrap**: configurable via `--data-dir` / `LGB_GATEWAY_DATADIR`. Platform-conventional defaults: `/var/lib/lgb` (Docker, Linux), `~/Library/Application Support/lgb` (macOS), `%PROGRAMDATA%\lgb` (Windows). Bootstrap validates path, creates with `0700` if missing, fails loudly on permission errors.
- **`lgb doctor`** initial checks: data dir writable, `restic` binary on `$PATH`, Go runtime version, HTTP port availability.
- **`/metrics` route reserved** in the HTTP server stub with an empty Prometheus registry — instrumentation deferred to Phase 2.
- **Docker development stack** (`docker-compose.dev.yml`): gateway container + `lgb-plcsim` container running a small `cmd/plcsim` binary that uses gologix's built-in `Server` + `MapTagProvider` to expose deterministic CIP responses on `:44818`. A mosquitto broker is added for later phases but not consumed yet.
- **ADRs** (9 total) in `docs/adr/` covering every locked-in library choice — see Approach §ADR plan.
- **`.golangci.yml`** — strict baseline (`errcheck`, `staticcheck`, `gosimple`, `unused`, `govet`, `ineffassign`).
- **`.goreleaser.yaml`** skeleton — not yet wired into CI.
- **`make generate`** target — placeholder for future protobuf code generation (no `.proto` files yet).
- **Frontend scaffold** (`frontend/`): Vite + React + TypeScript + `.nvmrc`. Root-level `embed.go` stub for future static asset embedding. No UI implementation.
- **CI updates** (`.github/workflows/ci.yml`): add a `golangci-lint` step inside the existing `has_go == true` guard; add a conditional frontend `npm ci && npm run build` step inside a `has_frontend == true` guard.
- **Initial test suite under strict TDD**: smoke tests for every CLI subcommand, config loader unit tests, logger tests, error model tests, doctor check tests, retry primitive tests.

### Out of Scope (deferred to named subsequent SDD changes)

| Concern | Deferred to |
|---------|-------------|
| Real PLC CIP communication, tag store, per-PLC goroutines, hot-reload | `plc-driver` |
| OPC UA server with namespace per PLC + Phase 1 spike on security modes | `opc-ua-server` |
| Sparkplug B Edge Node + Device lifecycle (NBIRTH/NDEATH/DBIRTH/DDEATH), DCMD/NCMD handling | `sparkplug-edge-node` |
| Historian schema, ingestion pipeline, retention enforcement | `historian` |
| Backup module beyond the doctor check (restic invocations, repos, schedules, restore) | `backup-module` |
| First-run wizard, JWT issuance, user CRUD, RBAC, audit trail | `auth-and-users` |
| REST API surface beyond `/metrics` reservation and server bootstrap stub | `rest-api` |
| WebSocket channel for the UI | `realtime-api` |
| Frontend UI (login, dashboard, tag mapping, diagnostics, backups, users) | `frontend-mvp` |
| Prometheus instrumentation populating the reserved `/metrics` route | `observability` |
| GoReleaser pipeline wiring into release CI + cosign signing | `release-pipeline` |

## Capabilities

> Phase 0 introduces foundational capabilities only. No user-facing product capabilities (those land in later changes). The capability names below become `openspec/specs/<name>/spec.md` files via sdd-spec.

### New Capabilities

- `cli`: top-level binary structure, subcommands, global flags, help/version output, exit codes.
- `config`: YAML schema, koanf loader, env overlay (`LGB_{SECTION}_{FIELD}`), validation, hot-reload via `file.Provider.Watch()`.
- `logging`: structured `slog` setup, level/format selection, handler choice (text/JSON), output target.
- `errors`: project-wide error sentinels, wrapping convention, `errors.Is`/`As` discipline, panic policy.
- `retry`: exponential backoff primitive with jitter, max-attempts, context cancellation.
- `data-dir`: cross-platform default resolution, bootstrap order, permission discipline (`0700`).
- `doctor`: diagnostic checks contract (data dir writable, `restic` on PATH, Go runtime, port availability).
- `dev-stack`: Docker Compose dev environment + `cmd/plcsim` deterministic CIP simulator contract.

### Modified Capabilities

None — this is the first change. There are no existing specs to modify.

## Approach

### Package layout

```
lgb/
├── cmd/
│   ├── lgb/
│   │   ├── main.go              # ldflags-injected build metadata; cobra root init
│   │   └── cmd/
│   │       ├── root.go          # root command, global flags (--config, --data-dir, --log-format, --log-level)
│   │       ├── server.go        # 'server' subcommand — starts HTTP server stub with /metrics reserved
│   │       ├── doctor.go        # 'doctor' subcommand — runs diagnostic checks
│   │       ├── status.go        # 'status' subcommand — prints JSON health snapshot
│   │       ├── config.go        # 'config' command group
│   │       ├── config_validate.go # 'config validate' subcommand
│   │       └── version.go       # 'version' subcommand
│   └── plcsim/
│       └── main.go              # gologix Server + MapTagProvider on :44818
├── internal/
│   ├── config/
│   │   ├── config.go            # Config struct, schema, defaults
│   │   ├── loader.go            # koanf-based loader, env overlay, validation
│   │   ├── watcher.go           # hot-reload via koanf file Watch()
│   │   └── config_test.go
│   ├── log/
│   │   ├── log.go               # slog setup: JSON/text handler, level from config
│   │   └── log_test.go
│   ├── errors/
│   │   ├── errors.go            # sentinel errors (ErrConfigInvalid, ErrNotFound, ...)
│   │   └── errors_test.go
│   ├── retry/
│   │   ├── retry.go             # exponential backoff with jitter + context cancellation
│   │   └── retry_test.go
│   ├── datadir/
│   │   ├── datadir.go           # cross-platform default resolution + bootstrap
│   │   └── datadir_test.go
│   ├── doctor/
│   │   ├── doctor.go            # check registry, Result type
│   │   └── doctor_test.go
│   ├── version/
│   │   └── version.go           # build metadata from ldflags
│   ├── server/
│   │   └── server.go            # HTTP server stub, router with /metrics reserved
│   ├── health/
│   │   └── handler.go           # GET /health → {"status":"ok"}
│   └── testutil/
│       ├── plcsim.go            # starts/stops gologix Server in-process for tests
│       └── config.go            # test config builders
├── docs/
│   └── adr/                     # 9 ADRs (see ADR plan below)
├── docker/
│   ├── Dockerfile               # multi-stage: build → final (restic binary copied in)
│   └── Dockerfile.dev           # dev image with Go toolchain
├── frontend/                    # Vite + React + TS scaffold; no UI
│   ├── package.json
│   ├── vite.config.ts
│   ├── tsconfig.json
│   ├── .nvmrc
│   └── src/main.tsx             # empty placeholder
├── embed.go                     # root-level static embed stub (no assets yet)
├── docker-compose.dev.yml       # gateway + plcsim + mosquitto
├── .goreleaser.yaml             # skeleton, not wired to CI
├── .golangci.yml                # strict baseline
└── go.mod                       # all direct deps pinned
```

### Library decisions

All choices below are pure-Go and compile under `CGO_ENABLED=0` on linux/amd64, linux/arm64, darwin/arm64, windows/amd64.

| Library | Version | Rationale | Risk / Mitigation |
|---------|---------|-----------|--------------------|
| `github.com/spf13/cobra` | v1.10.2 | Nested subcommands, shell completion, used by k8s/Hugo/gh — required by `config validate` nesting | Boilerplate accepted; pinned major version |
| `github.com/knadh/koanf/v2` | v2.3.4 | Preserves camelCase keys (viper lowercases all keys), modular, env-overlay provider, `Watch()` hot-reload | Pin v2 line; wrap behind `internal/config` so library never leaks |
| `log/slog` (stdlib) | Go 1.24 | Stdlib structured logger — no external dep, JSON+text handlers built in | None |
| `github.com/danomagnum/gologix` | v0.41.0-beta | Validated CIP driver with `MapTagProvider` CIP server for the simulator | Beta API surface — pin exact version, wrap behind `internal/plc` interface in the `plc-driver` change |
| `github.com/gopcua/opcua` | v0.8.0 | Mature client, substantially complete server | Server-side security modes need a Phase 1 spike — captured in ADR-0005 and `opc-ua-server` change |
| `github.com/eclipse/paho.mqtt.golang` | v1.5.1 | MQTT 3.1.1 + LWT (NDEATH); Sparkplug B 3.0 is spec-compliant on 3.1.1 | `SetOrderMatters(false)` documented as mandatory; enforced in `sparkplug-edge-node` change |
| `modernc.org/sqlite` | v1.50.1 | Pure-Go SQLite (~75% of CGo mattn throughput); WAL + `VACUUM INTO` supported | Single writer constraint — historian goroutine pattern in `historian` change |
| `restic` binary | v0.18.0 | `--json` stable, exit codes in JSON output; invoked via `os/exec` subprocess | Bundled via multi-stage Docker (`FROM restic/restic AS restic-bin`); `lgb doctor` checks PATH presence |
| `github.com/golang-jwt/jwt/v5` | v5.3.1 | Pure-Go JWT, HS256 sufficient for MVP | Signing key stored as env-var secret (`LGB_AUTH_JWT_SECRET`), never in YAML |

### Configuration schema (canonical)

The full YAML schema is lifted verbatim from `exploration.md` § Configuration Schema. Repeated here only for the env-overlay rule.

**Secret-loading convention**: every secret field is overridable via an environment variable using the pattern `LGB_{SECTION_UPPER}_{FIELD_UPPER}`. The env value wins over the YAML value at koanf merge time.

Examples:

| YAML path | Env var |
|-----------|---------|
| `auth.jwtSecret` | `LGB_AUTH_JWT_SECRET` |
| `mqtt.password` | `LGB_MQTT_PASSWORD` |
| `mqtt.passwordFile` | `LGB_MQTT_PASSWORDFILE` |
| `backup.repos[0].password` | `LGB_BACKUP_REPOS_0_PASSWORD` |
| `gateway.dataDir` | `LGB_GATEWAY_DATADIR` |

The convention is documented in `internal/config/loader.go` doc comment AND in `docs/adr/ADR-0002-config.md`. Validation MUST reject startup if any secret field resolves to empty when the surrounding capability is enabled (e.g. `auth.jwtSecret == ""` blocks `lgb server` start).

### Error model

- **Sentinel errors per package**: e.g. `internal/config` exports `ErrConfigInvalid`, `ErrConfigMissing`, `ErrConfigPermission`. Callers compare via `errors.Is`.
- **Wrapping**: every non-trivial error is wrapped with `fmt.Errorf("...: %w", err)` to preserve the chain.
- **Boundary typing**: HTTP handlers and CLI commands translate internal errors into structured exit codes / HTTP status, never leaking raw library errors.
- **No panics in library code**: panics are reserved for programmer errors (`internal/...` packages return errors). `main.go` may convert a top-level error to `os.Exit(1)` after logging.
- **`errors.Join`** is used where multiple validation failures must be reported at once (e.g. config validation aggregates all violations).

### Retry primitive

```go
package retry

// Do runs fn with exponential backoff + jitter until it returns nil,
// the context is cancelled, or maxAttempts is reached. Initial delay
// is opts.Initial; each retry doubles the delay up to opts.Max with
// ±25% jitter applied to each interval.
type Options struct {
    Initial     time.Duration // default 100ms
    Max         time.Duration // default 30s
    MaxAttempts int           // 0 = unlimited (until ctx cancels)
    Jitter      float64       // default 0.25
}

func Do(ctx context.Context, opts Options, fn func(ctx context.Context) error) error
```

Returns `ctx.Err()` on cancellation, the last `fn` error on max-attempts exhaustion, or `nil` on success. Pure-stdlib, ~50 LOC including tests.

### dataDir + first-run

**Resolution order** (highest priority wins):

1. `--data-dir` CLI flag
2. `LGB_GATEWAY_DATADIR` env var (via koanf overlay)
3. `gateway.dataDir` YAML value
4. Platform default

**Platform defaults**:

| Platform | Default |
|----------|---------|
| Docker / Linux | `/var/lib/lgb` |
| macOS | `${HOME}/Library/Application Support/lgb` |
| Windows | `%PROGRAMDATA%\lgb` |

**Bootstrap order** (in `internal/datadir.Ensure`):

1. Resolve the absolute path (expand `~`, env vars).
2. If it does not exist → create with `0700` (Linux/macOS) or default ACL (Windows).
3. If it exists but is not a directory → fail with `ErrDataDirInvalid`.
4. If it exists but is not writable by the running user → fail with `ErrDataDirPermission` (clear, actionable message).
5. Log resolved path at INFO on startup.

**`lgb doctor`** reports the resolved path, ownership/mode (POSIX), and pass/fail per check.

### CI changes

- Add a `golangci-lint` job (or step inside the build job) gated by the existing `has_go == true` guard. Use `golangci/golangci-lint-action@v6` pinned by SHA.
- Add a conditional frontend job (`needs: has_frontend`) that runs `npm ci && npm run build` when `frontend/package.json` exists.
- No GoReleaser step yet — the `.goreleaser.yaml` is committed as scaffolding but the release workflow lands in `release-pipeline`.

### ADR plan

Nine ADRs created in `docs/adr/`, all in **Proposed** status at the end of this change (promoted to **Accepted** at archive). Each follows the canonical template from `exploration.md` §ADR Template.

| ADR | Title | Decision |
|-----|-------|----------|
| ADR-0001 | CLI framework | Cobra v1.10.2 |
| ADR-0002 | Config loader | koanf v2 with env overlay (`LGB_{SECTION}_{FIELD}`) |
| ADR-0003 | Logging | `log/slog` (stdlib) |
| ADR-0004 | PLC driver | `danomagnum/gologix` v0.41.0-beta |
| ADR-0005 | OPC UA library | `gopcua/opcua` v0.8.0 with mandatory Phase 1 spike on server security modes; `awcullen/opcua` named as contingency |
| ADR-0006 | MQTT + Sparkplug B | `eclipse/paho.mqtt.golang` v1.5.1 + project-owned `internal/sparkplug` |
| ADR-0007 | Historian | `modernc.org/sqlite` v1.50.1 (pure-Go) |
| ADR-0008 | Backups | `restic` v0.18.0 invoked as subprocess with `--json` |
| ADR-0009 | Pure-Go / no CGo | All direct dependencies must compile with `CGO_ENABLED=0` on all four targets; CI enforces |

## Affected Areas

| Area | Impact | Description |
|------|--------|-------------|
| `cmd/lgb/` | New | Cobra root + subcommands |
| `cmd/plcsim/` | New | gologix CIP server simulator binary |
| `internal/config/` | New | koanf loader, env overlay, hot-reload, validation |
| `internal/log/` | New | slog setup |
| `internal/errors/` | New | Sentinel errors + wrapping policy |
| `internal/retry/` | New | Exponential backoff primitive |
| `internal/datadir/` | New | Cross-platform data dir resolution + bootstrap |
| `internal/doctor/` | New | Diagnostic check registry |
| `internal/version/` | New | Build metadata via ldflags |
| `internal/server/` | New | HTTP server stub with `/metrics` reserved |
| `internal/health/` | New | `/health` handler |
| `internal/testutil/` | New | In-process plcsim test helpers, config builders |
| `docs/adr/` | New | 9 ADRs |
| `docker/`, `docker-compose.dev.yml` | New | Dev stack (gateway + plcsim + mosquitto) |
| `frontend/` | New | Vite + React + TS scaffold; no UI |
| `embed.go` | New | Static embed stub at module root |
| `.golangci.yml` | New | Strict baseline lint config |
| `.goreleaser.yaml` | New | Release skeleton (not wired to CI) |
| `Makefile` | Modified | Adds `lint`, `generate`, `docker-up`, `docker-down`, cross-compile targets |
| `.github/workflows/ci.yml` | Modified | Conditional golangci-lint + frontend build steps |
| `go.mod`, `go.sum` | Modified | All direct deps pinned |

## Acceptance Criteria

- [ ] `go build ./...` succeeds with `CGO_ENABLED=0` on linux/amd64, linux/arm64, darwin/arm64, windows/amd64 (CI verifies all four).
- [ ] `make test` (i.e. `go test ./... -race -count=1`) passes.
- [ ] `go vet ./...` is clean.
- [ ] `golangci-lint run` is clean with the strict baseline.
- [ ] `lgb version` prints version, commit hash, and build date populated via ldflags.
- [ ] `lgb config validate --config testdata/sample.yaml` exits 0 on a valid config; exits non-zero with a clear diagnostic listing every violation on an invalid one.
- [ ] `lgb doctor` returns OK on a clean Docker dev stack; reports `restic: not found on PATH` with non-zero exit when `restic` is removed.
- [ ] `lgb server --config testdata/sample.yaml` starts, binds the configured HTTP address, exposes `/metrics` (empty registry) and `/health`, and hot-reloads when the config file is edited (verified by a smoke test that watches a log message or status endpoint).
- [ ] `docker compose -f docker-compose.dev.yml up` brings both `gateway` and `plcsim` containers up; the gateway logs report that plcsim is reachable on `:44818` (TCP probe in a smoke test is acceptable for Phase 0 — full CIP dial happens in `plc-driver`).
- [ ] All 9 ADRs exist under `docs/adr/ADR-000N-*.md` and are in `Proposed` status at end of change (promoted to `Accepted` at archive).
- [ ] Every public function in `internal/` and every CLI command has at least one unit or smoke test (strict TDD — RED first, then GREEN).
- [ ] Frontend scaffold builds cleanly: `cd frontend && npm ci && npm run build` exits 0.
- [ ] Secret env-var override works end-to-end: a YAML with `auth.jwtSecret: ""` plus `LGB_AUTH_JWT_SECRET=...` in the environment passes validation; the same YAML without the env var fails validation with `auth.jwtSecret is required`.
- [ ] Hot-reload watcher debounces multiple writes and emits a single reload event (verified by unit test on `internal/config/watcher.go`).

## Risks

| Risk | Likelihood | Phase 0 Mitigation | Carry-over |
|------|------------|--------------------|------------|
| gopcua server security modes unverified | High | Documented in ADR-0005 with explicit Phase 1 spike requirement; `awcullen/opcua` named as contingency | `opc-ua-server` change opens with the spike task |
| gologix beta API may break | Medium | Pin exact version in `go.mod`; wrap behind `PLCDriver` interface | `plc-driver` change owns the interface implementation |
| paho deadlock with default `SetOrderMatters(true)` | Medium | Documented in ADR-0006; project convention recorded; not enforced yet (no MQTT code) | `sparkplug-edge-node` change enforces `SetOrderMatters(false)` in handler setup |
| Sparkplug NDEATH/NBIRTH not replayed by paho auto-reconnect | Medium | Documented in ADR-0006 as known caveat | `sparkplug-edge-node` change implements explicit reconnect hook |
| restic binary missing from final Docker image | Medium | Multi-stage Dockerfile copies `restic` from `restic/restic` upstream image; `lgb doctor` flags absence with clear remediation message | `backup-module` change relies on the doctor signal |
| SQLite concurrent writer contention | Low (Phase 0) | Architecture documented in ADR-0007; no historian code in Phase 0 | `historian` change implements buffered-channel + batched-insert pattern |

## Rollback Plan

Phase 0 is additive only — no existing functionality changes. Rollback is per-PR (chained-PR strategy below). Each slice is self-mergeable; reverting it cleanly removes the introduced files. The only mutation to pre-existing files is `Makefile` (additive targets) and `.github/workflows/ci.yml` (additive conditional steps) — both are reversed by a standard `git revert`. `go.mod` / `go.sum` changes follow the same pattern. No data migration, no destructive deltas.

## Dependencies

- Network access during `go mod tidy` and `npm ci` (CI only).
- `protoc` is NOT required at build time (no protobuf in Phase 0).
- Docker Engine 24+ for the dev stack.
- Node 20+ for the frontend scaffold (`.nvmrc` pins the version).

## Delivery Strategy

Honest estimate of changed lines (additions, including generated config files and ADRs, but excluding `go.sum` autoset):

| Slice | Approx. lines | Content |
|-------|---------------|---------|
| Skeleton | ~450 | `cmd/lgb/main.go`, root cobra wiring, `internal/{config,log,errors,retry,datadir,version}/` + their unit tests, `go.mod` updates |
| CLI subcommands | ~400 | `server`, `doctor`, `status`, `config validate`, `version` subcommands + `internal/{server,health,doctor}/` + smoke tests |
| Dev stack | ~300 | `cmd/plcsim/main.go`, `docker-compose.dev.yml`, `docker/Dockerfile`, `docker/Dockerfile.dev`, frontend scaffold (`package.json`, `vite.config.ts`, `tsconfig.json`, `src/main.tsx`, `.nvmrc`), root `embed.go` |
| Tooling + ADRs | ~550 | `.golangci.yml`, `.goreleaser.yaml`, `Makefile` updates, `.github/workflows/ci.yml` updates, 9 ADR files |
| **Total** | **~1700** | Far above the 400-line single-PR threshold |

**Recommendation: chained PRs.** Single-PR is not acceptable for ~1700 changed lines.

Proposed chain (each slice compiles cleanly, tests pass standalone, and is mergeable on its own):

1. **`chore/mvp-foundation-skeleton`** → `internal/` package skeleton + config + logger + errors + retry + datadir + version, with unit tests. CI: green. Does NOT yet ship a usable CLI.
2. **`chore/mvp-foundation-cli`** (rebased onto #1) → Cobra root + all subcommands + `internal/server`, `internal/health`, `internal/doctor` + smoke tests. CI: green. The binary builds and `lgb version`, `lgb doctor`, `lgb config validate`, `lgb status` all work.
3. **`chore/mvp-foundation-dev-stack`** (rebased onto #2) → `cmd/plcsim`, `docker/`, `docker-compose.dev.yml`, frontend scaffold, root `embed.go`. CI: green. `docker compose up` brings the stack.
4. **`chore/mvp-foundation-tooling`** (rebased onto #3) → `.golangci.yml`, `.goreleaser.yaml`, Makefile + CI updates, all 9 ADRs. CI: green and the lint step runs.

Follows the project's chained-PR convention: PR #1 targets `main` (or a `feature/mvp-foundation` tracker branch if the repo prefers stacks), PRs #2–#4 target their immediate predecessor; rebase on each merge to keep diffs clean.

**Decision needed before apply**: yes — confirm the chain above before `sdd-tasks` and `sdd-apply` start work. If the maintainer prefers a different cut (e.g., merge tooling earlier so lint gates from the first PR), `sdd-tasks` must re-batch accordingly.

## Out-of-Scope (named carry-over)

The following concerns are explicitly deferred. Each becomes the input to a future `/sdd-new <change-name>` invocation.

- `plc-driver` — Real CIP communication, tag store, per-PLC goroutines, hot-reload of PLC connections.
- `opc-ua-server` — OPC UA server with namespace per PLC; **opens with the mandatory Phase 1 spike on gopcua server security modes** (`None | Sign | SignAndEncrypt` + username/password) before any production implementation.
- `sparkplug-edge-node` — Sparkplug B Edge Node + Device lifecycle, NBIRTH/NDEATH/DBIRTH/DDEATH state machine, DCMD/NCMD handling, `internal/sparkplug` protobuf-generated code from the canonical `.proto`.
- `historian` — SQLite schema, historian goroutine + buffered channel, batched-insert ingestion, retention enforcement, `VACUUM INTO` integration with backups.
- `backup-module` — restic invocations, multi-repo configuration, scheduled and manual backups, restore flow, `restic check` integrity verification.
- `auth-and-users` — First-run admin wizard, JWT issuance via golang-jwt/jwt/v5, user CRUD, role-based access (admin/operator/viewer), audit trail.
- `rest-api` — REST API surface beyond the `/metrics` and `/health` stubs.
- `realtime-api` — WebSocket channel for the UI.
- `frontend-mvp` — Login screen, dashboard, tag mapping editor, diagnostics, backups, users — all UI on top of the Phase 0 scaffold.
- `observability` — Populating the reserved `/metrics` route with Prometheus metrics; OpenTelemetry tracing if/when needed.
- `release-pipeline` — Wiring `.goreleaser.yaml` into a release workflow, multi-arch Docker manifests, cosign signing.

## Success Criteria

- [ ] All Acceptance Criteria above pass on CI for every target platform.
- [ ] A new contributor can `git clone`, `make test`, `docker compose -f docker-compose.dev.yml up`, and see the gateway log a successful TCP probe to `plcsim:44818` within 60 seconds — without reading any documentation other than the README.
- [ ] All 9 ADRs are reviewable and serve as the canonical reference for every library choice for the remainder of the MVP.
- [ ] The secret management convention (`LGB_{SECTION}_{FIELD}`) is documented in ADR-0002 and consumed by `internal/config` such that every subsequent change inherits it without further design discussion.
