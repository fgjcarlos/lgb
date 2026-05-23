---
change: mvp-foundation
phase: tasks
date: 2026-05-22
status: draft
---

# Tasks: MVP Foundation — Phase 0 Scaffolding

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | ~1700 |
| 400-line budget risk | High |
| Chained PRs recommended | Yes |
| Suggested split | Skeleton → CLI → Dev Stack → Tooling (4 stacked PRs) |
| Delivery strategy | ask-on-risk |
| Chain strategy | stacked-to-main |

Decision needed before apply: Yes
Chained PRs recommended: Yes
Chain strategy: stacked-to-main
400-line budget risk: High

### Suggested Work Units

| Unit | Goal | Likely PR | Notes |
|------|------|-----------|-------|
| 1 | `internal/` package skeleton + unit tests | `chore/mvp-foundation-skeleton` | Targets `main`; no usable binary yet |
| 2 | Cobra CLI + doctor + HTTP server stub | `chore/mvp-foundation-cli` | Stacked on PR 1; binary is runnable after merge |
| 3 | plcsim, Docker stack, frontend scaffold | `chore/mvp-foundation-dev-stack` | Stacked on PR 2; `docker compose up` works after merge |
| 4 | Linting, release skeleton, ADRs, CI | `chore/mvp-foundation-tooling` | Stacked on PR 3; lint gates active after merge |

---

## Risk Acknowledgements

| Risk | Mitigation |
|------|------------|
| `embed.go` `!no_embed` guard must be removed before archive | Tracked as T-3.07; verify phase will flag if guard is still present |
| Windows ACL deferral (`0700` is advisory) | Documented in design §9; acceptance test skips mode check on Windows |
| `golangci-lint-action` SHA pin unresolved (`<SHA-v6>` placeholder) | T-4.01 resolves the exact SHA before any CI work in slice 4 |
| gologix beta API surface | Pinned at `v0.41.0-beta` in `go.mod`; wrapped in `internal/testutil` only |
| `LGB_AUTH_JWT_SECRET=dev-secret-not-for-prod` in `docker-compose.dev.yml` | README warning required; doctor check in Phase 1; tracked in T-3.05 |

---

## Chain Composition Summary

| Slice | Branch | ~Lines | Base | Self-mergeable |
|-------|--------|--------|------|----------------|
| 1 — Skeleton | `chore/mvp-foundation-skeleton` | ~450 | `main` | Yes (unit tests, no binary) |
| 2 — CLI | `chore/mvp-foundation-cli` | ~500 | slice 1 | Yes (binary builds; smoke tests pass) |
| 3 — Dev Stack | `chore/mvp-foundation-dev-stack` | ~400 | slice 2 | Yes (`docker compose up` works) |
| 4 — Tooling | `chore/mvp-foundation-tooling` | ~350 | slice 3 | Yes (lint + CI gates active) |

Each slice is rebased onto the previous before opening a PR. The diff of each PR shows only that slice's changes.

---

## Slice 1 — `chore/mvp-foundation-skeleton` (~450 lines)

Foundation packages with unit tests. No runnable binary yet. All tasks follow RED → GREEN order.

### Group A — Shared scaffolding

- [x] **T-1.01** `chore` — Add direct deps to `go.mod`: `github.com/spf13/cobra@v1.10.2`, `github.com/knadh/koanf/v2@v2.3.4`, `koanf/providers/{file,env,confmap}`, `koanf/parsers/yaml`, `github.com/danomagnum/gologix@v0.41.0-beta`; run `go mod tidy`.
  - **Files**: `go.mod`, `go.sum`
  - **Reqs**: MVP-FND-2.1, MVP-FND-6.6, MVP-FND-9.2
  - **Design**: §16 (pure-Go dep verification)
  - **Deps**: none
  - **DoD**: `go mod tidy` exits 0; `CGO_ENABLED=0 go build ./...` exits 0 (only `main` package stubs needed).

- [x] **T-1.02** `test` — **[RED]** Write `internal/errors/errors_test.go`: assert all seven sentinels (`ErrConfigInvalid`, `ErrConfigMissing`, `ErrConfigPermission`, `ErrDataDirInvalid`, `ErrDataDirPermission`, `ErrCheckFailed`, `ErrMaxAttempts`) are distinct non-nil errors; assert `errors.Is` wrapping works; assert `Join` helper preserves each constituent.
  - **Files**: `internal/errors/errors_test.go`
  - **Reqs**: MVP-FND-5.1, MVP-FND-5.3
  - **Design**: §8 (error model)
  - **Deps**: T-1.01
  - **DoD**: `go test ./internal/errors/...` FAILS (package does not exist yet).

- [x] **T-1.03** `impl` — **[GREEN]** Create `internal/errors/errors.go`: export the seven sentinels via `errors.New`; thin `Join(errs ...error) error` wrapper over stdlib `errors.Join`.
  - **Files**: `internal/errors/errors.go`
  - **Reqs**: MVP-FND-5.1, MVP-FND-5.3, MVP-FND-5.4
  - **Design**: §8
  - **Deps**: T-1.02
  - **DoD**: `go test ./internal/errors/...` passes.

### Group B — Version package

- [x] **T-1.04** `test` — **[RED]** Write `internal/version/version_test.go`: assert `Version`, `Commit`, `Date` fall back to `"dev"`, `"none"`, `"unknown"` when unset; assert `Info()` returns populated struct.
  - **Files**: `internal/version/version_test.go`
  - **Reqs**: MVP-FND-1.7
  - **Design**: §3 (version package), decision #25
  - **Deps**: T-1.01
  - **DoD**: test file compiles and fails (missing package).

- [x] **T-1.05** `impl` — **[GREEN]** Create `internal/version/version.go`: package-level `var Version = "dev"`, `Commit = "none"`, `Date = "unknown"`; `type Info struct{Version, Commit, Date string}`; `func Info() Info`.
  - **Files**: `internal/version/version.go`
  - **Reqs**: MVP-FND-1.7
  - **Design**: §3
  - **Deps**: T-1.04
  - **DoD**: `go test ./internal/version/...` passes; `CGO_ENABLED=0 go build` exits 0.

### Group C — Retry

- [x] **T-1.06** `test` — **[RED]** Write `internal/retry/retry_test.go`: table-driven tests for (a) successful fn on first call returns nil, (b) exponential delay growth with `Jitter=0.0` (mock clock or fake time via injectable sleep), (c) context cancellation returns `ctx.Err()`, (d) `MaxAttempts=3` exhaustion returns `ErrMaxAttempts` with last fn error unwrappable, (e) zero `Options` uses defaults.
  - **Files**: `internal/retry/retry_test.go`
  - **Reqs**: MVP-FND-6.1–6.5
  - **Design**: §4.2 (retry.Do contract), §20.3
  - **Deps**: T-1.03
  - **DoD**: tests fail (missing package).

- [x] **T-1.07** `impl` — **[GREEN]** Create `internal/retry/retry.go`: `type Options struct{Initial, Max time.Duration; MaxAttempts int; Jitter float64}`; `func Do(ctx, opts, fn) error` with exponential backoff, ±jitter, `select{timer|ctx.Done}`, `ErrMaxAttempts` sentinel wrapping (re-exported from `internal/errors`).
  - **Files**: `internal/retry/retry.go`
  - **Reqs**: MVP-FND-6.1–6.6
  - **Design**: §4.2
  - **Deps**: T-1.06
  - **DoD**: `go test ./internal/retry/...` passes; `go list -deps ./internal/retry` shows only stdlib.

### Group D — Logger

- [x] **T-1.08** `test` — **[RED]** Write `internal/log/log_test.go`: (a) JSON format emits valid JSON per line, (b) text format emits key=value, (c) invalid level string returns error (not panic), (d) invalid format string returns error, (e) race detector: two goroutines log concurrently → no data race (use `-race`), (f) DEBUG messages suppressed at INFO level.
  - **Files**: `internal/log/log_test.go`
  - **Reqs**: MVP-FND-4.1–4.3, MVP-FND-4.6
  - **Design**: §7
  - **Deps**: T-1.01
  - **DoD**: tests fail (missing package).

- [x] **T-1.09** `impl` — **[GREEN]** Create `internal/log/log.go`: `type Options struct{Level, Format string; Out io.Writer}`; `func New(opts Options) (*slog.Logger, error)` selecting `NewTextHandler`/`NewJSONHandler`; `AddSource: lvl == slog.LevelDebug`; error on invalid level/format. Create `internal/log/redact.go`: `redactingHandler` wrapping chosen handler; key set derived from `secret:"true"` tags at init via reflection (receives key set as parameter from `internal/config` — no import cycle).
  - **Files**: `internal/log/log.go`, `internal/log/redact.go`
  - **Reqs**: MVP-FND-4.1–4.3, MVP-FND-4.5, MVP-FND-4.6
  - **Design**: §7
  - **Deps**: T-1.08
  - **DoD**: `go test ./internal/log/...` passes including race test.

### Group E — Config

- [x] **T-1.10** `test` — **[RED]** Write `internal/config/config_test.go` and `testdata/`: (a) load `testdata/sample.yaml` → typed `*Config` with expected field values, (b) missing file → error wrapping `ErrConfigMissing`, (c) camelCase key preserved (`logLevel` not `loglevel`), (d) env var `LGB_GATEWAY_LOGLEVEL` overrides YAML value, (e) `Validate()` with valid config returns nil, (f) `Validate()` with two violations returns joined error containing both, `errors.Is(err, ErrConfigInvalid)` true, (g) `jwtSecret` from env overrides empty YAML field, (h) `Redacted()` replaces secret fields with `"[redacted]"`. Create `testdata/sample.yaml` (valid) and `testdata/invalid.yaml` (two violations).
  - **Files**: `internal/config/config_test.go`, `internal/config/testdata/sample.yaml`, `internal/config/testdata/invalid.yaml`
  - **Reqs**: MVP-FND-2.1–2.6, MVP-FND-3.1, MVP-FND-3.2
  - **Design**: §4.1, §5.1–5.4
  - **Deps**: T-1.03
  - **DoD**: tests fail (missing package).

- [x] **T-1.11** `impl` — **[GREEN]** Create `internal/config/config.go`: `type Config struct` with all sections and `secret:"true"` tags on `JwtSecret`, `Password`, `PasswordFile`; `(*Config).Redacted() *Config` via reflection; `(*Config).Validate() error` using `errors.Join`. Create `internal/config/loader.go`: `func Load(path string) (*Config, error)` using koanf provider stack (confmap defaults → file+yaml → env `LGB_` prefix → unmarshal to typed struct); package-level doc comment documenting secret convention. Create `internal/config/watcher.go`: `func Watch(ctx, path string, onChange func(*Config)) error` with 200 ms debounce.
  - **Files**: `internal/config/config.go`, `internal/config/loader.go`, `internal/config/watcher.go`
  - **Reqs**: MVP-FND-2.1–2.6, MVP-FND-3.1, MVP-FND-3.2
  - **Design**: §4.1, §5.1–5.4
  - **Deps**: T-1.10
  - **DoD**: `go test ./internal/config/...` passes.

- [x] **T-1.12** `test` — **[RED]** Write `internal/config/watcher_test.go` (build-tagged `//go:build integration`): (a) file change triggers `onChange` within 1 s, (b) five writes within 100 ms trigger exactly one callback, (c) context cancel stops watcher and returns `ctx.Err()`.
  - **Files**: `internal/config/watcher_test.go`
  - **Reqs**: MVP-FND-2.5
  - **Design**: §5.2 (hot-reload), §20.2
  - **Deps**: T-1.11
  - **DoD**: `go test -tags=integration ./internal/config/...` passes.

### Group F — dataDir

- [x] **T-1.13** `test` — **[RED]** Write `internal/datadir/datadir_test.go`: (a) `DefaultPath()` returns `/var/lib/lgb` on Linux, (b) `Ensure` creates missing dir with `0700` on POSIX, (c) `Ensure` on existing writable dir returns nil, (d) `Ensure` on existing regular file returns `ErrDataDirInvalid`, (e) `Resolve` with cliOverride wins over cfg value, (f) `Resolve` with empty override uses cfg; (g) write probe returns `ErrDataDirPermission` on unwritable dir (integration tag for fs-side-effects). Use `t.TempDir()` for filesystem tests.
  - **Files**: `internal/datadir/datadir_test.go`
  - **Reqs**: MVP-FND-7.1–7.4
  - **Design**: §9.1–9.4, §4.4
  - **Deps**: T-1.11
  - **DoD**: tests fail (missing package).

- [x] **T-1.14** `impl` — **[GREEN]** Create `internal/datadir/datadir.go`: `Resolve(cfg, cliOverride string) (string, error)`, `Ensure(path string) (string, error)` (expand `~`, `filepath.Abs`, `MkdirAll 0700`, write probe), `DefaultPath()` delegating to build-tag files. Create `internal/datadir/default_unix.go` (`//go:build !darwin && !windows`), `default_darwin.go`, `default_windows.go`.
  - **Files**: `internal/datadir/datadir.go`, `internal/datadir/default_unix.go`, `internal/datadir/default_darwin.go`, `internal/datadir/default_windows.go`
  - **Reqs**: MVP-FND-7.1–7.5
  - **Design**: §9.1–9.4
  - **Deps**: T-1.13
  - **DoD**: `go test ./internal/datadir/...` passes; `CGO_ENABLED=0 go build` exits 0.

### Group G — testutil scaffold

- [x] **T-1.15** `impl` — Create `internal/testutil/config.go`: `MinimalConfig(t *testing.T) *config.Config` returning a minimum-valid config using `t.TempDir()` as dataDir. No test needed (it is itself a test helper; verified by use in later slices).
  - **Files**: `internal/testutil/config.go`
  - **Reqs**: MVP-FND-2.6 (test ergonomics)
  - **Design**: §3 (testutil package)
  - **Deps**: T-1.11
  - **DoD**: `CGO_ENABLED=0 go build ./internal/testutil/...` exits 0.

---

## Slice 2 — `chore/mvp-foundation-cli` (~500 lines)

Cobra command tree, HTTP server stub, doctor checks, dataDir bootstrap. After merge the binary builds and every subcommand is testable.

### Group A — Scaffolding tests

- [x] **T-2.01** `chore` — Create binary entry-point stubs: `cmd/lgb/main.go` (calls `cmd.NewRoot().Execute()`, sets `slog.SetDefault` after boot); `cmd/lgb/cmd/root.go` (`NewRoot`, `Deps` struct, `PersistentPreRunE` skeleton); `cmd/lgb/cmd/exit.go` (`exitCode(err) int` mapping table). Create `cmd/lgb/testdata/sample.yaml` (copy from `internal/config/testdata/sample.yaml`).
  - **Files**: `cmd/lgb/main.go`, `cmd/lgb/cmd/root.go`, `cmd/lgb/cmd/exit.go`, `cmd/lgb/testdata/sample.yaml`
  - **Reqs**: MVP-FND-1.1, MVP-FND-5.5
  - **Design**: §6.1–6.4, §20.1
  - **Deps**: T-1.15
  - **DoD**: `CGO_ENABLED=0 go build ./cmd/lgb` exits 0.

- [x] **T-2.02** `test` — **[RED]** Write `cmd/lgb/cmd/version_test.go`: (a) plain output contains `"dev"` for unset build, (b) `--json` output parses as `{"version":…,"commit":…,"date":…}`, (c) exit code is 0.
  - **Files**: `cmd/lgb/cmd/version_test.go`
  - **Reqs**: MVP-FND-1.2
  - **Design**: §6.1, §6.5
  - **Deps**: T-2.01
  - **DoD**: tests fail (command not wired).

- [x] **T-2.03** `impl` — **[GREEN]** Create `cmd/lgb/cmd/version.go`: `NewVersionCmd(d *Deps)` prints `version.Info()` as plain or `--json`; exits 0.
  - **Files**: `cmd/lgb/cmd/version.go`
  - **Reqs**: MVP-FND-1.2, MVP-FND-1.7
  - **Design**: §6.1
  - **Deps**: T-2.02
  - **DoD**: `go test ./cmd/lgb/cmd/...` (version tests) passes.

### Group B — Config validate command

- [x] **T-2.04** `test` — **[RED]** Write `cmd/lgb/cmd/config_validate_test.go`: (a) `testdata/sample.yaml` → stdout `config OK`, exit 0; (b) `testdata/invalid.yaml` → exit 1, stderr lists both violations; (c) missing file path → exit 1 referencing file; (d) `--json` valid → `{"valid":true}`; (e) `--json` invalid → `{"valid":false,"errors":[…]}`.
  - **Files**: `cmd/lgb/cmd/config_validate_test.go`, `cmd/lgb/testdata/invalid.yaml`
  - **Reqs**: MVP-FND-1.6
  - **Design**: §6.1, §6.5
  - **Deps**: T-2.03
  - **DoD**: tests fail (command not wired).

- [x] **T-2.05** `impl` — **[GREEN]** Create `cmd/lgb/cmd/config.go` (group) and `cmd/lgb/cmd/config_validate.go`: load & validate; plain/JSON output; no server started; exit 0/1.
  - **Files**: `cmd/lgb/cmd/config.go`, `cmd/lgb/cmd/config_validate.go`
  - **Reqs**: MVP-FND-1.6
  - **Design**: §6.1
  - **Deps**: T-2.04
  - **DoD**: `go test ./cmd/lgb/cmd/...` (config tests) passes.

### Group C — Status command

- [x] **T-2.06** `test` — **[RED]** Write `cmd/lgb/cmd/status_test.go`: stdout is valid JSON containing `"status":"ok"`, exit 0.
  - **Files**: `cmd/lgb/cmd/status_test.go`
  - **Reqs**: MVP-FND-1.5
  - **Design**: §6.1
  - **Deps**: T-2.03
  - **DoD**: test fails (command not wired).

- [x] **T-2.07** `impl` — **[GREEN]** Create `cmd/lgb/cmd/status.go`: prints `{"status":"ok","phase":"0","uptime_seconds":0}`; exits 0.
  - **Files**: `cmd/lgb/cmd/status.go`
  - **Reqs**: MVP-FND-1.5
  - **Design**: §6.1
  - **Deps**: T-2.06
  - **DoD**: `go test ./cmd/lgb/cmd/...` (status tests) passes.

### Group D — HTTP server internals

- [x] **T-2.08** `test` — **[RED]** Write `internal/health/handler_test.go`: `GET /health` → 200, body `{"status":"ok"}`, `Content-Type: application/json`. Write `internal/server/server_test.go`: (a) `Run(ctx)` binds port from config, `/health` returns 200, `/metrics` returns 200 with correct `Content-Type`; (b) `Run(ctx)` returns nil on context cancel (graceful shutdown within 1 s); (c) `/readyz` returns 200 after bind.
  - **Files**: `internal/health/handler_test.go`, `internal/server/server_test.go`
  - **Reqs**: MVP-FND-1.3, MVP-FND-1.8, MVP-FND-1.9
  - **Design**: §11, §4.3, §4.5
  - **Deps**: T-1.15
  - **DoD**: tests fail (missing packages).

- [x] **T-2.09** `impl` — **[GREEN]** Create `internal/health/handler.go`: `Handler() http.Handler` returning 200 JSON. Create `internal/httpx/shutdown.go`: `Shutdown(ctx, srv, deadline) error`. Create `internal/httpx/mux.go`: shared mux constructor. Create `internal/server/server.go`: `New(cfg, log, checks) *Server`; `(*Server).Run(ctx) error` mounts `/health`, `/metrics` (stub body `"# empty\n"`), `/readyz`; uses `httpx.Shutdown`.
  - **Files**: `internal/health/handler.go`, `internal/httpx/shutdown.go`, `internal/httpx/mux.go`, `internal/server/server.go`
  - **Reqs**: MVP-FND-1.3, MVP-FND-1.8, MVP-FND-1.9
  - **Design**: §11, §4.3–4.5
  - **Deps**: T-2.08
  - **DoD**: `go test ./internal/...` passes.

### Group E — Doctor internals

- [x] **T-2.10** `test` — **[RED]** Write `internal/doctor/doctor_test.go`: (a) 3 registered checks → 3 results, (b) parallel execution (goroutine count via test spy), (c) panicking check recovered into FAIL result, other checks unaffected, (d) worst-result exit-code table (all pass → 0, warn only → 0, any fail → 1). Write `internal/doctor/checks_test.go`: (a) `restic-on-path` WARN when binary absent, (b) `data-dir-writable` FAIL when dir unwritable, (c) `http-port-available` FAIL when port bound, (d) `go-runtime-version` returns INFO status, (e) `config-loaded` always PASS.
  - **Files**: `internal/doctor/doctor_test.go`, `internal/doctor/checks_test.go`
  - **Reqs**: MVP-FND-8.1–8.6
  - **Design**: §10, §4.3, §20.4
  - **Deps**: T-1.14
  - **DoD**: tests fail (missing package).

- [x] **T-2.11** `impl` — **[GREEN]** Create `internal/doctor/doctor.go`: `CheckStatus` enum, `Result` struct, `Check` interface, `Registry` struct, `Run(ctx, reg) []Result` (errgroup + recover), `Default(cfg) *Registry`. Create `internal/doctor/checks.go`: five unexported check structs (`dataDirCheck`, `resticCheck`, `goRuntimeCheck`, `portCheck`, `configLoadedCheck`).
  - **Files**: `internal/doctor/doctor.go`, `internal/doctor/checks.go`
  - **Reqs**: MVP-FND-8.1–8.6
  - **Design**: §10, §4.3
  - **Deps**: T-2.10
  - **DoD**: `go test ./internal/doctor/...` passes.

### Group F — Doctor and server commands

- [x] **T-2.12** `test` — **[RED]** Write `cmd/lgb/cmd/doctor_test.go`: (a) all checks pass → exit 0, stdout `[PASS]` entries; (b) injected FAIL check → exit 1; (c) injected WARN only → exit 0; (d) `--json` → valid JSON with `checks` array and `overall` field. Use injectable `Deps.Doctor` registry (fake checks implementing `doctor.Check`).
  - **Files**: `cmd/lgb/cmd/doctor_test.go`
  - **Reqs**: MVP-FND-1.4, MVP-FND-8.3–8.5
  - **Design**: §6.1–6.3, §20.4
  - **Deps**: T-2.11
  - **DoD**: tests fail (command not wired).

- [x] **T-2.13** `impl` — **[GREEN]** Create `cmd/lgb/cmd/doctor.go`: `NewDoctorCmd(d *Deps)` builds `doctor.Default(d.Config)`, calls `doctor.Run`, formats plain/JSON output, maps exit codes per §8.3.
  - **Files**: `cmd/lgb/cmd/doctor.go`
  - **Reqs**: MVP-FND-1.4, MVP-FND-8.3–8.5
  - **Design**: §6.1
  - **Deps**: T-2.12
  - **DoD**: `go test ./cmd/lgb/cmd/...` (doctor tests) passes.

- [x] **T-2.14** `test` — **[RED]** Write `cmd/lgb/cmd/server_test.go`: (a) `jwtSecret == ""` + no env → exits 1, stderr contains `auth.jwtSecret is required`; (b) valid config + `LGB_AUTH_JWT_SECRET` set → `Run` context-cancelled cleanly → exit 0; (c) dataDir bootstrap called (spy via injectable function); (d) `LGB_GATEWAY_LOGLEVEL` env override respected.
  - **Files**: `cmd/lgb/cmd/server_test.go`
  - **Reqs**: MVP-FND-1.3, MVP-FND-2.4, MVP-FND-3.1, MVP-FND-7.5
  - **Design**: §6.3, §20.1
  - **Deps**: T-2.09, T-2.11
  - **DoD**: tests fail (command not wired).

- [x] **T-2.15** `impl` — **[GREEN]** Create `cmd/lgb/cmd/server.go`: `NewServerCmd(d *Deps)`: calls `datadir.Ensure`, validates `jwtSecret`, wires `signal.NotifyContext`, calls `server.New(d.Config, d.Logger, doctor.Default(d.Config).Checks()).Run(ctx)`, logs resolved dataDir at INFO (`component="datadir"`).
  - **Files**: `cmd/lgb/cmd/server.go`
  - **Reqs**: MVP-FND-1.3, MVP-FND-1.9, MVP-FND-7.5
  - **Design**: §6.3, §20.1
  - **Deps**: T-2.14
  - **DoD**: `go test ./cmd/lgb/cmd/...` passes; `CGO_ENABLED=0 go build ./cmd/lgb` exits 0.

### Group G — Root wiring + PersistentPreRunE

- [x] **T-2.16** `test` — **[RED]** Write `cmd/lgb/cmd/root_test.go`: (a) `lgb --help` → exit 0, stdout contains subcommand list; (b) `lgb --unknown-flag` → exit 1, stderr mentions unknown flag; (c) `PersistentPreRunE` populates `Deps.Config` and `Deps.Logger` before subcommand runs (spy via `version` subcommand test).
  - **Files**: `cmd/lgb/cmd/root_test.go`
  - **Reqs**: MVP-FND-1.1
  - **Design**: §6.2–6.3
  - **Deps**: T-2.15
  - **DoD**: tests fail (PersistentPreRunE not fully wired).

- [x] **T-2.17** `impl` — **[GREEN]** Complete `cmd/lgb/cmd/root.go`: register all flags; `PersistentPreRunE` calls `config.Load`, applies CLI overrides to `*Config`, calls `log.New`, sets `slog.SetDefault`; wire all subcommands. Update `cmd/lgb/main.go` Makefile LDFLAGS target (`-X github.com/fgjcarlos/lgb/internal/version.Version=…`).
  - **Files**: `cmd/lgb/cmd/root.go`, `cmd/lgb/main.go`, `Makefile`
  - **Reqs**: MVP-FND-1.1, MVP-FND-1.7, MVP-FND-4.2, MVP-FND-4.3
  - **Design**: §6.2–6.4
  - **Deps**: T-2.16
  - **DoD**: `go test ./cmd/lgb/...` passes (all CLI smoke tests green); `CGO_ENABLED=0 go build ./cmd/lgb` exits 0.

### Group H — Integration smoke test

- [x] **T-2.18** `test` — **[RED/integration]** Write `cmd/lgb/e2e/server_e2e_test.go` (`//go:build e2e`): spawn built binary, `lgb server --config testdata/sample.yaml`, poll `GET /health`, assert 200; send SIGTERM, assert exit 0. Write `cmd/lgb/e2e/smoke_test.go` (`//go:build e2e`): `lgb version --json` → valid JSON, exit 0; `lgb status` → JSON with `status:"ok"`, exit 0; `lgb config validate --config testdata/sample.yaml` → exit 0.
  - **Files**: `cmd/lgb/e2e/server_e2e_test.go`, `cmd/lgb/e2e/smoke_test.go`
  - **Reqs**: MVP-FND-1.2–1.6
  - **Design**: §17 (e2e layer)
  - **Deps**: T-2.17
  - **DoD**: `go test -tags=e2e ./cmd/lgb/e2e/...` passes (requires pre-built binary from `make build`).

---

## Slice 3 — `chore/mvp-foundation-dev-stack` (~400 lines)

plcsim binary, Docker files, docker-compose, frontend scaffold, embed.go stub.

### Group A — plcsim

- [x] **T-3.01** `test` — **[RED]** Write `cmd/plcsim/main_test.go` (`//go:build integration`): `testutil.StartPLCSim(t)` starts in-process gologix server; `net.Dial("tcp", addr)` succeeds; three required tags (`SimBool`, `SimInt`, `SimFloat`) are accessible via tag provider (read back from `MapTagProvider`).
  - **Files**: `cmd/plcsim/main_test.go`
  - **Reqs**: MVP-FND-9.2
  - **Design**: §12, §20.5
  - **Deps**: T-1.15
  - **DoD**: test fails (plcsim + testutil.StartPLCSim not implemented).

- [x] **T-3.02** `impl` — **[GREEN]** Create `cmd/plcsim/main.go`: build `gologix.MapTagProvider` seeded from embedded `testdata/tags.json` (`SimBool=true`, `SimInt=42`, `SimFloat=3.14`); `gologix.Server.Serve(":44818")`; SIGTERM → `Server.Close()`; log `"plcsim listening"` at INFO. Create `cmd/plcsim/testdata/tags.json`. Create `internal/testutil/plcsim.go`: `StartPLCSim(t) (addr string, stop func())` using same provider construction + embedded testdata.
  - **Files**: `cmd/plcsim/main.go`, `cmd/plcsim/testdata/tags.json`, `internal/testutil/plcsim.go`
  - **Reqs**: MVP-FND-9.2
  - **Design**: §12
  - **Deps**: T-3.01
  - **DoD**: `go test -tags=integration ./cmd/plcsim/...` passes; `CGO_ENABLED=0 go build ./cmd/plcsim` exits 0.

### Group B — Docker files

- [x] **T-3.03** `impl` — Create `docker/Dockerfile`: three-stage build (`restic-bin` from `restic/restic:0.18.0`, `build` from `golang:1.24-alpine`, `final` from `gcr.io/distroless/static-debian12:nonroot`); copies `lgb` + `restic` to `/usr/local/bin/`; `ENTRYPOINT` + `CMD`. Create `docker/Dockerfile.dev`: single-stage `golang:1.24-alpine` with `air` installed, workspace mount. Create `docker/Dockerfile.plcsim`: multi-stage targeting `./cmd/plcsim`.
  - **Files**: `docker/Dockerfile`, `docker/Dockerfile.dev`, `docker/Dockerfile.plcsim`
  - **Reqs**: MVP-FND-9.1, MVP-FND-9.4
  - **Design**: §13.2–13.4
  - **Deps**: T-3.02
  - **DoD**: `docker build -f docker/Dockerfile .` exits 0 (CI smoke); final image contains `lgb` and `restic`.

- [x] **T-3.04** `impl` — Create `docker-compose.dev.yml` with three services (`gateway`, `plcsim`, `mqtt`), healthchecks, `LGB_AUTH_JWT_SECRET=dev-secret-not-for-prod` env on gateway, named volume `lgb-data`, network `lgb-dev`.
  - **Files**: `docker-compose.dev.yml`
  - **Reqs**: MVP-FND-9.1, MVP-FND-9.3
  - **Design**: §13.1
  - **Deps**: T-3.03
  - **DoD**: `docker compose -f docker-compose.dev.yml config` exits 0 (YAML valid).

- [x] **T-3.05** `docs` — Add README section warning that `LGB_AUTH_JWT_SECRET=dev-secret-not-for-prod` in `docker-compose.dev.yml` is NOT production-safe; update `README.md` Getting Started section to reference `make docker-up`.
  - **Files**: `README.md`
  - **Reqs**: MVP-FND-9.1 (risk mitigation)
  - **Design**: §24 (risk: dev JWT secret)
  - **Deps**: T-3.04
  - **DoD**: README contains dev-secret warning.

### Group C — Frontend scaffold

- [x] **T-3.06** `impl` — Create `frontend/` Vite + React + TS scaffold: `package.json` (react, react-dom, vite, typescript as devDeps), `vite.config.ts`, `tsconfig.json`, `.nvmrc` (content: `20`), `src/main.tsx` (empty placeholder comment only). Running `npm ci && npm run build` in `frontend/` MUST exit 0 and produce `frontend/dist/index.html`.
  - **Files**: `frontend/package.json`, `frontend/vite.config.ts`, `frontend/tsconfig.json`, `frontend/.nvmrc`, `frontend/src/main.tsx`
  - **Reqs**: MVP-FND-9.5
  - **Design**: §21 (file-change summary, frontend row)
  - **Deps**: T-3.04
  - **DoD**: `cd frontend && npm ci && npm run build` exits 0; `frontend/dist/index.html` exists.

- [x] **T-3.07** `impl` — Create root-level `embed.go` with `//go:build !no_embed` guard and `//go:embed all:frontend/dist` directive. Create root-level `noassets.go` with `//go:build no_embed` (empty package `main` — ensures the build tag works cleanly). Update `Makefile` `build` target to pass `-tags no_embed`; update CI build step to pass `-tags no_embed`. **Note**: the `!no_embed` guard MUST be removed before archive per spec MVP-FND-1.10; this task creates the guard-active version; a future archive-prep task will remove it.
  - **Files**: `embed.go`, `noassets.go`, `Makefile`
  - **Reqs**: MVP-FND-1.10, MVP-FND-9.6
  - **Design**: §24 (embed guard risk)
  - **Deps**: T-3.06
  - **DoD**: `CGO_ENABLED=0 go build -tags no_embed ./...` exits 0; `CGO_ENABLED=0 go build ./...` (without tag) exits 0 only when `frontend/dist` exists.

### Group D — Gateway plcsim probe + Makefile targets

- [x] **T-3.08** `test` — **[RED]** Write `cmd/lgb/cmd/server_probe_test.go` (`//go:build integration`): start `testutil.StartPLCSim(t)`, start server, assert log contains `"plcsim reachable"` within 10 s. Write second scenario: plcsim not running → log contains `"plcsim unreachable"`, server still running.
  - **Files**: `cmd/lgb/cmd/server_probe_test.go`
  - **Reqs**: MVP-FND-9.3
  - **Design**: §13.1 (gateway probe)
  - **Deps**: T-3.02, T-2.17
  - **DoD**: tests fail (probe not implemented in server command).

- [x] **T-3.09** `impl` — **[GREEN]** Update `cmd/lgb/cmd/server.go`: after `datadir.Ensure`, attempt `net.DialTimeout("tcp", plcsimAddr, 5s)` where `plcsimAddr` defaults to `plcsim:44818` (configurable via `cfg.PLCSim.Addr` or a const); log INFO `component="startup"` with `"plcsim reachable"` or `"plcsim unreachable"`.
  - **Files**: `cmd/lgb/cmd/server.go`
  - **Reqs**: MVP-FND-9.3
  - **Design**: §13.1
  - **Deps**: T-3.08
  - **DoD**: `go test -tags=integration ./cmd/lgb/cmd/...` passes (probe tests green).

- [x] **T-3.10** `impl` — Add `docker-up`, `docker-down` targets to `Makefile`; add `build-all` cross-compile target for four platforms; add `make test`, `make vet`, `make lint` (stubbed — lint binary not present yet, exits 0 gracefully when `.golangci.yml` absent).
  - **Files**: `Makefile`
  - **Reqs**: MVP-FND-9.7, MVP-FND-9.8
  - **Design**: §21 (Makefile row)
  - **Deps**: T-3.07
  - **DoD**: `make build-all` exits 0 (CGO_ENABLED=0 for all four targets); `make docker-up` invokes compose.

---

## Slice 4 — `chore/mvp-foundation-tooling` (~350 lines)

Linter config, release skeleton, ADRs, CI workflow updates. After merge, lint gates are active.

### Group A — SHA resolution (blocking prerequisite)

- [ ] **T-4.01** `chore` — Resolve the exact SHA for `golangci/golangci-lint-action@v6` by checking the upstream tag on `github.com/golangci/golangci-lint-action`. Record the SHA (format: `golangci/golangci-lint-action@<40-char-sha>`) in a comment at the top of `.github/workflows/ci.yml` and use it in the lint step. This task MUST complete before T-4.05.
  - **Files**: `.github/workflows/ci.yml` (comment only — full step added in T-4.05)
  - **Reqs**: MVP-FND-1.11
  - **Design**: §14
  - **Deps**: T-3.10
  - **DoD**: SHA resolved and recorded; CI file contains a comment with the resolved SHA and the rationale.

### Group B — Linting

- [ ] **T-4.02** `impl` — Create `.golangci.yml`: enable `errcheck`, `staticcheck`, `gosimple`, `unused`, `govet`, `ineffassign`; `run.timeout: 5m`; valid for `golangci-lint` v1.60+. Run `golangci-lint run` locally against all slice 1–3 source and fix any reported issues (no suppressions allowed for the six required linters).
  - **Files**: `.golangci.yml`, any source files with lint issues
  - **Reqs**: MVP-FND-9.9
  - **Design**: §19 decision #23
  - **Deps**: T-4.01
  - **DoD**: `golangci-lint run` exits 0 on the full source tree.

### Group C — Release skeleton

- [ ] **T-4.03** `impl` — Create `.goreleaser.yaml`: `builds[0]` section with `env: [CGO_ENABLED=0]` and all four GOOS/GOARCH pairs; `ldflags` injecting version/commit/date from `internal/version`; `archives`, `checksum`, `changelog` sections as skeleton. File MUST be valid YAML (no CI wiring yet).
  - **Files**: `.goreleaser.yaml`
  - **Reqs**: MVP-FND-9.10
  - **Design**: §19 decision #23, §21 (goreleaser row)
  - **Deps**: T-4.02
  - **DoD**: `goreleaser check .goreleaser.yaml` exits 0 (if goreleaser available) OR YAML parses without errors; `builds[0].env` contains `CGO_ENABLED=0`.

### Group D — ADRs

- [ ] **T-4.04** `docs` — Create `docs/adr/0000-template.md` (canonical ADR template: Status / Decision / Context / Options Considered / Rationale / Consequences / References). Create `docs/adr/README.md` as ADR index listing all 10 files (0000 template + 0001–0009). Create ADRs `0001` through `0009` in `docs/adr/` with status `Proposed`, titles and content per design §15. Each ADR body follows the template and documents the decision, context, options, rationale, and consequences.
  - **Files**: `docs/adr/0000-template.md`, `docs/adr/README.md`, `docs/adr/0001-cli-framework.md` … `docs/adr/0009-pure-go-no-cgo.md`
  - **Reqs**: MVP-FND-1.14
  - **Design**: §15
  - **Deps**: T-4.03
  - **DoD**: `ls docs/adr/` shows 11 files (0000 + 0001–0009 + README); each contains `**Status**: Proposed`.

### Group E — CI workflow updates

- [ ] **T-4.05** `config` — Update `.github/workflows/ci.yml`: (a) add `golangci-lint` step inside `has_go == true` guard using the SHA resolved in T-4.01, referencing `.golangci.yml`, running after `go vet`; (b) add `frontend-build` job with `has_frontend` detection step, `actions/setup-node@v4` with `node-version-file: 'frontend/.nvmrc'`, `npm ci && npm run build` step; (c) add `make generate` placeholder step inside `has_go == true` guard.
  - **Files**: `.github/workflows/ci.yml`
  - **Reqs**: MVP-FND-1.11, MVP-FND-1.12, MVP-FND-1.13
  - **Design**: §14
  - **Deps**: T-4.04
  - **DoD**: CI YAML is valid (`yamllint` / GitHub Actions parser accepts it); lint step SHA matches T-4.01 resolution; frontend step uses `.nvmrc`.

### Group F — Makefile generate target

- [ ] **T-4.06** `impl` — Add `generate` target to `Makefile`: checks for `.proto` files via `find . -name '*.proto'`; if none found, prints `# no .proto files — skipping protobuf codegen` and exits 0. Add `make adr-index` placeholder target (prints ADR list from `docs/adr/`). Add `make lint` target calling `golangci-lint run` (now that `.golangci.yml` exists).
  - **Files**: `Makefile`
  - **Reqs**: MVP-FND-1.13
  - **Design**: §21 (Makefile row)
  - **Deps**: T-4.05
  - **DoD**: `make generate` exits 0 with the expected notice; `make lint` exits 0.

### Group G — Archive-prep guard tracking

- [ ] **T-4.07** `chore` — Add a comment in `embed.go` and a GitHub issue reference (or TODO in `tasks.md`) that the `!no_embed` build-tag guard created in T-3.07 MUST be removed before this change is archived (per spec MVP-FND-1.10). Verify that `go build ./...` (without `-tags no_embed`) fails with `frontend/dist missing` when `frontend/dist` is absent — confirming the guard will catch the requirement at archive time.
  - **Files**: `embed.go` (comment update only)
  - **Reqs**: MVP-FND-1.10
  - **Design**: §24 (embed guard risk)
  - **Deps**: T-4.06
  - **DoD**: `embed.go` contains a `TODO(archive): remove !no_embed guard` comment; verify step will flag this if guard is still present at archive.

---

## Cross-slice dependency summary

```
T-1.01  →  T-1.02 → T-1.03 (errors)
T-1.01  →  T-1.04 → T-1.05 (version)
T-1.03  →  T-1.06 → T-1.07 (retry)
T-1.01  →  T-1.08 → T-1.09 (log)
T-1.03  →  T-1.10 → T-1.11 (config) → T-1.12 (watcher/integration)
T-1.11  →  T-1.13 → T-1.14 (datadir)
T-1.11  →  T-1.15 (testutil/config)

T-1.15  →  T-2.01 (CLI scaffolding)
T-2.01  →  T-2.02 → T-2.03 (version cmd)
T-2.03  →  T-2.04 → T-2.05 (config validate cmd)
T-2.03  →  T-2.06 → T-2.07 (status cmd)
T-1.15  →  T-2.08 → T-2.09 (server internals)
T-1.14  →  T-2.10 → T-2.11 (doctor internals)
T-2.11  →  T-2.12 → T-2.13 (doctor cmd)
T-2.09, T-2.11  →  T-2.14 → T-2.15 (server cmd)
T-2.15  →  T-2.16 → T-2.17 (root wiring)
T-2.17  →  T-2.18 (e2e smoke)

T-1.15  →  T-3.01 → T-3.02 (plcsim + testutil)
T-3.02  →  T-3.03 → T-3.04 (docker files + compose)
T-3.04  →  T-3.05 (README warning)
T-3.04  →  T-3.06 (frontend scaffold)
T-3.06  →  T-3.07 (embed.go + Makefile no_embed)
T-3.02, T-2.17 → T-3.08 → T-3.09 (probe impl)
T-3.07  →  T-3.10 (Makefile targets)

T-3.10  →  T-4.01 (SHA resolution)
T-4.01  →  T-4.02 (golangci.yml + lint)
T-4.02  →  T-4.03 (goreleaser skeleton)
T-4.03  →  T-4.04 (ADRs)
T-4.04  →  T-4.05 (CI workflow updates)
T-4.05  →  T-4.06 (Makefile generate + lint targets)
T-4.06  →  T-4.07 (embed guard comment)
```
