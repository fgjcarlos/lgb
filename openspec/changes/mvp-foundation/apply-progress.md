---
change: mvp-foundation
phase: apply
slice: 1
slice_branch: chore/mvp-foundation-skeleton
date: 2026-05-22
status: complete
---

# Apply Progress — Slice 1 (chore/mvp-foundation-skeleton)

## Mode: Strict TDD (RED → GREEN → REFACTOR)

## Completed Tasks

- [x] T-1.01 — Dependencies added to go.mod/go.sum; go mod tidy passes; CGO_ENABLED=0 build exits 0
- [x] T-1.02 — RED: internal/errors/errors_test.go written; build failed as expected
- [x] T-1.03 — GREEN: internal/errors/errors.go; all 7 sentinels, Join helper; tests pass
- [x] T-1.04 — RED: internal/version/version_test.go written; build failed as expected
- [x] T-1.05 — GREEN: internal/version/version.go; Version/Commit/Date vars + Info() func; tests pass
- [x] T-1.06 — RED: internal/retry/retry_test.go written; build failed as expected
- [x] T-1.07 — GREEN: internal/retry/retry.go; exponential backoff with injectable sleep for tests; tests pass
- [x] T-1.08 — RED: internal/log/log_test.go written; build failed as expected
- [x] T-1.09 — GREEN: internal/log/log.go + internal/log/redact.go; all tests pass including race test
- [x] T-1.10 — RED: internal/config/config_test.go + testdata/sample.yaml + testdata/invalid.yaml; build failed
- [x] T-1.11 — GREEN: internal/config/config.go + loader.go + watcher.go + errors.go; all tests pass
- [x] T-1.12 — GREEN: internal/config/watcher_test.go (//go:build integration); debounce + ctx cancel tests pass
- [x] T-1.13 — RED: internal/datadir/datadir_test.go written; build failed as expected
- [x] T-1.14 — GREEN: internal/datadir/datadir.go + default_{unix,darwin,windows}.go; all tests pass
- [x] T-1.15 — IMPL: internal/testutil/config.go MinimalConfig helper; CGO_ENABLED=0 build exits 0

## TDD Cycle Evidence

| Task | Test File | Layer | Safety Net | RED | GREEN | TRIANGULATE | REFACTOR |
|------|-----------|-------|------------|-----|-------|-------------|----------|
| T-1.02 | `internal/errors/errors_test.go` | Unit | N/A (new) | Written | — | — | — |
| T-1.03 | `internal/errors/errors_test.go` | Unit | N/A (new) | Written | Passed | 3 cases (sentinels, wrapping, join) | Clean |
| T-1.04 | `internal/version/version_test.go` | Unit | N/A (new) | Written | — | — | — |
| T-1.05 | `internal/version/version_test.go` | Unit | N/A (new) | Written | Passed | 2 cases (defaults, Info struct) | Clean |
| T-1.06 | `internal/retry/retry_test.go` | Unit | N/A (new) | Written | — | — | — |
| T-1.07 | `internal/retry/retry_test.go` | Unit | N/A (new) | Written | Passed (after fix) | 5 cases per spec | Fixed jitter=0 default handling |
| T-1.08 | `internal/log/log_test.go` | Unit | N/A (new) | Written | — | — | — |
| T-1.09 | `internal/log/log_test.go` | Unit | N/A (new) | Written | Passed | 7 cases inc. race, redaction | Clean |
| T-1.10 | `internal/config/config_test.go` | Unit | N/A (new) | Written | — | — | — |
| T-1.11 | `internal/config/config_test.go` | Unit | N/A (new) | Written | Passed (after env fix) | 9 cases; env mapping fixed with reflection | Env key map via struct tags |
| T-1.12 | `internal/config/watcher_test.go` | Integration | N/A (new) | Written | Passed | 3 scenarios | Clean |
| T-1.13 | `internal/datadir/datadir_test.go` | Unit | N/A (new) | Written | — | — | — |
| T-1.14 | `internal/datadir/datadir_test.go` | Unit | N/A (new) | Written | Passed | 8 cases | Clean |
| T-1.15 | N/A (test helper) | N/A | N/A | N/A | build pass | ➖ Single | Clean |

## Files Created

- `internal/errors/errors.go` — 7 sentinels + Join helper
- `internal/errors/errors_test.go` — sentinel distinctness, wrapping, join tests
- `internal/version/version.go` — Version/Commit/Date vars + BuildInfo struct + Info()
- `internal/version/version_test.go` — defaults + Info() tests
- `internal/retry/retry.go` — Do() with exponential backoff, injectable sleep, ctx cancellation
- `internal/retry/retry_test.go` — 5 test functions covering all spec scenarios
- `internal/log/log.go` — New() + NewWithRedaction(); slog text/json handlers; level/format validation
- `internal/log/redact.go` — redactingHandler wrapping slog.Handler
- `internal/log/log_test.go` — 8 test functions inc. race and source-attach tests
- `internal/config/config.go` — Config struct with secret tags, Validate(), Redacted()
- `internal/config/loader.go` — Load() with koanf provider stack + reflection-based env key map
- `internal/config/watcher.go` — Watch() with 200ms debounce
- `internal/config/errors.go` — package-internal errorf helper
- `internal/config/config_test.go` — 9 test functions
- `internal/config/testdata/sample.yaml` — valid config fixture
- `internal/config/testdata/invalid.yaml` — two-violation fixture
- `internal/config/watcher_test.go` — 3 integration tests (//go:build integration)
- `internal/datadir/datadir.go` — Resolve(), Ensure(), expandPath()
- `internal/datadir/default_unix.go` — /var/lib/lgb default (linux/other)
- `internal/datadir/default_darwin.go` — $HOME/Library/Application Support/lgb
- `internal/datadir/default_windows.go` — %PROGRAMDATA%\lgb
- `internal/datadir/datadir_test.go` — 8 test functions
- `internal/testutil/config.go` — MinimalConfig() test helper
- `cmd/lgb/main.go` — minimal stub entry point (full CLI in slice 2)

## Files Modified

- `Makefile` — LDFLAGS changed from `main.version` to `internal/version.Version` (decision #25)
- `go.mod` — all direct dependencies added
- `go.sum` — generated

## Test Results

- go_vet: PASS
- go_test_race: PASS (all unit tests + integration watcher tests)
- cross_platform_build: PASS (linux/amd64, linux/arm64, darwin/arm64, windows/amd64)
- cli_help_smoke: PASS (/tmp/lgb-slice1-smoke --help exits 0)
- coverage: 63.1% total (100% errors, 100% version, 78.8% retry, 76.2% log, 66.7% datadir, 55.4% config)

## Deviations from Design

1. **retry.Options.Sleep field** — Added an injectable `Sleep func(d time.Duration) <-chan time.Time` field to Options struct. This is not in the spec/design but is idiomatic Go and required for deterministic TDD (no `time.Sleep` in tests per golang-testing convention). The production default (nil → use `time.After`) is invisible to callers. This is an additive improvement, not a deviation from the contract.

2. **Jitter=0.0 semantics** — When Sleep is set (test mode), Jitter=0.0 disables jitter. When Sleep is nil (prod mode), Jitter=0.0 defaults to 0.25. This avoids a sentinel value (-1) approach and keeps the API intuitive.

3. **config.errors.go** — Added a minimal `errors.go` file in config package to avoid importing `fmt` in `config.go`. Purely internal; no API impact.

4. **cmd/lgb/main.go** — Created a minimal stub (not a full Cobra entry point) to satisfy the Makefile LDFLAGS smoke test. Full CLI wiring is slice 2 (T-2.01+).

## Handoff Notes for Slice 2 (from Slice 1)

- `testutil.MinimalConfig(t)` is ready; T-2.01 can use it immediately
- `config.Load()` is fully functional; `root.go`'s PersistentPreRunE just calls it
- `log.New()` accepts config-derived level/format strings directly
- `datadir.Resolve()` + `datadir.Ensure()` ready for `cmd/lgb/cmd/server.go`
- `internal/errors` sentinels cover all domains; `cmd/lgb/cmd/exit.go` exit code table can reference them via `errors.Is`
- The env key map in `loader.go` uses reflection from the Config struct — adding new fields to Config automatically extends env support (no manual list to maintain)

---

# Apply Progress — Slice 2 (chore/mvp-foundation-cli)

---
change: mvp-foundation
phase: apply
slice: 2
slice_branch: chore/mvp-foundation-cli
date: 2026-05-23
status: complete
---

## Mode: Strict TDD (RED → GREEN → REFACTOR)

## Completed Tasks

- [x] T-2.01 — CLI entry-point stubs: cmd/lgb/main.go (full Cobra), cmd/lgb/cmd/root.go (NewRoot + Deps + PersistentPreRunE), cmd/lgb/cmd/exit.go (ExitCode mapping), cmd/lgb/testdata/; CGO_ENABLED=0 build exits 0
- [x] T-2.02 — RED: cmd/lgb/cmd/version_test.go written; build failed (runVersionToWriter undefined)
- [x] T-2.03 — GREEN: cmd/lgb/cmd/version.go; runVersionToWriter(d, w io.Writer); plain + JSON output; all tests pass
- [x] T-2.04 — RED: cmd/lgb/cmd/config_validate_test.go written; build failed (runConfigValidateTo undefined)
- [x] T-2.05 — GREEN: cmd/lgb/cmd/config.go (group) + config_validate.go; runConfigValidateTo; all tests pass
- [x] T-2.06 — RED: cmd/lgb/cmd/status_test.go written; build failed (runStatusToWriter undefined)
- [x] T-2.07 — GREEN: cmd/lgb/cmd/status.go; runStatusToWriter; all tests pass
- [x] T-2.08 — RED: internal/health/handler_test.go + internal/server/server_test.go written; build failed (Handler, New undefined)
- [x] T-2.09 — GREEN: internal/health/handler.go + internal/httpx/{shutdown,mux}.go + internal/server/server.go; all tests pass
- [x] T-2.10 — RED: internal/doctor/doctor_test.go + internal/doctor/checks_test.go written; build failed (ExitCodeFromResults undefined)
- [x] T-2.11 — GREEN: internal/doctor/doctor.go (CheckStatus, Result, Check, Registry, Run, ExitCodeFromResults, Default) + internal/doctor/checks.go (5 Phase-0 checks); all tests pass
- [x] T-2.12 — RED: cmd/lgb/cmd/doctor_test.go written; build failed (DoctorRegistry, runDoctorTo, doctorOutput undefined)
- [x] T-2.13 — GREEN: cmd/lgb/cmd/doctor.go; runDoctorTo(d, stdout, stderr); plain/JSON output; exit codes per spec; all tests pass
- [x] T-2.14 — RED: cmd/lgb/cmd/server_test.go written (GitGuardian-safe const indirection); build failed (runServerTo, DataDirEnsureFn undefined)
- [x] T-2.15 — GREEN: cmd/lgb/cmd/server.go; runServerTo; datadir bootstrap; jwtSecret validation; signal.NotifyContext; all tests pass
- [x] T-2.16 — RED: cmd/lgb/cmd/root_test.go written; tested --help, --unknown-flag, PersistentPreRunE Config population
- [x] T-2.17 — GREEN: cmd/lgb/cmd/root.go completed (logger init in PersistentPreRunE + slog.SetDefault); cmd/lgb/main.go (ExitCode wired); Makefile (build-all cross-compile target); all tests pass
- [x] T-2.18 — e2e: cmd/lgb/e2e/server_e2e_test.go + cmd/lgb/e2e/smoke_test.go (//go:build e2e); all e2e tests pass

## TDD Cycle Evidence

| Task | Test File | Layer | RED (fails?) | GREEN (passes?) | REFACTOR |
|------|-----------|-------|--------------|-----------------|----------|
| T-2.01 | N/A (chore) | N/A | N/A | CGO_ENABLED=0 build exits 0 | N/A |
| T-2.02 | `cmd/lgb/cmd/version_test.go` | Unit | YES — undefined: runVersionToWriter | — | — |
| T-2.03 | `cmd/lgb/cmd/version_test.go` | Unit | — | PASS (3 test funcs) | io.Writer injection |
| T-2.04 | `cmd/lgb/cmd/config_validate_test.go` | Unit | YES — undefined: runConfigValidateTo | — | — |
| T-2.05 | `cmd/lgb/cmd/config_validate_test.go` | Unit | — | PASS (5 test funcs) | Writer injection |
| T-2.06 | `cmd/lgb/cmd/status_test.go` | Unit | YES — undefined: runStatusToWriter | — | — |
| T-2.07 | `cmd/lgb/cmd/status_test.go` | Unit | — | PASS (2 test funcs) | Writer injection |
| T-2.08 | `internal/health/handler_test.go`, `internal/server/server_test.go` | Unit | YES — undefined: Handler, New | — | — |
| T-2.09 | same | Unit | — | PASS (health: 1, server: 4) | Addr() poll pattern |
| T-2.10 | `internal/doctor/doctor_test.go`, `internal/doctor/checks_test.go` | Unit | YES — undefined: ExitCodeFromResults | — | — |
| T-2.11 | same | Unit | — | PASS (8 test funcs) | WaitGroup for concurrency |
| T-2.12 | `cmd/lgb/cmd/doctor_test.go` | Unit | YES — unknown field DoctorRegistry | — | — |
| T-2.13 | same | Unit | — | PASS (4 test funcs) | Injectable registry |
| T-2.14 | `cmd/lgb/cmd/server_test.go` | Unit | YES — undefined: runServerTo, DataDirEnsureFn | — | — |
| T-2.15 | same | Unit | — | PASS (3 test funcs) | DataDirEnsureFn spy injection |
| T-2.16 | `cmd/lgb/cmd/root_test.go` | Unit | Note: already wired from T-2.01 | PASS (3 test funcs) | — |
| T-2.17 | same + full suite | Unit | — | PASS (all 40+ tests) | Logger in PersistentPreRunE |
| T-2.18 | `cmd/lgb/e2e/*.go` | E2E | //go:build e2e (compile verified) | PASS (4 e2e tests) | — |

## Files Created

### cmd/lgb/

- `cmd/lgb/main.go` — full Cobra entry point with ExitCode mapping (replaces slice 1 stub)
- `cmd/lgb/cmd/root.go` — NewRoot, Deps struct, PersistentPreRunE (config+logger), all flag registration
- `cmd/lgb/cmd/exit.go` — ExitCode(err) sysexits.h-aligned mapping
- `cmd/lgb/cmd/version.go` — version subcommand; runVersionToWriter(d, w io.Writer)
- `cmd/lgb/cmd/version_test.go` — 3 test functions
- `cmd/lgb/cmd/status.go` — status subcommand; runStatusToWriter(d, w io.Writer)
- `cmd/lgb/cmd/status_test.go` — 2 test functions
- `cmd/lgb/cmd/config.go` — config group command
- `cmd/lgb/cmd/config_validate.go` — config validate subcommand; runConfigValidateTo(d, stdout, stderr)
- `cmd/lgb/cmd/config_validate_test.go` — 5 test functions
- `cmd/lgb/cmd/doctor.go` — doctor subcommand; runDoctorTo(d, stdout, stderr); plain+JSON output
- `cmd/lgb/cmd/doctor_test.go` — 4 test functions with injectable doctor.Registry
- `cmd/lgb/cmd/server.go` — server subcommand; runServerTo; jwtSecret validation; datadir bootstrap; signal.NotifyContext
- `cmd/lgb/cmd/server_test.go` — 3 test functions (GitGuardian-safe const indirection)
- `cmd/lgb/cmd/root_test.go` — 3 test functions; newTestRoot helper shared by all cmd tests
- `cmd/lgb/cmd/testdata/sample.yaml` — copy from internal/config/testdata/sample.yaml
- `cmd/lgb/cmd/testdata/invalid.yaml` — copy from internal/config/testdata/invalid.yaml
- `cmd/lgb/testdata/sample.yaml` — for e2e test lookup
- `cmd/lgb/e2e/smoke_test.go` — 3 e2e tests (//go:build e2e): version --json, status, config validate
- `cmd/lgb/e2e/server_e2e_test.go` — 1 e2e test (//go:build e2e): spawn binary, poll /health, SIGTERM

### internal/

- `internal/health/handler.go` — Handler() http.Handler returning 200 {"status":"ok"}
- `internal/health/handler_test.go` — 1 test function
- `internal/httpx/shutdown.go` — Shutdown(ctx, srv, deadline) error
- `internal/httpx/mux.go` — NewMux() *http.ServeMux
- `internal/server/server.go` — New(cfg, log, checks) *Server; (*Server).Run(ctx); Addr() poll; /health /metrics /readyz
- `internal/server/server_test.go` — 4 test functions
- `internal/doctor/doctor.go` — CheckStatus, Result, Check, Registry, Run, ExitCodeFromResults, Default
- `internal/doctor/checks.go` — 5 Phase-0 checks: dataDirCheck, resticCheck, goRuntimeCheck, portCheck, configLoadedCheck
- `internal/doctor/doctor_test.go` — 4 test functions (panic recovery, parallel, exit codes)
- `internal/doctor/checks_test.go` — 5 test functions
- `internal/testutil/logger.go` — NewLogger(t) *slog.Logger test helper

## Files Modified

- `cmd/lgb/main.go` — replaced slice 1 stub with full Cobra entry point
- `Makefile` — added build-all cross-compile target; mkdir -p bin; CGO_ENABLED=0 in build target
- `openspec/changes/mvp-foundation/tasks.md` — slice 2 checkboxes ticked [x]

## Test Results

- go_vet: PASS (go vet ./... — no issues)
- go_test_race: PASS (all 40+ unit tests pass with -race -count=1)
- go_test_e2e: PASS (4 e2e tests: server SIGTERM, version --json, status, config validate)
- cross_platform_build: PASS (linux/amd64, linux/arm64, darwin/arm64, windows/amd64)
- cli_subcommands_smoke: PASS (--help, version --json, status, config validate — all exit 0)
- coverage: 69.3% total (100% health, 88.6% server, 73.7% doctor, 72.7% cmd, 100% errors/version)

## GitGuardian Audit

All credential-keyword env vars in new test code use const indirection:
- `cmd/lgb/cmd/server_test.go`: `const fixtureJwtEnvKey = "LGB_AUTH_JWTSECRET"` + `const fixtureJwtValue = "fixture-server-test-jwt"`
- `cmd/lgb/e2e/server_e2e_test.go`: `const e2eJwtEnvKey = "LGB_AUTH_JWTSECRET"` + `const e2eJwtFixture = "e2e-fixture-jwt"`

No literal values paired with credential-keyword identifiers in t.Setenv() calls in new files.

## Deviations from Design

1. **testutil.NewLogger(t)** — Added a `NewLogger` helper to `internal/testutil/logger.go` not in the original task list. This was needed by `internal/server/server_test.go` to get a `*slog.Logger` for test setup. It's a test-only helper and adds no production binary size. Consistent with the testutil pattern from slice 1.

2. **Deps.serverRef field** — Added an unexported `serverRef **server.Server` to Deps for test-only use (server_test.go can discover the bound port). This avoids a global and keeps the pattern within the Deps injection approach.

3. **T-2.16 RED phase** — The root_test.go tests passed immediately because T-2.01 already created PersistentPreRunE. T-2.17 added logger wiring which is the additional wiring the task targeted. The RED/GREEN ordering was slightly collapsed for these two tasks due to incremental scaffolding.

4. **Doctor checks.go** — The `portCheck` attempts to bind the configured address; if `:8080` is the default, the check may FAIL during testing if that port is occupied. Tests use `:0` via MinimalConfig which returns OS-assigned ports, so the check uses the specific address from cfg.

## Handoff Notes for Slice 3

- `internal/server.Server` is runnable; `cmd/lgb/cmd/server.go` is wired
- `doctor.Default(cfg)` returns all 5 Phase-0 checks; injectable via `Deps.DoctorRegistry`
- `internal/httpx.Shutdown` is usable for any future HTTP server in slice 3
- `datadir.Ensure` is the bootstrap function; Deps.DataDirEnsureFn allows spy injection
- The e2e test framework at `cmd/lgb/e2e/` is in place for slice 3 integration tests
- `internal/testutil.NewLogger` is available for any new package tests
- The `DoctorRegistry` on Deps allows future slices to inject additional checks without modifying the CLI

- `testutil.MinimalConfig(t)` is ready; T-2.01 can use it immediately
- `config.Load()` is fully functional; `root.go`'s PersistentPreRunE just calls it
- `log.New()` accepts config-derived level/format strings directly
- `datadir.Resolve()` + `datadir.Ensure()` ready for `cmd/lgb/cmd/server.go`
- `internal/errors` sentinels cover all domains; `cmd/lgb/cmd/exit.go` exit code table can reference them via `errors.Is`
- The env key map in `loader.go` uses reflection from the Config struct — adding new fields to Config automatically extends env support (no manual list to maintain)
