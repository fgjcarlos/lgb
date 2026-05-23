# Logix Gateway Bridge (LGB)

[![CI](https://github.com/fgjcarlos/lgb/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/fgjcarlos/lgb/actions/workflows/ci.yml)

**LGB is an open source bidirectional gateway between Rockwell ControlLogix / CompactLogix PLCs and modern IIoT standards (OPC UA, MQTT Sparkplug B).**

It exposes PLC tags as OPC UA variables and Sparkplug B metrics, accepts writes from both protocols with explicit ACLs, and provides a modern web UI for tag mapping, diagnostics, backups, and user management.

> The goal is simple: **bridge the proprietary Allen-Bradley CIP world to open IIoT standards, with a usable interface and full operational features — no expensive commercial gateway required.**

---

## Why LGB?

Connecting Rockwell PLCs to modern IIoT systems usually requires expensive commercial gateways (Kepware, Cogent DataHub, Ignition) or fragile Node-RED stacks. LGB focuses on the missing middle:

- Native EtherNet/IP (CIP) communication with ControlLogix and CompactLogix
- Bidirectional OPC UA server with namespace per PLC
- Bidirectional MQTT Sparkplug B Edge Node (one Edge Node, multiple Devices)
- Visual tag mapping with explicit write ACL per tag
- Audit trail for all PLC writes and admin actions
- Local historian with configurable retention
- Built-in backup system with rotation, schedules, and remote repositories (restic)
- Multi-platform: Docker first, native binaries for Linux / Windows / macOS / ARM (Raspberry Pi)

LGB does not implement control logic. The PLC stays in charge of control — LGB is a translator and observability layer.

---

## Project status

**Status: early concept / pre-MVP.**

The repository is being prepared for the first technical milestone. The first target is a usable MVP that bridges up to 10 ControlLogix / CompactLogix PLCs to OPC UA and Sparkplug B with full bidirectional support.

See [ROADMAP.md](./ROADMAP.md) for planned phases. Architecture decisions will be tracked under [docs/adr](./docs/adr/) once the first design phase is complete.

---

## Core principles

- **The PLC stays in charge of control.** LGB is a gateway, not a controller.
- **Bidirectional by design.** Writes from OPC UA and Sparkplug DCMD propagate to the PLC, gated by explicit per-tag ACLs.
- **Open IIoT standards on the gateway side.** OPC UA and MQTT Sparkplug B as published specs, no vendor lock-in on the consumer side.
- **Edge-first.** Pure-Go binary, no CGo, runs on Raspberry Pi as well as on a server.
- **Multi-platform from day one.** Docker first, native binaries for Linux / Windows / macOS / ARM with the same code.
- **Security as a product requirement.** Bcrypt password hashing, JWT sessions, per-tag write ACLs, audit trail, TLS.
- **Industrial-friendly operations.** Clear diagnostics, hot-reload of PLC connections without restart, store-and-forward MQTT, integrity-checked backups.

---

## Target users

- Industrial IoT engineers
- Automation and OT/IT integrators
- SCADA / MES developers who need Rockwell access through open protocols
- Edge computing deployments
- Smart manufacturing and Industry 4.0 projects

---

## Planned MVP scope

The first usable version should stay intentionally focused:

- Up to ~10 ControlLogix / CompactLogix PLCs in a single gateway instance
- EtherNet/IP (CIP) driver via `gologix`, with Multi-Service Packet batching
- OPC UA server (bidirectional) via `gopcua/opcua`
- MQTT Sparkplug B Edge Node (bidirectional) via `eclipse/paho.mqtt.golang` and a project-owned Sparkplug protocol layer
- Visual PLC connection management with hot-reload (add / remove a PLC without restart)
- Tag mapping editor with explicit writable ACL per tag
- Audit trail for writes and administrative actions
- Local historian on SQLite (`modernc.org/sqlite`, pure Go)
- Backup module with `restic`: multi-repo, retention policies, schedules, restore from UI
- First-run admin wizard (Portainer-style) + JWT auth + role-based access (admin / operator / viewer)
- REST API and WebSocket channel for the web UI
- Docker Compose deployment + native binary builds

Non-goals for the MVP:

- Control logic execution (stays in the PLC)
- Legacy PLC families (MicroLogix, SLC500, PLC-5) — modern only for now
- High availability / clustering
- Multi-tenancy
- SSO / LDAP
- Built-in analytics dashboards (the recommended path is consuming Sparkplug or OPC UA from Grafana, Ignition, or similar)

---

## Proposed architecture

```text
            ┌──────────────────────────────────────────┐
            │                Web UI                     │
            │             React + Vite                  │
            └──────────────────┬───────────────────────┘
                               │
                         REST / WebSocket
                               │
            ┌──────────────────▼───────────────────────┐
            │                LGB Core                   │
            │                  Go                       │
            │                                           │
            │   PLC Manager   ↔   Tag Store   ↔   APIs  │
            │   (goroutine                              │
            │    per PLC)                               │
            └─┬──────────┬──────────────┬────────┬────┬─┘
              │          │              │        │    │
              ▼          ▼              ▼        ▼    ▼
       ┌──────────┐  ┌────────┐  ┌──────────┐  ┌───┐ ┌──────────┐
       │ Rockwell │  │  OPC   │  │ Sparkplug│  │   │ │ Restic   │
       │   PLCs   │  │  UA    │  │   B      │  │SQL│ │ backups  │
       │ (CIP via │  │ Server │  │ (paho +  │  │ite│ │ (multi-  │
       │ gologix) │  │        │  │  custom) │  │   │ │  repo)   │
       └──────────┘  └────────┘  └──────────┘  └───┘ └──────────┘
```

---

## Getting started (development stack)

The development stack runs the gateway binary, the PLC simulator, and an MQTT broker locally using Docker Compose.

### Prerequisites

- Docker Engine 24+ with the Compose plugin
- `LGB_AUTH_JWT_SECRET` set in your shell or in a local `.env` file

### Quick start

```sh
# 1. Copy the example env file and supply a value for LGB_AUTH_JWT_SECRET.
#    Do NOT use any placeholder value as a production key.
#    Do NOT commit the filled-in file.
cp docker/.env.dev.example docker/.env.dev
# edit docker/.env.dev and set a real (non-placeholder) value

# 2. Start the stack (gateway + plcsim + mqtt).
make docker-up

# 3. Verify the gateway is healthy.
curl http://localhost:8080/health

# 4. Stop the stack and remove volumes.
make docker-down
```

### Authentication requirement

The dev stack **requires** `LGB_AUTH_JWT_SECRET` to be set before starting. The gateway refuses to start without it. Export the variable in your shell or fill in `docker/.env.dev` (copied from `docker/.env.dev.example`). The example file ships with the variable name only — you must generate the value yourself and never commit a real one.

Generate a suitable value with:

```sh
openssl rand -hex 32
```

---

## License

Apache License 2.0. See [LICENSE](./LICENSE) and [NOTICE](./NOTICE).

LGB is not affiliated with, endorsed by, or sponsored by Rockwell Automation, ODVA, the OPC Foundation, the Eclipse Foundation, or OASIS. See [NOTICE](./NOTICE) for trademark acknowledgements.
