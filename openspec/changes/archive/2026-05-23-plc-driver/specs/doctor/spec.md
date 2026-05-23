---
change: plc-driver
phase: spec
domain: doctor
date: 2026-05-23
status: draft
type: delta
---

# Doctor Domain Delta Specification — plc-driver

## Purpose

Adds a `plc-reachable` diagnostic check to `internal/doctor`. The check performs a TCP dial to each configured PLC address and reports reachability as PASS or FAIL. One result is produced per PLC entry; the check is registered automatically when PLCs are configured.

Base spec: `openspec/specs/doctor/spec.md` (mvp-foundation). All requirements in the base spec remain in force.

---

## Requirements

### [PLC-DOC-1.1] plc-reachable check — one result per configured PLC

The `internal/doctor` package MUST register a `plc-reachable` check for each entry in `config.PLCs`. Each check instance MUST produce a `Result` with:

- `Name`: `"plc-reachable/<plc-name>"` (kebab-case; uses the PLC's `name` field, or the address if `name` is empty)
- `Status`: `StatusPass` if TCP dial succeeds within the timeout; `StatusFail` otherwise
- `Message`: human-readable description including the address and the outcome or error

If `config.PLCs` is empty, no `plc-reachable` check is registered and the doctor output is unaffected.

#### Scenario: Single PLC reachable

- GIVEN a config with one PLC pointing at a running plcsim
- WHEN `lgb doctor` runs
- THEN the result named `"plc-reachable/<name>"` has status `Pass`
- AND the message contains the PLC address

#### Scenario: Single PLC unreachable

- GIVEN a config with one PLC pointing at a non-listening address (`127.0.0.1:19999`)
- WHEN `lgb doctor` runs
- THEN the result named `"plc-reachable/<name>"` has status `Fail`
- AND the message contains the PLC address and the connection error

#### Scenario: No PLCs configured — no plc-reachable result

- GIVEN `config.PLCs` is empty
- WHEN `lgb doctor` runs
- THEN no result with name prefix `"plc-reachable/"` appears in the output

#### Scenario: Multiple PLCs — one result each

- GIVEN a config with two PLC entries (A reachable, B unreachable)
- WHEN `lgb doctor` runs
- THEN two results appear: `"plc-reachable/A"` with status `Pass` and `"plc-reachable/B"` with status `Fail`
- AND exit code is 1 (at least one Fail per MVP-FND-8.3)

---

### [PLC-DOC-1.2] TCP dial — EtherNet/IP port, configurable timeout

The check MUST use `net.DialTimeout("tcp", address, timeout)` where:

- `address` is `config.PLC.Address` (the same address the driver connects to; must include port or default to `:44818` if no port is specified)
- `timeout` is derived from `config.PLC.SocketTimeout` (defaulting to `5s` per PLC-CFG-1.1)

The check MUST NOT establish a CIP session — TCP reachability only. The connection MUST be closed immediately after a successful dial.

Platform note: `net.DialTimeout` is available on all four target platforms without platform-specific code.

#### Scenario: TCP dial respects SocketTimeout

- GIVEN `socketTimeout: "100ms"` and the PLC address is a black-hole (no RST, no accept)
- WHEN `lgb doctor` runs the `plc-reachable` check
- THEN the result appears within ~200 ms (timeout + overhead)
- AND the result status is `Fail`

#### Scenario: Address without port defaults to :44818

- GIVEN a PLC config with `address: "192.168.1.10"` (no port)
- WHEN the check dials
- THEN it dials `"192.168.1.10:44818"`

---

### [PLC-DOC-1.3] Check runs concurrently with other checks

Per MVP-FND-8.6, all doctor checks run concurrently. The `plc-reachable` checks for multiple PLCs MUST also run concurrently with each other — each PLC is an independent check instance, so the existing `sync.WaitGroup`-based runner handles this automatically.

#### Scenario: Two PLC checks run in parallel

- GIVEN two PLC checks each with `socketTimeout: "500ms"` and both addresses unreachable
- WHEN `lgb doctor` runs
- THEN both results appear within ~700 ms (i.e., NOT ~1000 ms — they ran in parallel)

---

### [PLC-DOC-1.4] Registration in doctor.Default()

The `doctor.Default(cfg *config.Config)` function MUST be updated to iterate over `cfg.PLCs` and register one `plcReachableCheck` instance per PLC. The registration MUST happen after the existing Phase-0 checks so the ordering is:

1. `data-dir-writable`
2. `restic-on-path`
3. `go-runtime-version`
4. `http-port-available`
5. `config-loaded`
6. `plc-reachable/<name>` (one per PLC, in config order)

#### Scenario: Default registry includes PLC check when PLCs are configured

- GIVEN `cfg.PLCs` has one entry
- WHEN `doctor.Default(cfg)` is called
- THEN `r.Checks()` has length 6
- AND the sixth check has name `"plc-reachable/<name>"`

---

### [PLC-DOC-1.5] Check result status is FAIL — not WARN

A PLC that is unreachable MUST produce a `StatusFail` result (not `StatusWarn`). A PLC that cannot be reached is a blocking condition for the gateway's primary function. This is distinct from the `restic-on-path` check (backup is optional); PLC connectivity is not optional.

#### Scenario: Unreachable PLC produces Fail — exit code 1

- GIVEN a config with one PLC that is unreachable
- WHEN `lgb doctor` runs
- THEN exit code is 1
- AND the `plc-reachable/<name>` result has status `Fail`
