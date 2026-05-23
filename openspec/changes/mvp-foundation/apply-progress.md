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

## Handoff Notes for Slice 2

- `testutil.MinimalConfig(t)` is ready; T-2.01 can use it immediately
- `config.Load()` is fully functional; `root.go`'s PersistentPreRunE just calls it
- `log.New()` accepts config-derived level/format strings directly
- `datadir.Resolve()` + `datadir.Ensure()` ready for `cmd/lgb/cmd/server.go`
- `internal/errors` sentinels cover all domains; `cmd/lgb/cmd/exit.go` exit code table can reference them via `errors.Is`
- The env key map in `loader.go` uses reflection from the Config struct — adding new fields to Config automatically extends env support (no manual list to maintain)
