---
change: mvp-foundation
phase: design
date: 2026-05-22
status: draft
roadmap_phase: 0
inputs:
  - openspec/changes/mvp-foundation/proposal.md
  - openspec/changes/mvp-foundation/exploration.md
  - openspec/changes/mvp-foundation/specs/{cli,config,logging,errors,retry,data-dir,doctor}/spec.md
---

# Design: MVP Foundation — Phase 0 Architecture

## 1. Technical Approach (one-paragraph summary)

LGB Phase 0 is a single-binary Go program (`cmd/lgb`) built around a Cobra command tree. A small set of orthogonal `internal/` packages provide the runtime primitives (`config`, `log`, `errors`, `retry`, `datadir`, `doctor`, `version`, `server`, `health`, `httpx`, `testutil`). Everything is wired by composition in `cmd/lgb/cmd/root.go` — no globals, no init-time side effects. A second tiny binary (`cmd/plcsim`) reuses gologix's in-process CIP server so dev and CI can exercise the gateway end-to-end without hardware. All design decisions privilege: (a) pure-Go and `CGO_ENABLED=0` on the four target platforms, (b) stdlib over third-party libraries when stdlib is sufficient, (c) interface-at-the-boundary so library churn never leaks into domain code.

---

## 2. Component Graph (Phase 0)

```
                          ┌───────────────────────────────────────────┐
                          │              cmd/lgb (CLI)                │
                          │                                           │
                          │   main.go → cmd/root.go (Cobra)           │
                          │     ├── server.go ──► RunServer(deps)     │
                          │     ├── doctor.go ──► doctor.Run(ctx,reg) │
                          │     ├── status.go ──► JSON stub           │
                          │     ├── config.go                         │
                          │     │     └── config_validate.go          │
                          │     └── version.go ──► version.Info()     │
                          └───────────────────────────────────────────┘
                                            │ composes
            ┌───────────────────────────────┼───────────────────────────────┐
            ▼                               ▼                               ▼
  ┌──────────────────┐           ┌────────────────────┐           ┌──────────────────┐
  │ internal/config  │           │ internal/datadir   │           │ internal/log     │
  │   Loader (koanf) │           │   Resolve / Ensure │           │   slog setup     │
  │   Watcher        │           └────────────────────┘           │   Redactor wrap  │
  └────────┬─────────┘                     │                       └────────┬─────────┘
           │                                │                                │
           │ uses                           │                                │
           ▼                                ▼                                ▼
  ┌──────────────────┐           ┌────────────────────┐           ┌──────────────────┐
  │ internal/errors  │           │ internal/doctor    │           │ internal/server  │
  │   sentinels      │           │   Check interface  │◄──────────┤   HTTP stub      │
  │   Join helpers   │           │   5 Phase-0 checks │  observes │   /health        │
  └──────────────────┘           └────────────────────┘   ports   │   /metrics (rsvd)│
           ▲                                                       │   graceful shutd │
           │ wrap %w                                               └────────┬─────────┘
           │                                                                │
           │                                                                ▼
  ┌──────────────────┐                                            ┌──────────────────┐
  │ internal/retry   │                                            │ internal/health  │
  │   Do(ctx, opts)  │                                            │   GET handler    │
  └──────────────────┘                                            └──────────────────┘
           ▲
           │ (no Phase-0 consumer — wired in Phase 1)

  ┌──────────────────┐           ┌────────────────────┐           ┌──────────────────┐
  │ internal/version │           │ internal/httpx     │           │ internal/testutil│
  │   ldflags vars   │           │   shutdown helpers │           │   plcsim helpers │
  │   Info() struct  │           │   shared mux/types │           │   cfg builders   │
  └──────────────────┘           └────────────────────┘           └──────────────────┘

  ┌─────────────────────────────────────────────────────────────────────────┐
  │ cmd/plcsim — gologix Server + MapTagProvider on :44818 (deterministic)  │
  └─────────────────────────────────────────────────────────────────────────┘
```

Import-direction rule (enforced by review and by package layout): `cmd/*` may import any `internal/*`. Inside `internal/`, the allowed direction is roughly **leaves → trunk**:

| From → To | Allowed? | Why |
|-----------|----------|-----|
| `config`, `log`, `retry`, `datadir`, `doctor`, `server`, `health`, `version`, `httpx` → `errors` | Yes | sentinel hub |
| `server` → `health`, `httpx` | Yes | composition |
| `config` → `log` | **No** | `log` is initialised from `*Config`, must not loop back |
| `log` → `config` | **No** | `log` exposes `New(level, format string)` and is fed by `cmd/` |
| `doctor` → `config`, `datadir` | Yes | checks read resolved values |
| any `internal/` → `cmd/` | **No** | one-way arrow |
| any `internal/` → `testutil` | **Test files only** (`_test.go`) | keeps test-only deps out of production binary |

---

## 3. `internal/` package layout

| Package | Files | One-line responsibility | Exported surface (Phase 0) |
|---------|-------|--------------------------|-----------------------------|
| `internal/config` | `config.go`, `loader.go`, `watcher.go`, `redact.go`, `*_test.go` | Schema, koanf-backed loader, env overlay, validation, hot-reload, redaction | `type Config struct`, `Load(path string) (*Config, error)`, `Watch(ctx, path, onChange) error`, `(*Config).Redacted() *Config`, sentinels via `internal/errors` |
| `internal/log` | `log.go`, `redact.go`, `*_test.go` | slog initialisation + secret redaction wrapper | `type Options struct{Level, Format string; Out io.Writer}`, `New(opts Options) (*slog.Logger, error)` |
| `internal/errors` | `errors.go`, `*_test.go` | Central sentinel registry, multi-error helper | `ErrConfigInvalid`, `ErrConfigMissing`, `ErrConfigPermission`, `ErrDataDirInvalid`, `ErrDataDirPermission`, `ErrCheckFailed`, `ErrMaxAttempts`; `Join(errs ...error) error` (thin wrapper over `errors.Join`) |
| `internal/retry` | `retry.go`, `*_test.go` | Exponential-backoff primitive | `type Options struct`, `func Do(ctx, opts, fn) error`, `var ErrMaxAttempts` |
| `internal/datadir` | `datadir.go`, `default_unix.go`, `default_darwin.go`, `default_windows.go`, `*_test.go` | Cross-platform resolution + `0700` bootstrap | `Resolve(cfg *config.Config, cliOverride string) (string, error)`, `Ensure(path string) error`, `DefaultPath() string` |
| `internal/doctor` | `doctor.go`, `checks.go`, `*_test.go` | Check registry + Phase-0 checks | `type Check interface`, `type Result struct`, `type Registry struct`, `Run(ctx, reg) []Result`, `Default(cfg *config.Config) *Registry` |
| `internal/version` | `version.go`, `*_test.go` | Build-metadata holder (ldflags) | `var Version, Commit, Date string`; `func Info() InfoStruct` |
| `internal/server` | `server.go`, `*_test.go` | HTTP server stub, router, graceful shutdown | `type Server struct`, `New(cfg *config.Config, log *slog.Logger) *Server`, `(*Server).Run(ctx) error` |
| `internal/health` | `handler.go`, `*_test.go` | `GET /health` returns `{"status":"ok"}` | `func Handler() http.Handler` |
| `internal/httpx` | `shutdown.go`, `mux.go` | Shared HTTP helpers (graceful shutdown, mux constructor) | `func Shutdown(ctx, srv *http.Server, deadline time.Duration) error` |
| `internal/testutil` | `plcsim.go`, `config.go` | Test-only — start in-process plcsim, build minimal configs | `func StartPLCSim(t *testing.T) (addr string, stop func())`, `func MinimalConfig(t *testing.T) *config.Config` |

Platform-specific files use `//go:build` constraints (`_unix.go`, `_darwin.go`, `_windows.go`) — no CGo, no `runtime.GOOS` switches in business logic.

---

## 4. Cross-package interface contracts

### 4.1 `config.Loader` (functions, not a struct interface)

```go
package config

// Config is the canonical typed view of lgb.yaml.
type Config struct {
    Gateway   GatewaySection
    Server    ServerSection
    Auth      AuthSection
    PLCs      []PLC
    OPCUA     OPCUASection
    MQTT      MQTTSection
    Historian HistorianSection
    Backup    BackupSection
}

// Load reads the YAML at path, overlays LGB_* env vars, applies defaults,
// and validates the result. The merge order is fixed:
// defaults → YAML → LGB_* env → CLI overrides applied by caller.
// On success it returns a fully populated *Config.
// On failure it returns an error wrapping one of:
//   ErrConfigMissing | ErrConfigPermission | ErrConfigInvalid
// For ErrConfigInvalid the underlying error is the errors.Join of every
// validation violation (see §6).
func Load(path string) (*Config, error)

// Watch installs koanf's file watcher and invokes onChange whenever the
// file is written. Multiple writes within debounceWindow (≥ 200 ms) are
// coalesced into one callback with the final file state. Watch blocks
// until ctx is Done; it returns ctx.Err() on cancellation.
func Watch(ctx context.Context, path string, onChange func(*Config)) error

// Redacted returns a deep copy where every secret-tagged field is
// replaced with the literal "[redacted]". Used by every code path that
// emits the config to logs or stdout.
func (c *Config) Redacted() *Config
```

Secret tagging is done with a struct tag (`secret:"true"`) interpreted by `(*Config).Redacted()` via reflection — no manual list to maintain.

### 4.2 `retry.Do`

```go
package retry

type Options struct {
    Initial     time.Duration // default 100 ms
    Max         time.Duration // default 30 s
    MaxAttempts int           // 0 = unlimited (until ctx cancels)
    Jitter      float64       // default 0.25; clamped to [0, 1]
}

// Do runs fn until it returns nil, ctx is cancelled, or MaxAttempts is
// reached. Delay after attempt N is min(Initial × 2^(N-1), Max) × (1 ± Jitter).
// Returns: nil on success, ctx.Err() on cancellation,
// fmt.Errorf("retry: %w", ErrMaxAttempts) wrapping last fn error on exhaustion.
func Do(ctx context.Context, opts Options, fn func(context.Context) error) error
```

Pure stdlib. The package depends on `internal/errors` only for `ErrMaxAttempts` — no other internal coupling.

### 4.3 `doctor.Check`

```go
package doctor

type CheckStatus int
const (
    StatusInfo CheckStatus = iota
    StatusPass
    StatusWarn
    StatusFail
)

type Result struct {
    Name    string
    Status  CheckStatus
    Message string
    Took    time.Duration
}

type Check interface {
    Name() string
    Run(ctx context.Context) Result
}

type Registry struct{ /* unexported slice + mutex */ }
func (r *Registry) Register(c Check)
func (r *Registry) Checks() []Check

// Run executes all registered checks concurrently using errgroup.
// Individual panics are recovered into StatusFail results.
func Run(ctx context.Context, r *Registry) []Result

// Default returns a *Registry pre-populated with the 5 Phase-0 checks
// computed from cfg.
func Default(cfg *config.Config) *Registry
```

Each Phase-0 check is a small unexported struct implementing `Check`. Tests register fakes via the same interface — keeps the API contract under test.

### 4.4 `datadir.Resolve` / `Ensure`

```go
package datadir

// Resolve applies the precedence flag > env > yaml > platform-default
// using the *already merged* *config.Config (which incorporates env via
// koanf). The cliOverride argument is the value of --data-dir or "".
func Resolve(cfg *config.Config, cliOverride string) (string, error)

// Ensure resolves "~" / env vars in path, then guarantees it exists as
// a writable directory. Creates with 0700 on POSIX; default ACL on
// Windows. Returns:
//   nil on success
//   ErrDataDirInvalid    if path exists but is not a directory
//   ErrDataDirPermission if creation or write probe fails on perms
//   wrapped *os.PathError otherwise
func Ensure(path string) (resolved string, err error)

// DefaultPath returns the platform-conventional default. Implemented in
// default_unix.go (linux + others), default_darwin.go, default_windows.go.
func DefaultPath() string
```

The writability probe is `os.WriteFile(filepath.Join(path, ".lgb-write-probe"), nil, 0600)` followed by `os.Remove` — works on all four platforms without CGo.

### 4.5 `server.Server`

```go
package server

type Server struct{ /* unexported */ }

func New(cfg *config.Config, log *slog.Logger, checks []doctor.Check) *Server

// Run binds the configured address, mounts /health, /metrics (empty
// prom registry), serves until ctx is cancelled or SIGTERM/SIGINT is
// caught, then calls httpx.Shutdown with cfg.Server.ShutdownTimeout
// (default 10 s). Returns nil on clean shutdown; wraps listen / serve
// errors otherwise.
func (s *Server) Run(ctx context.Context) error
```

Signal handling is wired in `cmd/lgb/cmd/server.go` via `signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)` — keeps `internal/server` test-friendly (no signal machinery inside the package).

---

## 5. Configuration semantics

### 5.1 Koanf provider stack and merge order

The loader assembles koanf in exactly this order; later providers overwrite earlier ones:

| # | Provider | Source | Purpose |
|---|----------|--------|---------|
| 1 | `confmap` | `defaults.go` | Compiled-in defaults for every field |
| 2 | `file` + `yaml.Parser()` | `--config` path | User-edited YAML |
| 3 | `env` (prefix `LGB_`, delim `_`) | process environment | Secret + override overlay |
| 4 | `confmap` | CLI flag overrides | `--data-dir`, `--log-level`, `--log-format` (applied post-merge by caller) |

Step 4 is applied by `cmd/lgb/cmd/root.go` after `config.Load` returns, by directly mutating named fields on the typed `*Config`. This keeps koanf's reflection cost off the hot path and lets Cobra's pflag binding stay vanilla.

The env-name transform is fixed: `LGB_GATEWAY_LOGLEVEL` ↔ `gateway.logLevel`. Koanf's env provider receives a callback that, for each key, lowercases the suffix and reinserts the dots from the schema map. The schema map is generated once from the `Config` struct via reflection at package init — there is no hardcoded list to drift.

### 5.2 Validation

`Config.Validate()` returns `errors.Join(violations...)` so callers see every problem at once. Each violation is a freshly constructed error wrapping `ErrConfigInvalid` via `fmt.Errorf("auth.jwtSecret: required when server is enabled: %w", ErrConfigInvalid)`. This makes `errors.Is(err, ErrConfigInvalid)` true for the joined error AND lets the CLI iterate `errors.Unwrap` for per-violation reporting in `--json`.

### 5.3 Hot-reloadable vs restart-only fields

| Section | Hot-reloadable? | Notes |
|---------|----------------|-------|
| `gateway.logLevel`, `gateway.logFormat` | Yes | Watcher swaps the slog handler atomically |
| `server.httpAddr`, `server.tlsEnabled`, `tlsCert`, `tlsKey` | **No — restart** | Changing the listener cleanly requires a graceful shutdown |
| `auth.jwtSecret`, `auth.sessionTTL` | No (Phase 1 — irrelevant Phase 0) | Token validation key changes break existing sessions |
| `plcs[*]`, `mqtt.*`, `opcua.*` | Owned by Phase-1 changes | Phase 0 watcher just re-validates and logs |
| `historian.*`, `backup.*` | Owned by later changes | Same as above |

In Phase 0 the watcher's only consumer is the logger — when `gateway.logLevel` or `gateway.logFormat` changes, `cmd/lgb/cmd/server.go`'s `onChange` callback re-builds `*slog.Logger` and calls `slog.SetDefault`. Restart-required fields trigger a WARN log: `config: server.httpAddr changed; restart required`.

### 5.4 Secret redaction contract

Every field tagged `secret:"true"` returns `"[redacted]"` from `(*Config).Redacted()`. The set is fixed via struct tags in `config.go`:

```go
type AuthSection struct {
    JwtSecret  string `koanf:"jwtSecret"  secret:"true"`
    SessionTTL string `koanf:"sessionTTL"`
    ...
}
type MQTTSection struct {
    Password     string `koanf:"password"     secret:"true"`
    PasswordFile string `koanf:"passwordFile" secret:"true"`
    ...
}
```

The slog handler is wrapped (`internal/log/redact.go`) with a `slog.Handler` that, on every record, scans for attribute keys named `jwtSecret`, `password`, `passwordFile`, etc., and rewrites the value to `"[redacted]"` before delegating to the underlying handler. This makes accidental `log.Info("config", "cfg", cfg)` safe even if a caller forgets to call `.Redacted()`.

---

## 6. CLI architecture

### 6.1 Cobra command tree

```
lgb (root)
├── version            — prints Info() from internal/version
├── server             — starts the HTTP stub + watcher
├── doctor             — runs doctor.Default(cfg) checks
├── status             — JSON health snapshot stub
└── config             — group
    └── validate       — loads & validates YAML, exits 0/1
```

### 6.2 Shared root flags (registered on root, inherited by every subcommand)

| Flag | Type | Default | Bound to |
|------|------|---------|----------|
| `--config` | string | `lgb.yaml` (cwd) | local var in root |
| `--data-dir` | string | `""` | passed to `datadir.Resolve` post-load |
| `--log-level` | string | `""` | overrides `cfg.Gateway.LogLevel` if non-empty |
| `--log-format` | string | `""` | overrides `cfg.Gateway.LogFormat` if non-empty |
| `--json` | bool | `false` | machine-readable output for `doctor`/`status`/`version` |

### 6.3 Dependency injection between root and subcommands

There are **no package-level globals**. Each subcommand factory returns a `*cobra.Command` built from a `Deps` struct:

```go
type Deps struct {
    ConfigPath string
    DataDir    string
    LogLevel   string
    LogFormat  string
    JSON       bool

    // Late-bound (populated by PersistentPreRunE before subcommand runs):
    Config *config.Config
    Logger *slog.Logger
}

func NewRoot() (*cobra.Command, *Deps)
func NewServerCmd(d *Deps) *cobra.Command
func NewDoctorCmd(d *Deps) *cobra.Command
// … etc.
```

`PersistentPreRunE` on root: loads config, applies CLI flag overrides, builds the logger, populates `Deps`. Subcommands consume `d.Config`, `d.Logger`, never touch koanf directly. This makes every subcommand unit-testable by constructing `Deps` directly and calling `cmd.RunE(cmd, args)`.

### 6.4 Exit-code table (aligned to `sysexits.h`)

| Code | Symbol | Meaning | Examples |
|------|--------|---------|----------|
| 0 | EX_OK | Success | All commands on happy path |
| 1 | EX_USAGE / generic failure | User error or validation failure | `lgb config validate` on bad YAML, `lgb server` with missing `jwtSecret`, doctor with any FAIL |
| 2 | EX_DATAERR | Internal/unexpected error | doctor panicked; config loader returned a non-sentinel wrapped error |
| 64 | EX_USAGE | Cobra usage error (unknown flag, missing required arg) | Cobra's default — surfaced as-is |
| 70 | EX_SOFTWARE (reserved) | Internal software defect — not used in Phase 0 | Reserved for future |
| 77 | EX_NOPERM | Permission denied on dataDir or config | Mapped from `ErrConfigPermission` / `ErrDataDirPermission` |

The mapping table lives in `cmd/lgb/cmd/exit.go` as a single `func exitCode(err error) int` that uses `errors.Is` against the sentinel registry.

### 6.5 `--json` output strategy

Each command that supports `--json` reads `d.JSON` and switches on a single branch at the top of its `RunE`. Both branches share the same data shape (a typed struct); the plain branch uses a small template, the JSON branch uses `encoding/json.NewEncoder(os.Stdout).Encode(payload)`. No formatting library, no `fmt`-based JSON.

---

## 7. Logger design

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Library | `log/slog` stdlib | Spec MVP-FND-4.1 requires it; zero external dep |
| `text` handler | `slog.NewTextHandler(out, &slog.HandlerOptions{Level: lvl})` | Stdlib, human-readable `key=value` |
| `json` handler | `slog.NewJSONHandler(out, &slog.HandlerOptions{Level: lvl})` | Stdlib, machine-readable |
| Output | `os.Stderr` by default | Conventional for daemons; stdout reserved for command output (`status`, `version --json`) |
| Source attachment | `AddSource: lvl == slog.LevelDebug` | File/line in DEBUG only — keeps INFO logs slim and grep-friendly |
| Redaction | Wrap chosen handler with `redactingHandler` | One central place to enforce secret hygiene |
| Global default | `slog.SetDefault(logger)` once in `main.go` after construction | Acceptable for Phase 0 per spec MVP-FND-4.6 |
| Hot reload | Atomic pointer swap on the `*slog.Logger` held in `*Deps`, then `slog.SetDefault` | Race-free because slog handlers are immutable; reassigning the default is one atomic store |

The redactor is a thin `slog.Handler` decorator (~30 LOC). Its set of "secret" attribute keys is derived from the `secret:"true"` struct tags via reflection at logger init — same source of truth as `(*Config).Redacted()`.

---

## 8. Error model

| Aspect | Decision |
|--------|----------|
| Sentinels | Defined in `internal/errors` (central) and re-exported by domain packages for ergonomic call sites (e.g. `config.ErrConfigInvalid = errors.ErrConfigInvalid`) |
| Wrapping | Mandatory `fmt.Errorf("op-noun: %w", err)`; message lowercase, no trailing period (spec MVP-FND-5.2) |
| Multi-error | `errors.Join` for validation aggregation; `errors.Is` traverses joined errors transparently |
| Panic policy | Library code never panics on user/runtime input; `main.go` may convert a top-level error to `os.Exit(N)` after logging |
| Boundary translation | Done in `cmd/lgb/cmd/exit.go` (CLI) and (future) HTTP middleware. `internal/` packages never know about exit codes or HTTP statuses |

Sentinel registry (one place, one file):

```
internal/errors/errors.go
  ErrConfigInvalid, ErrConfigMissing, ErrConfigPermission
  ErrDataDirInvalid, ErrDataDirPermission
  ErrCheckFailed
  ErrMaxAttempts
```

Domain packages alias for ergonomic local use (`var ErrConfigInvalid = errs.ErrConfigInvalid`). Tests in those packages assert via the domain-local name; cross-package callers use either.

---

## 9. dataDir bootstrap

### 9.1 Platform default resolution

| `runtime.GOOS` | File | Default |
|----------------|------|---------|
| `linux`, default | `default_unix.go` (build tag `!darwin && !windows`) | `/var/lib/lgb` |
| `darwin` | `default_darwin.go` | `${HOME}/Library/Application Support/lgb` (uses `os.UserHomeDir()`) |
| `windows` | `default_windows.go` | `%PROGRAMDATA%\lgb` (uses `os.Getenv("PROGRAMDATA")`, fallback `C:\ProgramData\lgb`) |

Build tags keep each implementation OS-pure — no runtime branching, no CGo, no `if runtime.GOOS ==` in business logic.

### 9.2 Bootstrap order in `Ensure`

1. Expand `~` (via `os.UserHomeDir`) and any `${VAR}` references using `os.ExpandEnv`.
2. `filepath.Abs` to absolutise.
3. `os.Stat(resolved)`.
   - Not exist → `os.MkdirAll(resolved, 0700)`. On error → wrap as `ErrDataDirPermission`.
   - Exists & is not a dir → `ErrDataDirInvalid`.
   - Exists & is a dir → continue.
4. Write probe: `os.WriteFile(filepath.Join(resolved, ".lgb-write-probe"), nil, 0600)` + `os.Remove`. On error → `ErrDataDirPermission`.
5. Return absolute resolved path.

### 9.3 Subdirectory layout (reserved, created lazily by later phases)

```
<dataDir>/
├── lgb.db              # historian (later)
├── certs/              # OPC UA / TLS material (later)
├── audit/              # auth audit log (later)
└── backup-tmp/         # `VACUUM INTO` snapshots before restic (later)
```

Phase 0 creates only `<dataDir>` itself.

### 9.4 Failure modes

| Failure | Sentinel | Exit code | User-facing message |
|---------|----------|-----------|---------------------|
| Parent dir not writable | `ErrDataDirPermission` | 77 | `data dir: cannot create {path}: permission denied — run as the owning user or pre-create the directory` |
| Path exists as a file | `ErrDataDirInvalid` | 1 | `data dir: {path} exists but is not a directory` |
| Write probe fails | `ErrDataDirPermission` | 77 | `data dir: {path} is not writable by current user` |

---

## 10. `lgb doctor` checks

| Check name | Status on fail | Implementation note |
|------------|---------------|---------------------|
| `data-dir-writable` | `Fail` | Calls `datadir.Ensure(resolved)` and inspects error |
| `restic-on-path` | `Warn` | `exec.LookPath("restic")`; not fatal — backup may be unused |
| `go-runtime-version` | `Info` | Parses `runtime.Version()`; reports `pass` if ≥ Go 1.24, else `info` (do not fail — informational) |
| `http-port-available` | `Fail` | `net.Listen("tcp", cfg.Server.HttpAddr)` then immediate `Close()`; fails on `EADDRINUSE` |
| `config-loaded` | `Fail` | Constant pass when reached (config was loaded by `PersistentPreRunE`); explicit pass so `doctor` output enumerates the config gate |

That is five Phase-0 checks. The exit-code mapping is:

| Worst result | Exit code |
|--------------|-----------|
| All `Info` / `Pass` | 0 |
| Any `Warn`, no `Fail` | 0 |
| Any `Fail` | 1 |
| Any check panicked (recovered into `Fail`) | 1 |
| Unexpected harness error | 2 |

---

## 11. HTTP server stub

| Element | Decision |
|---------|----------|
| Router | `http.NewServeMux()` — stdlib; Go 1.22's pattern-based routing is sufficient for `/health`, `/metrics`, future `/api/...` |
| `/health` | `internal/health.Handler()` returns `200 OK` with body `{"status":"ok"}` and `Content-Type: application/json` |
| `/metrics` | Returns `200 OK` with body `# empty\n` and `Content-Type: text/plain; version=0.0.4; charset=utf-8`. No `prometheus/client_golang` import yet — kept as a stdlib stub so Phase 0 doesn't pin a metrics library |
| `/readyz` (optional, recommended) | Returns `200` once `Server.Run` has bound the listener; `503` otherwise. Useful for `docker-compose` `healthcheck:`. Added in this Phase as a freebie. |
| Listener | `net.Listen("tcp", cfg.Server.HttpAddr)` — TLS deferred to Phase 1 (`server.tlsEnabled` ignored in Phase 0; warn-log if `true`) |
| Graceful shutdown | `httpx.Shutdown(ctx, srv, 10s)` — `signal.NotifyContext` in `cmd/lgb/cmd/server.go` cancels parent ctx; `Run` returns; main exits 0 |

---

## 12. `cmd/plcsim`

A 40-LOC binary that:

1. Builds a `gologix.MapTagProvider` and seeds a deterministic tag set from `cmd/plcsim/testdata/tags.json` (e.g., `Line1.Temp:REAL=72.5`, `Line1.Running:BOOL=true`, plus arrays and UDT samples).
2. Constructs `gologix.Server` with the provider, calls `Serve(":44818")`.
3. Handles SIGTERM by calling `Server.Close()` (or equivalent) and exits 0.

It is intentionally dumb — no config file, just `--addr` (default `:44818`) and `--tags` (default the embedded testdata). The tag file is **canonical** so docker-compose and unit tests load the same fixture.

The same provider construction lives in `internal/testutil.StartPLCSim(t)` for in-process integration tests, sharing the testdata JSON via `embed.FS`.

---

## 13. Docker dev stack

### 13.1 `docker-compose.dev.yml`

```yaml
services:
  gateway:
    build: { context: ., dockerfile: docker/Dockerfile.dev }
    depends_on:
      plcsim:   { condition: service_healthy }
      mosquitto:{ condition: service_started }
    volumes:
      - ./:/workspace
      - lgb-data:/var/lib/lgb
    ports: ["8080:8080"]
    environment:
      - LGB_AUTH_JWT_SECRET=dev-secret-not-for-prod
    networks: [lgb-dev]

  plcsim:
    build: { context: ., dockerfile: docker/Dockerfile.plcsim }
    ports: ["44818:44818"]
    healthcheck:
      test: ["CMD", "/bin/sh", "-c", "echo > /dev/tcp/localhost/44818"]
      interval: 5s
      retries: 3
    networks: [lgb-dev]

  mosquitto:
    image: eclipse-mosquitto:2
    ports: ["1883:1883"]
    networks: [lgb-dev]

volumes:
  lgb-data:
networks:
  lgb-dev:
```

### 13.2 Multi-stage `docker/Dockerfile`

```Dockerfile
# 1. restic binary
FROM restic/restic:0.18.0 AS restic-bin

# 2. Go build stage
FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ENV CGO_ENABLED=0
RUN go build -ldflags "-s -w -X github.com/fgjcarlos/lgb/internal/version.Version=$(git rev-parse --short HEAD 2>/dev/null || echo dev)" \
    -o /out/lgb ./cmd/lgb

# 3. Final runtime image
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build       /out/lgb       /usr/local/bin/lgb
COPY --from=restic-bin  /usr/bin/restic /usr/local/bin/restic
USER nonroot
ENTRYPOINT ["/usr/local/bin/lgb"]
CMD ["server", "--config", "/etc/lgb/lgb.yaml"]
```

### 13.3 `docker/Dockerfile.dev`

Single-stage `golang:1.24-alpine` with `air` (live-reload) installed and the workspace mounted — only used by `docker-compose.dev.yml`. Not shipped.

### 13.4 `docker/Dockerfile.plcsim`

Same multi-stage build but builds `./cmd/plcsim` instead of `./cmd/lgb`.

---

## 14. CI updates

The existing `.github/workflows/ci.yml` already gates Go steps on a `has_go` boolean detected by file probe. Phase 0 extends this in two ways, mirroring the existing pattern:

```yaml
# In backend-test job, after the existing go test step:
- name: golangci-lint
  if: steps.gocheck.outputs.has_go == 'true'
  uses: golangci/golangci-lint-action@<SHA-v6>  # pinned by SHA, not tag
  with:
    version: v1.63.0
    args: --config=.golangci.yml

# New job:
frontend-build:
  name: Frontend build
  runs-on: ubuntu-latest
  steps:
    - uses: actions/checkout@v4
    - name: Check for frontend
      id: fecheck
      run: |
        if [ -f frontend/package.json ]; then
          echo "has_frontend=true" >> "$GITHUB_OUTPUT"
        else
          echo "has_frontend=false" >> "$GITHUB_OUTPUT"
          echo "::notice::No frontend/package.json — skipping frontend build."
        fi
    - uses: actions/setup-node@v4
      if: steps.fecheck.outputs.has_frontend == 'true'
      with: { node-version-file: 'frontend/.nvmrc' }
    - name: npm ci && build
      if: steps.fecheck.outputs.has_frontend == 'true'
      working-directory: frontend
      run: npm ci && npm run build
```

The `golangci-lint-action` MUST be pinned by SHA (security per supply-chain guidance) — the exact SHA is resolved in `sdd-tasks`.

---

## 15. ADRs

Nine ADRs are authored under `docs/adr/` in **Proposed** status. Bodies belong to `sdd-apply`; titles and IDs are fixed here. A template lives at `docs/adr/0000-template.md`.

| # | Filename | Title |
|---|----------|-------|
| 0001 | `0001-cli-framework.md` | CLI framework — Cobra v1.10.2 |
| 0002 | `0002-config-loader.md` | Config loader — koanf v2 with `LGB_{SECTION}_{FIELD}` env overlay |
| 0003 | `0003-logging.md` | Logging — `log/slog` (stdlib) |
| 0004 | `0004-plc-driver.md` | PLC driver — `danomagnum/gologix` v0.41.0-beta |
| 0005 | `0005-opcua-library.md` | OPC UA library — `gopcua/opcua` v0.8.0 + mandatory Phase 1 spike; `awcullen/opcua` named contingency |
| 0006 | `0006-mqtt-sparkplug.md` | MQTT + Sparkplug B — `paho.mqtt.golang` v1.5.1 + project-owned `internal/sparkplug` |
| 0007 | `0007-historian.md` | Historian — `modernc.org/sqlite` v1.50.1 (pure-Go) |
| 0008 | `0008-backups.md` | Backups — `restic` v0.18.0 as subprocess with `--json` |
| 0009 | `0009-pure-go-no-cgo.md` | Pure-Go / no CGo — all direct deps must compile `CGO_ENABLED=0` on all four targets |

ADR template (single source for body authors):

```markdown
# ADR-{NNNN}: {Title}

**Date**: YYYY-MM-DD
**Status**: Proposed | Accepted | Superseded by ADR-XXXX | Deprecated

## Decision
{One paragraph. Lead with the choice.}

## Context
{Constraints: pure-Go, cross-platform, industrial reliability, etc.}

## Options Considered
| Option | Pros | Cons |
|--------|------|------|
| A | … | … |
| B | … | … |
| C | … | … |

## Rationale
{Why this option given the constraints.}

## Consequences
{Tradeoffs accepted. What to monitor or revisit.}

## References
{Links to issues, specs, benchmarks, vendor docs.}
```

---

## 16. Pure-Go dependency verification

Each direct dependency named in the proposal has been re-verified for `CGO_ENABLED=0` compatibility on all four targets:

| Library | Pure Go? | Verified by |
|---------|----------|-------------|
| `github.com/spf13/cobra` v1.10.2 | Yes | Source inspection — no `cgo` directives; used by k8s/Hugo with CGO_ENABLED=0 |
| `github.com/spf13/pflag` (transitive) | Yes | Cobra dep — pure Go |
| `github.com/knadh/koanf/v2` v2.3.4 | Yes | 99.6% Go per upstream; the `~0.4%` is small assembly in transitive deps (e.g. `mitchellh/copystructure`) — no CGo |
| `koanf/providers/file` v1.x | Yes | Wraps stdlib `os`/`io` + `fsnotify` — fsnotify is pure-Go |
| `koanf/providers/env` v1.x | Yes | Wraps stdlib `os.Environ` |
| `koanf/parsers/yaml` v1.x | Yes | Wraps `gopkg.in/yaml.v3` — pure Go |
| `fsnotify/fsnotify` (koanf transitive) | Yes | Pure Go; uses `inotify`/`kqueue`/`ReadDirectoryChangesW` via syscall, not CGo |
| `gopkg.in/yaml.v3` | Yes | Pure Go |
| `log/slog` (stdlib) | Yes | Stdlib |
| `github.com/danomagnum/gologix` v0.41.0-beta | Yes | Pure Go per upstream README; used only by `cmd/plcsim` and `internal/testutil` in Phase 0 |

Phase 0 does **not** pull `gopcua/opcua`, `paho.mqtt.golang`, `modernc.org/sqlite`, or `golang-jwt/jwt/v5` into `go.mod` — those land with their owning capability changes. ADR-0009 enforces the rule going forward.

**Flagged**: none. All Phase-0 deps are pure Go and compile cleanly with `CGO_ENABLED=0` on linux/{amd64, arm64}, darwin/arm64, windows/amd64.

---

## 17. Testing strategy under strict TDD

Every change to `internal/` or `cmd/` follows RED → GREEN → REFACTOR; the first commit of any work-unit MUST be a failing test.

| Layer | Location | What it tests | Tooling |
|-------|----------|---------------|---------|
| **Unit** | `internal/*/!*_test.go` (same package, `_test.go` files) | Pure functions, structs, error paths — config validation, retry math, datadir resolution logic, doctor result aggregation, redaction | stdlib `testing` |
| **Integration (build-tagged)** | `internal/*_integration_test.go` with `//go:build integration` | Real filesystem (datadir bootstrap), real TCP listen (port-available check, `/health` round-trip), real koanf hot-reload watcher debouncing | stdlib `testing` + `net/http/httptest` + `testutil.StartPLCSim` |
| **CLI smoke** | `cmd/lgb/cmd/*_test.go` | Each subcommand: build `Deps`, call `RunE`, capture stdout/stderr/exit code via `*bytes.Buffer` + an injectable `osExit` function | stdlib `testing` |
| **CLI end-to-end (build-tagged)** | `cmd/lgb/e2e/*_test.go` with `//go:build e2e` | Spawn the built binary in a tmp dir; assert exit codes, stdout JSON shapes, log output. Slow — runs in a dedicated CI job, NOT in `go test ./...` | stdlib `testing` + `os/exec` |

Testdata locations:

| Path | Purpose |
|------|---------|
| `cmd/lgb/testdata/sample.yaml` | Canonical valid config for CLI tests |
| `cmd/lgb/testdata/invalid.yaml` | Multi-violation config for `config validate` |
| `cmd/plcsim/testdata/tags.json` | Deterministic tag set for the simulator |
| `internal/config/testdata/*.yaml` | Loader-level fixtures |

Default `go test ./...` runs unit + CLI smoke only. CI separately runs `go test -tags=integration ./...` and `go test -tags=e2e ./cmd/lgb/e2e/...`.

---

## 18. Non-functional targets (Phase 0 — documented, not yet enforced)

| Metric | Target | Why documented now |
|--------|--------|--------------------|
| Cold boot (`lgb server` to `/health` ready) on Raspberry Pi 4 (linux/arm64) | < 1 s | Edge-deployment commitment; enforces frugal init paths |
| Resident set size idle (no PLCs, no historian) | < 50 MB | Embedded targets; rules out heavy logger/runtime overhead |
| Stripped binary size (`-ldflags="-s -w"`) | < 30 MB | Distribution over slow links; rules out kitchen-sink imports |
| Cold `go build ./...` on CI ubuntu-latest, cache empty | < 60 s | Developer feedback loop; enforces small dep graph |
| `go test ./...` wall-clock on CI ubuntu-latest | < 30 s | Encourages unit test purity over heavy fixtures |

Targets are **observed** in CI starting at archive time (added to a `bench` job) but are not gates yet — failures emit `::warning::` annotations. Phase 1 (`observability`) promotes them to gates once Prometheus instrumentation provides the measurements in production.

---

## 19. Consolidated decisions table (≥15 rows)

| # | Decision | Choice | Alternatives rejected | Rationale |
|---|----------|--------|-----------------------|-----------|
| 1 | CLI framework | Cobra v1.10.2 | urfave/cli v3, stdlib `flag` | Nested `config validate` subcommand; ecosystem (k8s/Hugo/gh) |
| 2 | Config loader | koanf v2.3.4 | viper, stdlib + yaml.v3 | Preserves camelCase keys; modular; per-provider hot-reload |
| 3 | Env overlay naming | `LGB_{SECTION_UPPER}_{FIELD_UPPER}` | Separate `secrets.yaml`, Vault | Single source of truth; ops-friendly; documented in ADR-0002 |
| 4 | Secret tagging | `secret:"true"` struct tag + reflection-driven redactor | Hardcoded key list, naming convention only | Co-located with field; refactor-safe |
| 5 | Logger | `log/slog` stdlib | zerolog, zap | Zero external dep; both text + JSON in stdlib |
| 6 | Source attachment | `AddSource: true` only at DEBUG | Always on, never on | Keeps INFO logs grep-friendly; full detail when debugging |
| 7 | Logger global default | `slog.SetDefault` once at boot + explicit `*Deps.Logger` for tests | Pure DI everywhere, package-level mutables | Pragmatic Phase 0 trade — spec MVP-FND-4.6 accepts it |
| 8 | Error model | stdlib sentinels + `%w` wrapping + `errors.Join` | `pkg/errors`, `cockroachdb/errors` | Stdlib is sufficient since Go 1.20; zero external dep |
| 9 | Sentinel registry | Central `internal/errors` + per-package aliases | Sentinels per package only | Single import for cross-package callers; aliases keep ergonomics |
| 10 | Retry primitive | Pure-stdlib `Do(ctx, opts, fn)` in `internal/retry` | `cenkalti/backoff/v4`, custom per-domain | ~50 LOC; no external dep; ADR-0009 alignment |
| 11 | dataDir defaults | `runtime.GOOS` via `//go:build` files | Single switch statement | OS-pure files; no runtime branching; no CGo |
| 12 | dataDir mode | `0700` POSIX, default ACL on Windows | `0755`, configurable | Defence-in-depth for jwtSecret/historian/audit material |
| 13 | dataDir write probe | Touch-and-remove `.lgb-write-probe` | `unix.Access`, stat-only | Cross-platform; honest "can I really write here" answer |
| 14 | Doctor concurrency | `errgroup`-driven parallel checks with `recover` | Sequential, no recover | Faster; one panicking check cannot mask others |
| 15 | Doctor `restic` status | `Warn` (not `Fail`) | `Fail` | Backup may be unused in dev; spec MVP-FND-8.2 mandates WARN |
| 16 | HTTP router | stdlib `http.NewServeMux` (Go 1.22 pattern routing) | chi, gorilla/mux | Stdlib is enough for 3 routes; deferred decision to Phase 1 REST API |
| 17 | `/metrics` body | Plain `# empty\n` stub | Pull in `client_golang` empty registry | Avoids pinning a metrics library until `observability` change |
| 18 | Graceful shutdown timeout | `10s` default | `5s`, `30s` | Tracks k8s `terminationGracePeriodSeconds` default; configurable |
| 19 | Exit codes | `sysexits.h` aligned: 0/1/2/64/77 | Arbitrary integers | Composable in shell scripts; recognised by humans |
| 20 | `plcsim` | Separate binary using gologix `Server` + `MapTagProvider` | Vendor firmware in Docker, FactoryTalk Logix Echo | Pure Go; deterministic in CI; no licence pain |
| 21 | Docker base | `gcr.io/distroless/static-debian12:nonroot` | `alpine`, `scratch`, `ubuntu` | Smallest cross-arch static base with non-root by default; no shell to attack |
| 22 | restic delivery | `COPY --from=restic/restic:0.18.0` in multi-stage build | apt-get inside Dockerfile, downloading at runtime | Reproducible; pinned; no network at build time after base pull |
| 23 | CI lint gating | `golangci-lint` inside existing `has_go == true` guard | Always run, new dedicated job | Mirrors established `chore/sdd-init-bootstrap` pattern; no behaviour change for empty repos |
| 24 | CI frontend gating | New `frontend-build` job with `has_frontend == true` guard | Add to `backend-test` job | Frontend is a separate concern; failing JS shouldn't block Go feedback |
| 25 | `version` package home | `internal/version` (ldflags target via `-X internal/version.X=...`) | `main.X` in `cmd/lgb/main.go` | Spec MVP-FND-1.7 requires importable from tests; resolves proposal/spec drift |
| 26 | TDD enforcement | Per `openspec/config.yaml` `apply.tdd: true`; first commit of each work-unit is failing test | Suggested TDD | Strict mode mandated by `sdd-init` |
| 27 | Build-tagged integration tests | `//go:build integration` + separate CI job | All-in-one `go test ./...` | Keeps default test suite < 30 s; preserves `go test ./...` ergonomics |

---

## 20. Sequence diagrams

### 20.1 Server boot

```
User              cmd/lgb       config         log         server       OS
 │                  │             │              │            │           │
 │ lgb server ──────▶              │              │            │           │
 │                  │ Load(path) ─▶              │            │           │
 │                  │             │ (defaults    │            │           │
 │                  │             │  → YAML      │            │           │
 │                  │             │  → env)      │            │           │
 │                  │ *Config ◀───┤              │            │           │
 │                  │ apply CLI overrides         │            │           │
 │                  │ log.New(cfg) ────────────▶ │            │           │
 │                  │ *slog.Logger ◀──────────── │            │           │
 │                  │ datadir.Ensure(path) ──────────────────▶ MkdirAll   │
 │                  │ ◀── ok or ErrDataDir*  ────────────────  │           │
 │                  │ server.New(cfg, log).Run(ctx) ─────────▶ │           │
 │                  │                                          │ Listen ──▶│
 │                  │                                          │◀──────────│
 │                  │                                          │ /health ✓ │
 │                  │ (signal.NotifyContext waits)             │           │
 │                  │ ◀────────── SIGTERM ────────────────────│           │
 │                  │ cancel ctx ─────────────────────────────▶│           │
 │                  │                                          │ Shutdown  │
 │                  │ ◀────── nil (clean) ────────────────────│           │
 │ exit 0 ◀─────────│                                          │           │
```

### 20.2 Config hot-reload

```
EditorApp     fsnotify     config.Watcher        Deps         slog
   │              │              │                 │            │
   │ write file ─▶               │                 │            │
   │              │ event ──────▶ (debounce 200ms) │            │
   │ write file ─▶                                 │            │
   │              │ event ──────▶ (coalesced)      │            │
   │                              │ Load(path) ─── │            │
   │                              │ *Config ◀───── │            │
   │                              │ Validate()                  │
   │                              │   err ? log WARN, no swap   │
   │                              │   ok  ? compare to current  │
   │                              │         restart-only? WARN  │
   │                              │         else apply:         │
   │                              │           atomic.Store(cfg) │
   │                              │           if logLevel/Format│
   │                              │             changed →       │
   │                              │             log.New ───────▶│
   │                              │             slog.SetDefault │
```

### 20.3 Retry primitive

```
caller          retry.Do          fn          ctx
  │                │              │            │
  │ Do(ctx,opts,fn)▶               │            │
  │                │ fn(ctx) ─────▶            │
  │                │ ◀── err ──── │            │
  │                │ attempt=1, delay=Initial*(1±J)
  │                │ select { timer | ctx.Done }
  │                │                            │ Done? ──▶ return ctx.Err()
  │                │ fn(ctx) ─────▶            │
  │                │ ◀── nil ─────│            │
  │ ◀── nil ───────│                            │
  │                │
  │ (alternate: MaxAttempts exhausted)
  │                │ fn(ctx) ─────▶ err repeated N times
  │ ◀── fmt.Errorf("retry: %w", ErrMaxAttempts) wrapping last err
```

### 20.4 Doctor `--json`

```
User       cmd/doctor       doctor.Registry      Check1..N      stdout
 │             │                  │                  │            │
 │ lgb doctor --json ▶            │                  │            │
 │             │ Default(cfg) ───▶│                  │            │
 │             │ Run(ctx,reg) ───▶│                  │            │
 │             │                  │ errgroup spawn ─▶│            │
 │             │                  │  (recover per goroutine)      │
 │             │                  │ ◀── Result × N ──│            │
 │             │ ◀── []Result ────│                  │            │
 │             │ classify worst → overall            │            │
 │             │ json.Encode({checks,overall}) ────────────────── ▶
 │             │ exit code = map(worst)                            │
 │ exit 0|1|2 ◀│
```

### 20.5 `plcsim` serve

```
docker compose      plcsim.main      gologix.Server    MapTagProvider
      │                  │                  │                  │
      │ start container ▶│                  │                  │
      │                  │ load testdata/tags.json ──────────▶ │
      │                  │ ◀── seeded map ─────────────────── │
      │                  │ New(Provider) ─▶                   │
      │                  │ Serve(":44818") ────────▶          │
      │ healthcheck      │                                     │
      │ ── TCP probe :44818 ───────────────▶ accept            │
      │ ◀── healthy ─────│                                     │
      │                  │ ◀── SIGTERM ──────────              │
      │                  │ Close() ─────────────▶              │
      │                  │ exit 0                              │
```

---

## 21. File-change summary

| Path | Action | Why |
|------|--------|-----|
| `cmd/lgb/main.go` | Create | ldflags-injected metadata bootstrap; calls `cmd.NewRoot().Execute()` |
| `cmd/lgb/cmd/*.go` | Create | Root + 6 subcommand factories + `exit.go` mapping table |
| `cmd/plcsim/main.go` + `testdata/tags.json` | Create | Deterministic CIP simulator |
| `internal/{config,log,errors,retry,datadir,doctor,version,server,health,httpx,testutil}/*.go` | Create | Phase-0 package skeleton + tests |
| `docs/adr/0000-template.md` + `docs/adr/0001..0009-*.md` | Create | 9 ADRs in `Proposed` status |
| `docker/Dockerfile`, `docker/Dockerfile.dev`, `docker/Dockerfile.plcsim` | Create | Multi-stage builds + restic bundling |
| `docker-compose.dev.yml` | Create | Dev stack: gateway + plcsim + mosquitto |
| `frontend/{package.json,vite.config.ts,tsconfig.json,.nvmrc,src/main.tsx}` | Create | Vite + React + TS scaffold; no UI |
| `embed.go` | Create | `//go:embed all:frontend/dist` directive (guarded for Phase 0 build) |
| `.golangci.yml` | Create | `errcheck`, `staticcheck`, `gosimple`, `unused`, `govet`, `ineffassign` baseline |
| `.goreleaser.yaml` | Create | Skeleton — not wired to CI |
| `Makefile` | Modify (additive) | `lint`, `generate`, `docker-up`, `docker-down`, cross-compile targets |
| `.github/workflows/ci.yml` | Modify (additive) | golangci-lint step + frontend-build job, both behind presence guards |
| `go.mod`, `go.sum` | Modify | Pin Cobra, koanf v2 (+ providers), gologix |

---

## 22. Migration / rollout

Phase 0 is additive. There is no existing functionality to migrate. Each chained-PR slice (see proposal §Delivery Strategy) is independently revertible via `git revert`. No data migration, no feature flags, no destructive deltas.

---

## 23. Open design questions

- [ ] None — every decision needed for `sdd-tasks` is locked in this document. The only outstanding **operational** decision is whether to merge the four chained PRs in the order proposed; that is a delivery-mechanics choice, not a design choice, and is owned by `sdd-tasks` + the maintainer.

---

## 24. Risks (design-level)

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| koanf env provider's reflection of nested keys (e.g. `LGB_BACKUP_REPOS_0_PASSWORD`) drifts from the struct shape | Low | Confusing dev experience | Schema map auto-generated from struct tags at init; covered by unit test that round-trips every secret-tagged field |
| Reflection-driven redaction misses a field added without the `secret:"true"` tag | Medium | Secret leak in logs | Lint rule (custom `analysis.Analyzer` ticketed for Phase 1) + reviewer checklist on ADR-0002; logger handler also blacklists known attribute key names as a belt-and-braces second layer |
| Go 1.22 `ServeMux` pattern routing becomes insufficient for Phase 1 REST API | Medium | Refactor cost when REST lands | Boundary is a single `http.Handler` returned by `internal/server` — swap to chi without touching call sites |
| `gologix` beta surface changes between Phase 0 and `plc-driver` | Medium | Plcsim recompile breakage | Pin exact `v0.41.0-beta`; `internal/testutil` wraps it; production `internal/plc` (Phase 1) defines the long-lived interface |
| `LGB_AUTH_JWT_SECRET=dev-secret-not-for-prod` in `docker-compose.dev.yml` leaks into a real deployment | Low | Auth bypass in prod | Loud warning in README + a doctor check (Phase 1) that flags the dev value as an `ErrCheckFailed` in production builds |
| Frontend embed (`//go:embed all:frontend/dist`) blocks `go build` until the UI is built | Medium | Onboarding friction | Phase 0 wraps the embed in a `//go:build !no_embed` constraint, with `make build` and CI passing `-tags no_embed` until the frontend ships real assets. Tracked in spec MVP-FND-1.10 |
