# ADR-0006: MQTT + Sparkplug B — `paho.mqtt.golang` v1.5.1 + project-owned `internal/sparkplug`

**Date**: 2026-05-23
**Status**: Proposed

## Decision

Use `github.com/eclipse/paho.mqtt.golang` v1.5.1 as the MQTT client library. Sparkplug B payload encoding/decoding is owned by a project-internal package `internal/sparkplug` (not a third-party library) to avoid taking a dependency on an unmaintained or non-pure-Go Sparkplug implementation. Phase 0 does NOT import either — this ADR is a forward decision for the `mqtt-sparkplug` change.

## Context

LGB must publish PLC tag data to MQTT brokers using the Sparkplug B specification (a thin framing layer over MQTT using protobuf payloads). The client must be pure-Go and must support QoS 1/2, TLS, and clean session handling. Sparkplug B is a simple protobuf schema — implementing it in-house is feasible and avoids library risk.

## Options Considered

| Option | Pros | Cons |
|--------|------|------|
| `paho.mqtt.golang` v1.5.1 + project `internal/sparkplug` | Pure-Go; Eclipse foundation maintained; widely used in industrial IoT | External MQTT dep; Sparkplug implementation effort (~500 LOC) |
| `paho.mqtt.golang` + `sepaio/sparkplug` | Avoids Sparkplug implementation | `sepaio/sparkplug` uses CGo protobuf; violates ADR-0009 |
| `gorilla/websocket` + custom MQTT | Full control | Full MQTT protocol implementation — not feasible |
| `hivemq/hivemq-mqtt-client-go` | HiveMQ maintained | Less mature; Go bindings are newer |

## Rationale

`paho.mqtt.golang` is the de-facto standard Go MQTT client. Implementing Sparkplug B in-house is straightforward because the payload is defined by a single protobuf schema file (`sparkplug_b.proto`) and the framing rules are well-documented. Using `protoc-gen-go` to generate Go types from the official proto schema is deterministic and avoids a dependency with uncertain CGo posture.

## Consequences

- **Accepted**: `paho.mqtt.golang` as an external dependency (pure-Go). `internal/sparkplug` is project-owned code.
- **Monitor**: paho.mqtt.golang v2 (`github.com/eclipse/paho.golang`) is in development with a cleaner API. Evaluate at `mqtt-sparkplug` change start.
- **Revisit**: If Sparkplug B payload complexity grows beyond Phase 1 scope, consider adopting an upstream Sparkplug Go library if a pure-Go one emerges.

## References

- Upstream paho: https://github.com/eclipse/paho.mqtt.golang
- Sparkplug B specification: https://sparkplug.eclipse.org
- Sparkplug protobuf schema: https://github.com/eclipse/sparkplug/tree/master/sparkplug_b
