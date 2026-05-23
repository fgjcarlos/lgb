# Architecture Decision Records (ADRs)

This directory contains the Architecture Decision Records for the LGB project.

ADRs document significant technical decisions — their context, options considered, rationale, and consequences. Each ADR has a status of **Proposed**, **Accepted**, **Superseded**, or **Deprecated**. Phase 0 ADRs start as Proposed and are promoted to Accepted after team review.

For new ADRs, copy `0000-template.md` and fill in each section. Number sequentially.

## Index

| # | File | Title | Status |
|---|------|-------|--------|
| 0000 | [0000-template.md](0000-template.md) | Template | — |
| 0001 | [0001-cli-framework.md](0001-cli-framework.md) | CLI framework — Cobra v1.10.2 | Proposed |
| 0002 | [0002-config-loader.md](0002-config-loader.md) | Config loader — koanf v2 with `LGB_{SECTION}_{FIELD}` env overlay | Proposed |
| 0003 | [0003-logging.md](0003-logging.md) | Logging — `log/slog` (stdlib) | Proposed |
| 0004 | [0004-plc-driver.md](0004-plc-driver.md) | PLC driver — `danomagnum/gologix` v0.41.0-beta | Proposed |
| 0005 | [0005-opcua-library.md](0005-opcua-library.md) | OPC UA library — `gopcua/opcua` v0.8.0 + mandatory Phase 1 spike | Proposed |
| 0006 | [0006-mqtt-sparkplug.md](0006-mqtt-sparkplug.md) | MQTT + Sparkplug B — `paho.mqtt.golang` v1.5.1 + project-owned `internal/sparkplug` | Proposed |
| 0007 | [0007-historian.md](0007-historian.md) | Historian — `modernc.org/sqlite` v1.50.1 (pure-Go) | Proposed |
| 0008 | [0008-backups.md](0008-backups.md) | Backups — `restic` v0.18.0 as subprocess with `--json` | Proposed |
| 0009 | [0009-pure-go-no-cgo.md](0009-pure-go-no-cgo.md) | Pure-Go / no CGo — all direct deps must compile `CGO_ENABLED=0` | Proposed |

## Adding a new ADR

1. Copy `0000-template.md` to `NNNN-short-title.md` (next sequential number).
2. Fill in all sections. Leave no `{placeholder}` text.
3. Set **Status** to `Proposed`.
4. Add a row to the index table above.
5. Open a PR and tag at least one maintainer for review.
6. On acceptance, update the **Status** field and this index.
