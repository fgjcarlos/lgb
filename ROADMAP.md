# LGB Roadmap

This roadmap describes the product direction for Logix Gateway Bridge.

It is intentionally split into two layers:

1. **Roadmap document**: communicates strategy, phases, and scope.
2. **GitHub Issues**: track concrete work items with acceptance criteria.

A roadmap explains *why and when*; issues explain *what exactly must be done next*.

---

## Roadmap philosophy

LGB should avoid becoming a generic SCADA platform or a clone of commercial gateways. The strongest initial positioning is:

> Bridge Rockwell ControlLogix / CompactLogix PLCs to OPC UA and MQTT Sparkplug B, bidirectionally, with a usable web UI and a clear operational model.

Priorities:

- Build a narrow but usable MVP first.
- Keep the PLC in charge of control logic. LGB never executes control.
- Treat the gateway as a long-running industrial service. Reliability, diagnostics, and audit are first-class concerns.
- Prefer pure-Go dependencies so the same binary runs on Linux, Windows, macOS, and ARM without CGo.
- Delay legacy PLC families, clustering, multi-tenancy, and SSO until the product proves value.

---

## Phase 0 — Project foundation

**Goal:** prepare the repository and define the technical base.

Deliverables:

- Repository layout for Go backend and React frontend
- Architecture Decision Records (ADRs) for the major library choices (CIP driver, OPC UA server, Sparkplug client, historian, backup engine)
- Development Docker Compose stack with one or more emulated PLCs (for example, gologix's test mode or a containerized PLC simulator)
- Makefile / task runner
- CI pipeline for linting, tests, and multi-platform builds
- License decision and trademark acknowledgements
- Contribution guidelines

Exit criteria:

- A new contributor can clone the repository and run the development stack.
- CI validates the repository on every pull request.
- The major library choices are documented as ADRs.

---

## Phase 1 — MVP: bidirectional gateway for up to 10 PLCs

**Goal:** deliver the smallest usable product that bridges up to ~10 ControlLogix / CompactLogix PLCs to OPC UA and Sparkplug B, in both directions.

Scope:

- PLC driver: EtherNet/IP CIP via `gologix`, with Multi-Service Packet batching
- One goroutine per PLC, per-PLC scan rate, in-memory tag store with quality codes
- OPC UA server (bidirectional) with namespace per PLC, security modes None / Sign / Sign-and-Encrypt
- MQTT Sparkplug B Edge Node with Device per PLC, bidirectional commands (DCMD / NCMD), correct birth / death lifecycle
- Per-tag write ACLs configured from the UI
- Audit trail for writes and administrative actions
- Historian on SQLite (`modernc.org/sqlite`) with retention policies per tag
- First-run admin wizard, JWT-based sessions, role-based access (admin / operator / viewer)
- REST API and WebSocket channel for the web UI
- React + Vite + TypeScript frontend
- Hot-reload of PLC connections (add / remove a PLC without restart)
- Docker Compose deployment example
- Native binary builds (Linux amd64 / arm64 first; Windows and macOS shortly after)

Exit criteria:

- An operator can configure up to 10 PLCs and see live tag values from both an OPC UA client and an MQTT Sparkplug consumer.
- Writes from OPC UA and Sparkplug DCMD propagate to the PLC when allowed by the ACL, and are recorded in the audit trail.
- The gateway recovers cleanly from PLC disconnects, MQTT broker disconnects, and OPC UA client churn.

---

## Phase 2 — Operations and backups

**Goal:** make LGB safe to operate day to day.

Scope:

- Backup module with `restic`: multi-repository, retention policies (`keep-hourly`, `keep-daily`, ...), manual and scheduled backups, restore from UI, integrity checks (`restic check`)
- Diagnostics: per-PLC connection health, last scan, CIP error counters, scan latency, MQTT broker health, OPC UA session counts
- Structured logs with redaction of sensitive values
- Configuration validation CLI (`lgb config validate`)
- Doctor command (`lgb doctor`) for common deployment problems

Exit criteria:

- An operator can recover the full gateway state (configuration, historian, audit) from a backup, on a different host.
- Diagnostics surface PLC and broker problems before users notice them.

---

## Phase 3 — Polishing and packaging

**Goal:** ship a real release.

Scope:

- GoReleaser-based release pipeline for amd64 + arm64 on Linux, Windows, macOS, plus multi-arch Docker images
- Documentation site (`docs/`) with deployment guide, OPC UA / Sparkplug usage notes, security baseline, ADR index, backup runbook
- Example integrations: Grafana via Sparkplug B, Ignition via Sparkplug B, generic OPC UA client
- Versioned API contracts and a documented upgrade path

Exit criteria:

- The first tagged release is downloadable and documented well enough for a new operator to deploy without contacting the maintainer.

---

## Beyond the MVP — non-commitments

The following are explicitly not promised, and will only be considered once the MVP has proven value:

- Legacy PLC families (MicroLogix, SLC500, PLC-5) via PCCC
- Other vendor PLCs (Siemens S7, Modbus, OMRON)
- High availability / clustering
- Multi-tenancy
- SSO / LDAP / OIDC integration
- Built-in analytics dashboards
- Embedded historical reporting (the path is to forward data to Grafana, Ignition, or a TimescaleDB / VictoriaMetrics deployment)
