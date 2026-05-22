---
change: mvp-foundation
domain: cli
phase: spec
date: 2026-05-22
status: draft
---

# CLI Specification

## Purpose

Top-level binary structure for the `lgb` gateway. Defines all subcommands, global flags, exit codes, output contract, build metadata injection, HTTP server stub, embedded frontend assets, CI workflow extensions, and the `make generate` placeholder.

## Requirements

### [MVP-FND-1.1] Root command global flags

The root `lgb` command MUST accept the following persistent flags available to all subcommands:

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--config` | string | `lgb.yaml` in working directory | Path to YAML config file |
| `--data-dir` | string | platform default | Override data directory |
| `--log-level` | string | `info` | One of `debug\|info\|warn\|error` |
| `--log-format` | string | `text` | One of `text\|json` |
| `--json` | bool | false | Emit machine-readable JSON output (applies to `doctor`, `status`, `version`) |

The root command MUST accept `--help` / `-h` and exit 0.

#### Scenario: Help flag exits successfully

- GIVEN the `lgb` binary is installed
- WHEN the user runs `lgb --help`
- THEN the process exits with code 0
- AND stdout contains the command description and list of subcommands

#### Scenario: Unknown flag returns error

- GIVEN the `lgb` binary is installed
- WHEN the user runs `lgb --unknown-flag`
- THEN the process exits with code 1
- AND stderr contains a message indicating the unknown flag

---

### [MVP-FND-1.2] `lgb version` subcommand

The `version` subcommand MUST print version, commit hash, and build date injected at build time via `-ldflags`. When `--json` is passed, output MUST be a JSON object `{"version":"…","commit":"…","date":"…"}`. Default (plain) output MUST be human-readable single-line. Exit code MUST be 0 in all cases.

When no ldflags are injected (development build), the fields MUST fall back to `"dev"`, `"none"`, and `"unknown"` respectively.

#### Scenario: Version output plain

- GIVEN a binary built with ldflags `-X main.version=1.0.0 -X main.commit=abc123 -X main.date=2026-05-22`
- WHEN the user runs `lgb version`
- THEN stdout contains `1.0.0`, `abc123`, and `2026-05-22`
- AND exit code is 0

#### Scenario: Version output JSON

- GIVEN the same built binary
- WHEN the user runs `lgb version --json`
- THEN stdout is valid JSON with keys `version`, `commit`, `date`
- AND exit code is 0

#### Scenario: Development build fallback

- GIVEN a binary built without ldflags
- WHEN the user runs `lgb version`
- THEN stdout contains the literal string `dev`
- AND exit code is 0

---

### [MVP-FND-1.3] `lgb server` subcommand

The `server` subcommand MUST start the HTTP server stub. It MUST read the config file specified by `--config`. It MUST bind to the address from `server.httpAddr` in config. It MUST expose `/health` (returns `{"status":"ok"}`) and reserve `/metrics` (returns 200 with empty Prometheus text exposition). It MUST log the listening address at INFO level on startup. It MUST support graceful shutdown on SIGTERM and SIGINT with a configurable drain deadline (default 10 s). Exit code MUST be 0 on clean shutdown; 1 on startup error.

The `server` subcommand MUST NOT start if `auth.jwtSecret` resolves to an empty string after config + env overlay.

#### Scenario: Server starts successfully

- GIVEN a valid config file with `server.httpAddr: ":8080"` and `LGB_AUTH_JWT_SECRET` set in env
- WHEN the user runs `lgb server --config lgb.yaml`
- THEN the process binds port 8080
- AND `GET /health` returns HTTP 200 `{"status":"ok"}`
- AND `GET /metrics` returns HTTP 200

#### Scenario: Server refuses to start without jwtSecret

- GIVEN a config file with `auth.jwtSecret: ""` and no `LGB_AUTH_JWT_SECRET` env var
- WHEN the user runs `lgb server --config lgb.yaml`
- THEN the process exits with code 1
- AND stderr contains `auth.jwtSecret is required`

#### Scenario: Graceful shutdown on SIGTERM

- GIVEN the server is running
- WHEN the process receives SIGTERM
- THEN in-flight requests are drained within the deadline
- AND the process exits with code 0

---

### [MVP-FND-1.4] `lgb doctor` subcommand

The `doctor` subcommand is specified fully in the `doctor` domain spec. The CLI contract here: it MUST accept `--json` and exit with the code determined by the worst check result (0 = all pass, 1 = at least one FAIL, 2 is reserved for internal/unexpected errors). See §MVP-FND-7.x for check details.

#### Scenario: Doctor exits 0 when all checks pass

- GIVEN all doctor checks pass
- WHEN the user runs `lgb doctor`
- THEN exit code is 0

#### Scenario: Doctor exits 1 when a check fails

- GIVEN at least one doctor check fails
- WHEN the user runs `lgb doctor`
- THEN exit code is 1

---

### [MVP-FND-1.5] `lgb status` subcommand

The `status` subcommand MUST print a health snapshot as JSON by default (it is always a machine-readable command). For Phase 0 the snapshot MUST contain `{"status":"ok","phase":"0","uptime_seconds":0}` or equivalent stub. Exit code MUST be 0.

#### Scenario: Status prints stub JSON

- GIVEN the binary is built
- WHEN the user runs `lgb status`
- THEN stdout is valid JSON containing at minimum `{"status":"ok"}`
- AND exit code is 0

---

### [MVP-FND-1.6] `lgb config validate` subcommand

The `config validate` subcommand MUST validate the YAML config file at the path given by `--config`. On success it MUST print `config OK` (or `{"valid":true}` with `--json`) and exit 0. On failure it MUST list every validation violation (one per line, or a JSON array with `--json`) and exit 1. It MUST NOT start the server or any goroutine.

#### Scenario: Valid config exits 0

- GIVEN a well-formed config file `testdata/sample.yaml`
- WHEN the user runs `lgb config validate --config testdata/sample.yaml`
- THEN stdout contains `config OK`
- AND exit code is 0

#### Scenario: Invalid config exits 1 with all violations listed

- GIVEN a config file with two invalid fields
- WHEN the user runs `lgb config validate --config bad.yaml`
- THEN exit code is 1
- AND stderr (or stdout) lists both violations

#### Scenario: Missing config file exits 1

- GIVEN no config file exists at the specified path
- WHEN the user runs `lgb config validate --config /nonexistent.yaml`
- THEN exit code is 1
- AND the message references the missing file path

---

### [MVP-FND-1.7] Build metadata via ldflags

The `internal/version` package MUST expose a `Info` struct (or equivalent exported variables) populated from ldflags at build time. The Makefile build target MUST inject `-X github.com/fgjcarlos/lgb/internal/version.Version=…` and equivalent for `Commit` and `Date`. The package MUST provide safe fallback values when not injected.

#### Scenario: Version package returns fallback in tests

- GIVEN no ldflags are injected during `go test`
- WHEN code calls `version.Version`
- THEN the returned value is `"dev"` (not empty, not a panic)

---

### [MVP-FND-1.8] HTTP server stub — `/metrics` route reservation

The HTTP server MUST register a `/metrics` route returning HTTP 200 with an empty Prometheus text exposition format body. The actual Prometheus registry is empty for Phase 0. The route MUST exist so future phases can register collectors without changing the routing layer.

#### Scenario: Metrics endpoint returns 200

- GIVEN the server is running
- WHEN an HTTP GET is made to `/metrics`
- THEN the response status is 200
- AND the Content-Type is `text/plain; version=0.0.4; charset=utf-8` (Prometheus text format)

---

### [MVP-FND-1.9] HTTP server — graceful shutdown contract

The server MUST call `http.Server.Shutdown(ctx)` on SIGTERM or SIGINT with a context deadline of `shutdownTimeout` (default 10 s, configurable). It MUST NOT call `os.Exit` directly — it MUST allow the calling goroutine to return cleanly. It MUST log a `"shutdown complete"` message at INFO level after the drain.

#### Scenario: Shutdown completes within deadline

- GIVEN a server with one long-running request (5 s)
- WHEN SIGTERM is sent and the drain deadline is 15 s
- THEN the server waits for the request to complete
- AND exits with code 0

#### Scenario: Shutdown times out and forces close

- GIVEN a server with one long-running request (20 s)
- WHEN SIGTERM is sent and the drain deadline is 5 s
- THEN the server closes after 5 s
- AND exits with code 0 (timeout is not a failure at the process level)

---

### [MVP-FND-1.10] `embed.go` — frontend dist embedding

The root-level `embed.go` MUST contain a `//go:embed all:frontend/dist` directive. The build MUST fail with a clear error if `frontend/dist` does not exist, ensuring users build the frontend before the backend. In Phase 0, the directive MAY be wrapped in a build tag or guarded so that `go build ./...` without the frontend build does not fail CI — however, the embed directive MUST be present and active (not commented out) before the change is archived.

#### Scenario: Build fails without frontend/dist

- GIVEN `frontend/dist` does not exist
- WHEN `go build ./...` is run
- THEN the build fails with an error referencing `frontend/dist`

#### Scenario: Build succeeds with frontend/dist present

- GIVEN `npm run build` has been run and `frontend/dist` exists
- WHEN `go build ./...` is run
- THEN the build succeeds

---

### [MVP-FND-1.11] CI workflow — golangci-lint step

The CI workflow MUST add a `golangci-lint` step using `golangci/golangci-lint-action@v6` pinned by SHA, gated by the existing `has_go == true` condition. The step MUST use the `.golangci.yml` configuration at the repository root. The step MUST run after the `go vet` step. It MUST NOT run when no Go source files are present.

#### Scenario: Lint step runs on Go source

- GIVEN Go source files exist and `has_go` is `true`
- WHEN CI runs
- THEN the golangci-lint step executes
- AND a lint failure causes the CI job to fail

#### Scenario: Lint step is skipped without Go source

- GIVEN no Go source files are present and `has_go` is `false`
- WHEN CI runs
- THEN the golangci-lint step is skipped without error

---

### [MVP-FND-1.12] CI workflow — frontend build step

The CI workflow MUST add a conditional step that runs `npm ci && npm run build` inside the `frontend/` directory, gated by a `has_frontend == true` condition (determined by the presence of `frontend/package.json`). The step MUST fail CI if `npm run build` exits non-zero.

#### Scenario: Frontend build runs when package.json exists

- GIVEN `frontend/package.json` is present and `has_frontend` is `true`
- WHEN CI runs
- THEN `npm ci && npm run build` executes in `frontend/`
- AND a build failure causes the CI job to fail

#### Scenario: Frontend step is skipped without package.json

- GIVEN `frontend/package.json` does not exist
- WHEN CI runs
- THEN the frontend build step is skipped

---

### [MVP-FND-1.13] `make generate` placeholder target

The Makefile MUST include a `generate` target. When no `.proto` files exist, the target MUST exit 0 and print a notice such as `# no .proto files — skipping protobuf codegen`. The target MUST NOT fail the build. It serves as a placeholder for future Sparkplug protobuf code generation.

#### Scenario: Generate target with no proto files

- GIVEN no `.proto` files exist in the repository
- WHEN `make generate` is run
- THEN exit code is 0
- AND stdout contains a notice about no proto files

---

### [MVP-FND-1.14] ADR template and index

The file `docs/adr/0000-template.md` MUST exist and follow the canonical ADR template (Status / Decision / Context / Options Considered / Rationale / Consequences / References). Nine numbered ADRs (`docs/adr/0001-cli-framework.md` through `docs/adr/0009-pure-go-no-cgo.md`) MUST exist with status `Proposed` at end of this change (promoted to `Accepted` at archive).

| ADR | Title |
|-----|-------|
| 0001 | CLI framework — Cobra v1.10.2 |
| 0002 | Config loader — koanf v2 with `LGB_{SECTION}_{FIELD}` env overlay |
| 0003 | Logging — `log/slog` stdlib |
| 0004 | PLC driver — `danomagnum/gologix` v0.41.0-beta |
| 0005 | OPC UA library — `gopcua/opcua` v0.8.0 with mandatory Phase 1 spike |
| 0006 | MQTT + Sparkplug B — `eclipse/paho.mqtt.golang` v1.5.1 |
| 0007 | Historian — `modernc.org/sqlite` v1.50.1 |
| 0008 | Backups — `restic` v0.18.0 subprocess |
| 0009 | Pure-Go / no CGo — all direct dependencies |

#### Scenario: All ADR files exist with correct status

- GIVEN the change is applied
- WHEN the `docs/adr/` directory is listed
- THEN files `0000-template.md` and `0001` through `0009` are present
- AND each contains the field `**Status**: Proposed`
