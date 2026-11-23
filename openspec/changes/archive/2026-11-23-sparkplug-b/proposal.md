# Proposal: Sparkplug B Edge Node

## Intent

The PLC scan loop reads tags but has no publish path — data stays inside the gateway. LGB needs to expose real-time PLC data to SCADA/MES systems via MQTT Sparkplug B (the industry standard for IIoT edge nodes). This change adds the MQTT client, protobuf codegen, Sparkplug B state machine, and the publish pipeline that connects the PLC Manager scan loop to the MQTT broker.

## Scope

### In Scope
- Protobuf codegen: vendor `sparkplug_b.proto`, wire `make generate` with `protoc-gen-go`
- `internal/sparkplug/` package: payload builders, sequence number tracker (0-255 wrapping), metric encoding for Phase 1 scalar types
- MQTT client wrapper (`internal/mqtt/`): paho v1.5.1, `SetOrderMatters(false)`, TLS-optional, NDEATH as Will Message, custom reconnect hook that replays NBIRTH
- Sparkplug B state machine: OFFLINE -> CONNECTING -> ONLINE; NBIRTH/NDEATH/DBIRTH/DDEATH/DDATA lifecycle
- Tag publish callback in PLC Manager scan loop (replace `// Phase 1: no tag store yet` placeholder)
- Config extensions: `GroupID`, `EdgeNodeID`, `QoS`, `KeepAlive`, `CleanSession` in `MQTTSection`; per-PLC `Tags []TagDef` with name + sparkplug type
- Integration tests against Mosquitto from `docker-compose.dev.yml`
- `CGO_ENABLED=0 go build ./...` passes on all four targets

### Out of Scope
- TLS certificate management / mTLS
- Store-and-forward / offline buffering
- NCMD / DCMD write-back to PLCs (read-only edge node for now)
- Sparkplug B Primary Host Application
- OPC UA integration
- UDT / complex type metrics
- Historian integration with MQTT

## Capabilities

### New Capabilities
- `sparkplug`: Sparkplug B protobuf types, payload builders, sequence tracking, state machine, BIRTH/DEATH lifecycle
- `mqtt`: MQTT client wrapper with paho v1.5.1, reconnect hook, Will Message, QoS, publish pipeline

### Modified Capabilities
- `config`: Add Sparkplug fields to `MQTTSection`; add `Tags` to `PLC` struct
- `plc`: Add tag publish callback to scan loop; emit tag reads to a channel/callback for downstream consumers

## Approach

1. **Protobuf first**: vendor `sparkplug_b.proto`, update `make generate` to produce Go types into `internal/sparkplug/pb/`
2. **Bottom-up packages**: `internal/sparkplug/` (payload encoding, seq tracker) -> `internal/mqtt/` (paho wrapper, state machine) -> integration into PLC Manager
3. **Callback decoupling**: PLC Manager scan loop emits `TagUpdate{PLC, Tag, Value, Timestamp}` via a callback/channel; the MQTT publisher consumes these and encodes DDATA payloads
4. **Reconnect safety**: custom `OnConnect` handler in paho publishes NBIRTH + DBIRTH for all connected PLCs; NDEATH is pre-registered as MQTT Will Message before first connect
5. **Sequence number**: single `uint64` atomic counter per edge node, wrapping at 256; survives reconnects (spec requirement)

## Affected Areas

| Area | Impact | Description |
|------|--------|-------------|
| `internal/sparkplug/pb/` | New | Generated protobuf Go types |
| `internal/sparkplug/` | New | Payload builders, seq tracker, state machine |
| `internal/mqtt/` | New | paho wrapper, client lifecycle, publish API |
| `internal/config/config.go` | Modified | Extended MQTTSection + PLC.Tags |
| `internal/plc/manager.go` | Modified | Tag read + callback in scan loop |
| `Makefile` | Modified | `generate` target updated for sparkplug proto |
| `go.mod` | Modified | Add paho.mqtt.golang, google.golang.org/protobuf |

## Risks

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| paho v1 reconnect does NOT replay NBIRTH | High | Custom `OnConnect` hook; do NOT rely on auto-reconnect alone |
| `SetOrderMatters(false)` omission causes deadlock | High | Set unconditionally in client constructor; unit test asserts it |
| Protobuf codegen needs `protoc` + `protoc-gen-go` in CI | Med | Document in Makefile; add CI step; `buf` as future alternative |
| NBIRTH race between connect callback and first DDATA | Med | State machine gates DDATA until NBIRTH+DBIRTH are acked (QoS 1) |
| Sequence number overflow/reset on reconnect | Low | Spec says seq resets to 0 on NBIRTH; track per edge node atomically |
| PLC scan loop perf regression from callback overhead | Low | Channel with buffer; publisher goroutine is independent |

## Rollback Plan

1. Revert the feature branch (no main-branch changes until merged)
2. PLC Manager scan loop returns to no-op placeholder — existing functionality unaffected
3. Remove `internal/sparkplug/` and `internal/mqtt/` packages
4. Remove paho + protobuf deps from `go.mod`; revert config extensions
5. `make generate` falls back to "no .proto files" path (existing behavior)

## Dependencies

- `github.com/eclipse/paho.mqtt.golang` v1.5.1 (pure-Go, per ADR-0006)
- `google.golang.org/protobuf` (pure-Go protobuf runtime)
- `protoc` + `protoc-gen-go` as build-time tools (not runtime deps)
- Eclipse Sparkplug B proto schema (vendored, not a Go dependency)
- Mosquitto broker in `docker-compose.dev.yml` (already present)

## Success Criteria

- [ ] Sparkplug B state machine passes unit tests for all state transitions
- [ ] NBIRTH/NDEATH/DBIRTH/DDATA payloads are valid protobuf per sparkplug_b.proto
- [ ] `SetOrderMatters(false)` is set on every paho client instantiation
- [ ] Custom reconnect hook replays NBIRTH + DBIRTH on every reconnect
- [ ] PLC scan loop reads tags and publishes DDATA to MQTT broker
- [ ] Integration tests connect to Mosquitto, publish NBIRTH, verify payload
- [ ] `CGO_ENABLED=0 go build ./...` passes on linux/amd64, linux/arm64, darwin/arm64, windows/amd64
- [ ] `make generate` produces Go types from `sparkplug_b.proto`
- [ ] `go test ./... -race -count=1` passes with no data races
- [ ] Sequence numbers wrap correctly at 256
