---
change: mvp-foundation
domain: data-dir
phase: spec
date: 2026-05-22
status: draft
---

# Data-Dir Specification

## Purpose

Cross-platform data directory resolution, bootstrap, and validation in `internal/datadir/`. Defines resolution priority, platform defaults, creation permissions, and failure modes.

## Requirements

### [MVP-FND-7.1] Resolution order

The data directory MUST be resolved in the following order (highest priority first):

1. `--data-dir` CLI flag
2. `LGB_GATEWAY_DATADIR` environment variable (via koanf overlay)
3. `gateway.dataDir` YAML value
4. Platform-conventional default (see §MVP-FND-7.2)

The resolved path MUST be absolute. Relative paths MUST be resolved against the working directory.

#### Scenario: CLI flag takes precedence over env and config

- GIVEN config `gateway.dataDir: "/var/lib/lgb"` and env `LGB_GATEWAY_DATADIR=/tmp/lgb-env`
- AND CLI flag `--data-dir=/tmp/lgb-flag` is passed
- WHEN `datadir.Resolve(cfg)` is called
- THEN the returned path is `/tmp/lgb-flag`

#### Scenario: Env overrides config when no flag

- GIVEN config `gateway.dataDir: "/var/lib/lgb"` and env `LGB_GATEWAY_DATADIR=/tmp/lgb-env`
- AND no `--data-dir` flag
- WHEN `datadir.Resolve(cfg)` is called
- THEN the returned path is `/tmp/lgb-env`

---

### [MVP-FND-7.2] Platform-conventional defaults

When no explicit value is provided via flag, env, or config, the data directory MUST default to:

| Platform | Default path |
|----------|-------------|
| Linux / Docker | `/var/lib/lgb` |
| macOS (darwin) | `${HOME}/Library/Application Support/lgb` |
| Windows | `%PROGRAMDATA%\lgb` |

Platform note: detection MUST use `runtime.GOOS` at compile time or runtime — no CGo. The `HOME` and `PROGRAMDATA` expansions MUST use `os.UserHomeDir()` / `os.Getenv("PROGRAMDATA")` respectively, not hardcoded paths.

#### Scenario: Linux default is /var/lib/lgb

- GIVEN `GOOS=linux`, no flag, no env var, no config value
- WHEN `datadir.DefaultPath()` is called
- THEN it returns `/var/lib/lgb`

#### Scenario: macOS default uses HOME

- GIVEN `GOOS=darwin`, `HOME=/Users/alice`, no override
- WHEN `datadir.DefaultPath()` is called
- THEN it returns `/Users/alice/Library/Application Support/lgb`

---

### [MVP-FND-7.3] Bootstrap — create with 0700 if missing

The `Ensure(path string) error` function MUST create the directory (including all necessary parents) with permissions `0700` (Linux/macOS) or default ACL (Windows) if it does not exist. It MUST return nil on success. It MUST return `ErrDataDirPermission` if creation fails due to permission denial.

Platform note: `os.MkdirAll(path, 0700)` satisfies this on POSIX; on Windows, `MkdirAll` creates directories with default ACL — the `0700` mode is advisory.

#### Scenario: Missing directory is created

- GIVEN a path that does not exist and the process has permission to create it
- WHEN `datadir.Ensure(path)` is called
- THEN the directory is created
- AND `os.Stat(path)` returns a directory entry
- AND the POSIX mode is `0700` (on Linux/macOS)

#### Scenario: Permission denied returns ErrDataDirPermission

- GIVEN a path whose parent directory is not writable
- WHEN `datadir.Ensure(path)` is called
- THEN `ErrDataDirPermission` is returned (directly or wrapped)
- AND `errors.Is(err, datadir.ErrDataDirPermission)` returns true

---

### [MVP-FND-7.4] Existing path validation

If the path already exists, `Ensure` MUST verify it is a directory (not a file or symlink to a file). If it is not a directory, `Ensure` MUST return `ErrDataDirInvalid`. If it exists and is a directory but is not writable by the running process, `Ensure` MUST return `ErrDataDirPermission`.

#### Scenario: Existing file at path returns ErrDataDirInvalid

- GIVEN a regular file exists at the target path
- WHEN `datadir.Ensure(path)` is called
- THEN `errors.Is(err, datadir.ErrDataDirInvalid)` returns true

#### Scenario: Existing directory is returned successfully

- GIVEN a writable directory already exists at the target path
- WHEN `datadir.Ensure(path)` is called
- THEN it returns nil

---

### [MVP-FND-7.5] Resolved path logged at INFO on startup

When the gateway starts (`lgb server`), it MUST log the resolved data directory path at INFO level with `component="datadir"` and the absolute path value.

#### Scenario: Data directory path is logged at startup

- GIVEN `lgb server` is started with a valid config
- WHEN the server initialises
- THEN the log output contains an INFO entry with `component="datadir"` and the resolved path
