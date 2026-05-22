---
change: mvp-foundation
domain: dev-stack
phase: spec
date: 2026-05-22
status: draft
---

# Dev-Stack Specification

## Purpose

Docker Compose development environment and in-process CIP PLC simulator contract for LGB. Defines the `docker-compose.dev.yml` service topology, the `cmd/plcsim` binary behavior, the frontend scaffold, and the `embed.go` integration.

## Requirements

### [MVP-FND-9.1] Docker Compose service topology

The `docker-compose.dev.yml` file MUST define exactly three services:

| Service | Image / Build | Port(s) | Purpose |
|---------|--------------|---------|---------|
| `gateway` | Built from `docker/Dockerfile.dev` | `8080:8080` | LGB gateway binary |
| `plcsim` | Built from `cmd/plcsim/main.go` inside `docker/Dockerfile.dev` | `44818:44818/tcp` | CIP PLC simulator |
| `mqtt` | `eclipse-mosquitto:2` (official) | `1883:1883` | MQTT broker (consumed in later phases) |

All three services MUST start successfully with `docker compose -f docker-compose.dev.yml up`. The `gateway` and `plcsim` services MUST be built from pure-Go source with `CGO_ENABLED=0`. The `mqtt` service is a third-party image and is not subject to the pure-Go constraint.

#### Scenario: All three services start without errors

- GIVEN Docker Engine 24+ is installed and the repository is checked out
- WHEN `docker compose -f docker-compose.dev.yml up --wait` is run
- THEN all three services reach a healthy or running state
- AND no container exits with a non-zero exit code

Test layer: smoke (Docker Compose in CI or manual dev)

---

### [MVP-FND-9.2] `cmd/plcsim` binary — deterministic CIP simulator

The `cmd/plcsim` binary MUST use `gologix`'s built-in `Server` type and `MapTagProvider` to expose a deterministic in-memory CIP server on TCP port `44818`. It MUST be pure-Go (`CGO_ENABLED=0`). It MUST pre-populate at least three named tags with static values so CI smoke tests can verify connectivity without configuring real PLCs.

Pre-populated tags MUST include at minimum:

| Tag name | Go type | Static value |
|----------|---------|-------------|
| `SimBool` | `bool` | `true` |
| `SimInt` | `int16` | `42` |
| `SimFloat` | `float32` | `3.14` |

The simulator MUST log `"plcsim listening"` at INFO level on successful startup.

#### Scenario: Simulator starts and accepts TCP connections

- GIVEN the `plcsim` container is running
- WHEN a TCP connection is established to `:44818`
- THEN the connection is accepted without TLS errors

Test layer: smoke (TCP probe via `net.Dial`)

#### Scenario: Simulator builds with CGO_ENABLED=0

- GIVEN the repository source
- WHEN `CGO_ENABLED=0 go build ./cmd/plcsim` is run on all four target platforms
- THEN the build exits 0 and produces a binary

Test layer: unit (go build in CI matrix)

---

### [MVP-FND-9.3] Gateway TCP probe to plcsim on startup

When the gateway starts inside the Docker Compose stack, it MUST perform a TCP probe to `plcsim:44818` and log the result at INFO level with `component="datadir"` (or `component="startup"`) and the outcome (`"plcsim reachable"` or `"plcsim unreachable"`). The probe MUST NOT fail the gateway startup — it is informational only for Phase 0. A full CIP connection is deferred to the `plc-driver` change.

#### Scenario: Gateway logs plcsim reachability

- GIVEN both gateway and plcsim containers are running
- WHEN the gateway initialises
- THEN the gateway logs contain `"plcsim reachable"` within 60 seconds of startup

Test layer: smoke (log scraping in Compose integration test)

#### Scenario: Probe failure does not crash gateway

- GIVEN plcsim is not running
- WHEN the gateway initialises
- THEN the gateway logs contain `"plcsim unreachable"`
- AND the gateway process remains running (does not exit)

Test layer: smoke

---

### [MVP-FND-9.4] Multi-stage Dockerfile with restic binary

The production `docker/Dockerfile` MUST use a multi-stage build:

1. Stage `builder`: Go toolchain image, builds the `lgb` binary with `CGO_ENABLED=0`.
2. Stage `restic-bin`: `restic/restic` upstream image, provides the `restic` binary.
3. Stage `final`: minimal base (e.g. `gcr.io/distroless/static` or `debian:bookworm-slim`), copies the `lgb` binary from `builder` and the `restic` binary from `restic-bin`.

The resulting image MUST contain both `lgb` and `restic` at `/usr/local/bin/`. The image MUST NOT include the Go toolchain in the final stage.

#### Scenario: Final image contains lgb and restic

- GIVEN the production Dockerfile is built
- WHEN `docker run --rm lgb which lgb` and `docker run --rm lgb which restic` are executed
- THEN both commands return exit 0 and the binary paths

Test layer: smoke (Docker build + run in CI)

---

### [MVP-FND-9.5] Frontend scaffold

The `frontend/` directory MUST contain a Vite + React + TypeScript scaffold. It MUST include `package.json`, `vite.config.ts`, `tsconfig.json`, `.nvmrc` (pinned to Node 20), and `src/main.tsx` (empty placeholder). Running `cd frontend && npm ci && npm run build` MUST exit 0 and produce a `frontend/dist/` directory.

Platform note: the Node.js toolchain is not subject to the pure-Go constraint. The `.nvmrc` file MUST pin the major Node version to 20 for reproducibility.

#### Scenario: Frontend scaffold builds cleanly

- GIVEN Node 20 is installed and `frontend/package.json` is present
- WHEN `cd frontend && npm ci && npm run build` is run
- THEN exit code is 0
- AND `frontend/dist/` is created with at minimum an `index.html`

Test layer: smoke (npm build in CI frontend job)

#### Scenario: .nvmrc pins Node 20

- GIVEN the repository is checked out
- WHEN `cat frontend/.nvmrc` is read
- THEN the content begins with `20`

---

### [MVP-FND-9.6] `embed.go` — frontend dist embedding stub

The root-level `embed.go` MUST declare a `//go:embed all:frontend/dist` directive and expose the embedded filesystem for future use. In Phase 0, the directive MAY be guarded by a build tag (e.g. `//go:build ignore`) to allow `go build ./...` to succeed before the frontend is built. However, the file MUST be present in the repository and the directive MUST be active (guard removed) before this change is archived.

#### Scenario: embed.go is present at the repository root

- GIVEN the change is applied
- WHEN `ls embed.go` is run at the repository root
- THEN the file exists

#### Scenario: Build succeeds after npm run build

- GIVEN `frontend/dist/` exists (after `npm run build`)
- WHEN `go build ./...` is run without the guard build tag
- THEN exit code is 0

---

### [MVP-FND-9.7] `make docker-up` and `make docker-down` targets

The Makefile MUST include `docker-up` and `docker-down` targets. `docker-up` MUST run `docker compose -f docker-compose.dev.yml up -d`. `docker-down` MUST run `docker compose -f docker-compose.dev.yml down`. Both MUST be documented with a `## Help` comment for `make help` output.

#### Scenario: make docker-up starts the stack

- GIVEN Docker Engine is running
- WHEN `make docker-up` is run
- THEN `docker compose ... up -d` is invoked
- AND exit code is 0 when all containers start

---

### [MVP-FND-9.8] Makefile cross-compile targets

The Makefile MUST include a `build-all` (or `cross-compile`) target that builds the `lgb` binary for all four platform/arch pairs using `CGO_ENABLED=0`. Each target binary MUST be placed in `bin/lgb-{GOOS}-{GOARCH}`. The `make test` target MUST invoke `go test ./... -race -count=1`.

#### Scenario: build-all produces four binaries

- GIVEN the source compiles for all four targets
- WHEN `make build-all` is run
- THEN `bin/lgb-linux-amd64`, `bin/lgb-linux-arm64`, `bin/lgb-darwin-arm64`, and `bin/lgb-windows-amd64.exe` all exist
- AND exit code is 0

Test layer: smoke (CI matrix)

---

### [MVP-FND-9.9] `.golangci.yml` — strict baseline linter config

The `.golangci.yml` file MUST enable at minimum the following linters: `errcheck`, `staticcheck`, `gosimple`, `unused`, `govet`, `ineffassign`. The configuration MUST be valid for `golangci-lint` v1.60+. Running `golangci-lint run` against the Phase 0 source MUST exit 0 (the initial source MUST be written to pass the baseline without suppressions).

#### Scenario: golangci-lint passes on Phase 0 source

- GIVEN the Phase 0 source is written
- WHEN `golangci-lint run` is executed
- THEN exit code is 0
- AND no issues from the six required linters are reported

Test layer: lint (CI step)

---

### [MVP-FND-9.10] `.goreleaser.yaml` skeleton

The `.goreleaser.yaml` file MUST exist at the repository root with a valid skeleton targeting all four platforms. It MUST NOT be wired into CI in Phase 0. It MUST define the `builds` section with `env: [CGO_ENABLED=0]` and the four target GOOS/GOARCH pairs. The file serves as documentation of the release pipeline shape for the `release-pipeline` change.

#### Scenario: goreleaser.yaml exists and is valid YAML

- GIVEN the change is applied
- WHEN `.goreleaser.yaml` is parsed as YAML
- THEN it is valid (no syntax errors)
- AND the `builds[0].env` list contains `CGO_ENABLED=0`
