---
change: sparkplug-b
phase: spec
domain: mqtt
date: 2026-05-23
status: draft
type: new
---

# MQTT Domain Specification

## Purpose

Defines `internal/mqtt/`: the paho.mqtt.golang v1.5.1 wrapper, connection lifecycle, Will Message registration, reconnect behavior with NBIRTH replay, topic namespace, publish API, and QoS/TLS configuration.

---

## Requirements

### [MQTT-1.1] paho client construction — SetOrderMatters(false) mandatory

Every `paho.mqtt.golang` client instance MUST have `SetOrderMatters(false)` called on its `ClientOptions` before the client is created. This is non-negotiable: the default value (`true`) causes paho to block the receive goroutine when waiting for QoS 1/2 acks, which deadlocks in high-throughput scenarios.

#### Scenario: SetOrderMatters is false on every client

- GIVEN the MQTT client is constructed
- WHEN `ClientOptions` is inspected before `NewClient()`
- THEN `OrderMatters` is `false`

#### Scenario: Unit test asserts SetOrderMatters

- GIVEN a unit test for the MQTT client constructor
- WHEN it constructs a client via the wrapper
- THEN it asserts that `SetOrderMatters(false)` was called (via options capture)

---

### [MQTT-1.2] NDEATH as MQTT Will Message

The NDEATH Sparkplug B payload MUST be registered as the MQTT Will Message before the first `Connect()` call. The Will MUST use:

| Property | Value |
|----------|-------|
| Topic | `spBv1.0/{group_id}/NDEATH/{edge_node_id}` |
| QoS | 1 |
| Retained | false |
| Payload | Pre-built NDEATH protobuf binary |

The Will MUST be set via `ClientOptions.SetWill(topic, payload, qos, retained)`. It MUST NOT be set after connection.

#### Scenario: Will is registered before first connect

- GIVEN the MQTT client wrapper is constructed with group and node IDs
- WHEN `ClientOptions` is inspected
- THEN `WillTopic` equals `spBv1.0/{group_id}/NDEATH/{edge_node_id}`
- AND `WillQos` is 1
- AND `WillPayload` is non-empty protobuf bytes

---

### [MQTT-1.3] Custom OnConnect hook — NBIRTH replay

The MQTT client wrapper MUST register a custom `OnConnect` handler via `ClientOptions.SetOnConnectHandler`. On every connect (initial and reconnect), the handler MUST:

1. Publish NBIRTH to `spBv1.0/{group_id}/NBIRTH/{edge_node_id}` at QoS 1.
2. For each currently-connected PLC (state = ONLINE in PLC Manager), publish DBIRTH to `spBv1.0/{group_id}/DBIRTH/{edge_node_id}/{device_id}` at QoS 1.
3. Transition the Sparkplug state machine to ONLINE after NBIRTH and all DBIRTHs are published.
4. MUST NOT publish DDATA until step 3 completes.

#### Scenario: NBIRTH published on initial connect

- GIVEN the MQTT client connects for the first time
- WHEN the OnConnect handler fires
- THEN NBIRTH is published before any DDATA
- AND state machine transitions to ONLINE

#### Scenario: NBIRTH replayed on reconnect

- GIVEN the client was previously ONLINE and drops the connection
- WHEN the client reconnects
- THEN NBIRTH + DBIRTH for all connected PLCs are published again
- AND seq is reset to 0 for NBIRTH

---

### [MQTT-1.4] Project-owned reconnect (no paho auto-reconnect)

The MQTT wrapper MUST disable paho's built-in auto-reconnect (`SetAutoReconnect(false)`). Reconnect behavior MUST be owned by the project's `internal/mqtt` package using a reconnect loop with backoff. This ensures:

1. NBIRTH replay is guaranteed via the custom `OnConnect` hook.
2. Reconnect policy (backoff, max attempts) is under project control.

The reconnect loop MUST respect context cancellation.

#### Scenario: Paho auto-reconnect is disabled

- GIVEN the MQTT client is constructed
- WHEN `ClientOptions` is inspected
- THEN `AutoReconnect` is `false`

#### Scenario: Reconnect loop respects context cancellation

- GIVEN the reconnect loop is running after a disconnect
- WHEN the parent context is cancelled
- THEN the reconnect loop exits without further attempts

---

### [MQTT-1.5] Topic namespace

All topics MUST follow the Sparkplug B v1.0 namespace:

```
spBv1.0/{group_id}/{message_type}/{edge_node_id}[/{device_id}]
```

| Message Type | Topic Pattern |
|-------------|---------------|
| NBIRTH | `spBv1.0/{group_id}/NBIRTH/{edge_node_id}` |
| NDEATH | `spBv1.0/{group_id}/NDEATH/{edge_node_id}` |
| DBIRTH | `spBv1.0/{group_id}/DBIRTH/{edge_node_id}/{device_id}` |
| DDEATH | `spBv1.0/{group_id}/DDEATH/{edge_node_id}/{device_id}` |
| DDATA | `spBv1.0/{group_id}/DDATA/{edge_node_id}/{device_id}` |

`{device_id}` MUST be the PLC `name` field from config. `{group_id}` and `{edge_node_id}` come from `MQTTSection`.

#### Scenario: DDATA topic is constructed correctly

- GIVEN group_id="plant-a", edge_node_id="lgb-1", device_id="press-1"
- WHEN a DDATA message is published for PLC "press-1"
- THEN the topic is `spBv1.0/plant-a/DDATA/lgb-1/press-1`

---

### [MQTT-1.6] QoS, clean session, and TLS-optional

The MQTT client MUST be configurable with:

| Setting | Config field | Default | Constraint |
|---------|-------------|---------|------------|
| QoS | `mqtt.qos` | 1 | 0, 1, or 2 |
| Clean session | `mqtt.cleanSession` | true | bool |
| Keep-alive | `mqtt.keepAlive` | 30s | positive duration |
| TLS | `mqtt.tlsEnabled` | false | bool; certs out of scope |

When TLS is disabled, plain TCP MUST be used. When TLS is enabled, the client MUST use the system CA pool (no custom cert management in Phase 1).

#### Scenario: Default QoS 1 for NBIRTH and DDATA

- GIVEN no explicit QoS in config
- WHEN NBIRTH or DDATA is published
- THEN the publish call uses QoS 1

---

### [MQTT-1.7] Publish API

The MQTT wrapper MUST expose a `Publish(topic string, qos byte, payload []byte) error` method. It MUST:

1. Return `ErrMQTTPublish` (wrapping the underlying error) on paho publish failure.
2. Block until the broker acknowledges (for QoS > 0) or the token times out.
3. MUST NOT be called if the state machine is not ONLINE; return `ErrMQTTConnect` in that case.

#### Scenario: Publish returns ErrMQTTConnect when offline

- GIVEN the state machine is OFFLINE
- WHEN `Publish(topic, 1, payload)` is called
- THEN it returns an error wrapping `ErrMQTTConnect`

#### Scenario: Publish returns ErrMQTTPublish on broker error

- GIVEN the broker rejects the publish
- WHEN `Publish(topic, 1, payload)` is called
- THEN it returns an error wrapping `ErrMQTTPublish`

---

### [MQTT-1.8] CGO_ENABLED=0 compliance

`internal/mqtt/` and its transitive dependencies MUST compile with `CGO_ENABLED=0` on all four target platforms. `paho.mqtt.golang` v1.5.1 is pure-Go.

#### Scenario: Cross-platform build

- GIVEN GOARCH and GOOS are set for each target platform
- WHEN `CGO_ENABLED=0 go build ./internal/mqtt/...` is run
- THEN it succeeds on linux/amd64, linux/arm64, darwin/arm64, windows/amd64
