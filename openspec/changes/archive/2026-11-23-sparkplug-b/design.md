---
change: sparkplug-b
phase: design
date: 2026-05-23
status: draft
inputs:
  - openspec/changes/sparkplug-b/proposal.md
  - openspec/changes/sparkplug-b/specs/sparkplug/spec.md
  - docs/adr/0006-mqtt-sparkplug.md
---

# Design: Sparkplug B Edge Node

## 1. Technical Approach

Three new packages provide the publish pipeline from PLC tags to MQTT broker:

1. `internal/sparkplug/` -- project-owned Sparkplug B state machine, payload builders (NBIRTH/NDEATH/DBIRTH/DDEATH/DDATA), sequence tracker, and metric encoding for Phase 1 scalars. Generated protobuf types live in `internal/sparkplug/pb/`.
2. `internal/mqtt/` -- thin wrapper around `paho.mqtt.golang` v1.5.1. Exposes a `Client` interface that isolates paho types at the package boundary (same pattern as gologix in `internal/plc/`). Sets `SetOrderMatters(false)` unconditionally. Manages NDEATH Will Message, custom `OnConnect` hook for NBIRTH/DBIRTH replay, and reconnect lifecycle.
3. PLC Manager modification -- the scan loop in `manager.go:251-266` replaces its no-op heartbeat with tag reads via `Driver.ReadTag()`. Tag updates are emitted to a callback (`TagCallback`) that the Sparkplug edge node consumes.

Data flows bottom-up: PLC scan tick -> `Driver.ReadTag` -> `TagCallback` -> `sparkplug.EdgeNode.HandleTagUpdate` -> encode DDATA -> `mqtt.Client.Publish`.

---

## 2. Package Layout

```
internal/sparkplug/
  pb/
    sparkplug_b.pb.go     -- generated protobuf types (committed)
  sparkplug_b.proto       -- vendored schema (source of truth)
  seq.go                  -- SeqTracker (atomic uint64 mod 256)
  state.go                -- StateMachine (OFFLINE/CONNECTING/ONLINE)
  payload.go              -- BuildNBIRTH, BuildNDEATH, BuildDBIRTH, BuildDDATA, BuildDDEATH
  metric.go               -- GoToSparkplugType mapping, metric encoding
  edge_node.go            -- EdgeNode: orchestrates state + seq + payloads + publish
  doc.go                  -- package doc
  seq_test.go
  state_test.go
  payload_test.go
  metric_test.go
  edge_node_test.go

internal/mqtt/
  client.go               -- Client interface + pahoClient adapter
  options.go              -- Options struct (broker URL, client ID, QoS, etc.)
  doc.go                  -- package doc
  client_test.go
  client_integration_test.go  -- //go:build integration; uses Mosquitto

proto/
  sparkplug_b.proto       -- canonical proto location for make generate
```

---

## 3. Component Diagram

```
  cmd/lgb/cmd/server.go
        |
        | composes SparkplugNode + PLCManager
        v
  internal/server.Server
        |
        | .Run(ctx) starts/stops sparkplugNode then plcMgr
        v
  internal/sparkplug.EdgeNode -------> internal/mqtt.Client
        |  (state machine,                  |
        |   payload builders,               | wraps
        |   seq tracker)                    v
        |                            pahoClient adapter
  TagCallback <-----+                      |
        |           |                      | delegates to
        v           |                      v
  internal/plc.Manager ------> []plc.Driver   paho.mqtt.golang
        |                          |               |
        | scan loop ticks          | wraps         | .Connect()
        | Driver.ReadTag()         v               | .Publish()
        |                   gologixDriver          | .Disconnect()
        |                          |
        v                          v
  internal/config.Config     *gologix.Client
  internal/errors
```

Import direction (enforced):

| From | To | Allowed? |
|------|----|----------|
| `sparkplug` | `mqtt`, `config`, `errors` | Yes |
| `sparkplug` | `plc`, `server`, `cmd` | **No** |
| `mqtt` | `config`, `errors` | Yes |
| `mqtt` | `sparkplug`, `plc`, `server` | **No** |
| `plc` | `config`, `errors`, `retry` | Yes |
| `plc` | `sparkplug`, `mqtt` | **No** |
| `server` | `sparkplug`, `plc`, `mqtt` | Yes (via interfaces) |
| any non-mqtt `internal/` | `paho.mqtt.golang` | **No** |

The `plc` package knows nothing about Sparkplug or MQTT. The wiring happens in `server.go` or `cmd/server.go` via a callback that bridges the packages.

---

## 4. Interface Definitions

```go
// --- internal/mqtt/client.go ---

package mqtt

// Client is the boundary interface for MQTT operations.
// Implementations MUST NOT expose paho types outside this package.
type Client interface {
    // Connect establishes the MQTT connection. Will Message (NDEATH) must
    // be configured via Options before Connect is called.
    Connect(ctx context.Context) error

    // Disconnect gracefully disconnects with the given quiesce period.
    Disconnect(quiesce uint) error

    // Publish sends a message. topic is the full Sparkplug topic string.
    // payload is pre-serialized protobuf bytes. qos is 0 or 1.
    Publish(ctx context.Context, topic string, qos byte, retained bool, payload []byte) error

    // IsConnected returns true if the MQTT session is active.
    IsConnected() bool

    // SetOnConnect registers a callback invoked on every (re)connect.
    // The EdgeNode uses this to replay NBIRTH + DBIRTH.
    SetOnConnect(fn func())
}

// Options configures the MQTT client.
type Options struct {
    BrokerURL    string
    ClientID     string
    Username     string
    Password     string
    QoS          byte          // default 0
    KeepAlive    time.Duration // default 30s
    CleanSession bool          // default true
    WillTopic    string        // NDEATH topic
    WillPayload  []byte        // pre-serialized NDEATH protobuf
    WillQoS      byte          // default 1
    WillRetain   bool          // default false
}
```

```go
// --- internal/sparkplug/edge_node.go ---

package sparkplug

// EdgeNode orchestrates the Sparkplug B lifecycle: state machine,
// sequence tracking, payload building, and MQTT publishing.
type EdgeNode struct { /* unexported */ }

// EdgeNodeConfig configures the edge node.
type EdgeNodeConfig struct {
    GroupID    string
    NodeID    string
    Client    mqtt.Client        // injected dependency
    Devices   []DeviceConfig     // one per PLC
    Log       *slog.Logger
}

// DeviceConfig maps a PLC name to its tag definitions.
type DeviceConfig struct {
    DeviceID string
    Tags     []TagDef
}

// TagDef defines a single Sparkplug metric tied to a PLC tag.
type TagDef struct {
    Name         string // Sparkplug metric name
    PLCTag       string // PLC tag address (e.g. "Motor.Speed")
    SparkplugType uint32 // Sparkplug B DataType enum value
}

func NewEdgeNode(cfg EdgeNodeConfig) *EdgeNode

// Start connects MQTT, publishes NBIRTH + DBIRTH for all devices, and
// transitions to ONLINE. NDEATH is registered as Will before Connect.
func (e *EdgeNode) Start(ctx context.Context) error

// Stop publishes DDEATH for each device, disconnects MQTT, and
// transitions to OFFLINE.
func (e *EdgeNode) Stop() error

// HandleTagUpdate is the callback invoked by the PLC Manager scan loop.
// It encodes the update as DDATA and publishes if the state is ONLINE.
// If not ONLINE, the update is dropped (logged at DEBUG).
func (e *EdgeNode) HandleTagUpdate(update TagUpdate)
```

```go
// --- internal/plc/manager.go (additions) ---

package plc

// TagUpdate represents a single tag read from a PLC scan tick.
type TagUpdate struct {
    PLCName   string
    Tag       string
    Value     any
    Timestamp time.Time
}

// TagCallback is the function signature for the PLC scan loop to emit
// tag reads. The Manager calls this for every successful tag read.
type TagCallback func(update TagUpdate)
```

```go
// --- internal/server/server.go (additions) ---

// SparkplugNode is the interface the Sparkplug edge node must satisfy
// for server lifecycle integration. Same pattern as PLCManager.
type SparkplugNode interface {
    Start(ctx context.Context) error
    Stop() error
}
```

---

## 5. Architecture Decisions

| # | Decision | Choice | Rejected alternatives | Rationale |
|---|----------|--------|-----------------------|-----------|
| 1 | MQTT client library | paho.mqtt.golang v1.5.1 | paho.golang v2 (alpha); hivemq-mqtt-client-go | v1.5.1 is stable, pure-Go, Eclipse-maintained; v2 API is cleaner but still alpha-quality with breaking changes; per ADR-0006 |
| 2 | SetOrderMatters(false) | Set unconditionally in pahoClient constructor; no config toggle | Allow user to enable ordering | Prevents known deadlock in paho v1 internal message router; no valid use case to re-enable; unit test asserts it |
| 3 | Sparkplug B implementation | Project-owned `internal/sparkplug/` | `sepaio/sparkplug` (CGo protobuf); `amimof/sparkplugb` | Single proto schema (~50 messages); ~500 LOC to implement; avoids CGo and unmaintained deps; per ADR-0006 |
| 4 | Tag update delivery | `TagCallback func(TagUpdate)` passed to Manager via constructor | Channel (`chan TagUpdate`); observer pattern with registration | Callback is zero-allocation, no buffer sizing decisions, matches Manager's single-goroutine-per-PLC model; callback runs in scan goroutine so must be non-blocking (EdgeNode buffers internally) |
| 5 | Reconnect strategy | Custom `OnConnect` hook via `paho.ClientOptions.SetOnConnectHandler`; replays NBIRTH + DBIRTH | Rely on paho auto-reconnect only; custom reconnect loop replacing paho's | paho auto-reconnect re-establishes TCP+MQTT but does NOT replay application payloads; custom hook is the documented pattern; we keep paho's AutoReconnect=true for TCP recovery |
| 6 | Protobuf codegen | Commit generated `*.pb.go` files; `make generate` runs `protoc --go_out` | CI-only generation; buf.build | Committed files mean `go build` works without protoc installed; `make generate` regenerates on schema change; buf is future work |
| 7 | Sequence number | `atomic.Uint64` counter, `Next()` returns `uint64(counter % 256)`, `Reset()` stores 0 | `sync.Mutex` + plain int; per-device counters | Sparkplug spec: one seq counter per edge node (NOT per device); atomic is lock-free and race-free; uint64 avoids overflow for trillions of messages |
| 8 | NBIRTH ordering | State machine gates DDATA until NBIRTH+DBIRTH sequence completes; OnConnect callback publishes NBIRTH(seq=0) then DBIRTH for each device synchronously before returning | Allow DDATA during CONNECTING; async BIRTH publishing | Sparkplug spec mandates NBIRTH before any DDATA; synchronous in OnConnect guarantees ordering because paho serializes publishes in callback context |
| 9 | NDEATH registration | Build NDEATH payload (with bdSeq metric) at startup; pass as Will Message in `paho.ClientOptions.SetWill` before first Connect; bdSeq increments on each reconnect | Publish NDEATH manually on disconnect | MQTT Will is broker-managed -- fires even on ungraceful disconnect (process crash, network drop); manual publish only covers graceful shutdown |
| 10 | Topic namespace | `spBv1.0/{group_id}/N{verb}/{edge_node_id}` for node; `spBv1.0/{group_id}/D{verb}/{edge_node_id}/{device_id}` for device | Custom topic scheme | Sparkplug B specification mandates this exact topic structure |
| 11 | EdgeNode internal buffering | Unbuffered channel + single publisher goroutine in EdgeNode; `HandleTagUpdate` sends to channel, publisher goroutine drains and encodes DDATA | Blocking callback directly to MQTT; ring buffer | Single publisher goroutine serializes all publishes naturally; channel decouples scan loop timing from MQTT latency; if channel full, drop update and log WARN (back-pressure signal) |
| 12 | Config extensions | Add Sparkplug fields to existing `MQTTSection`; add `Tags []TagDef` to `PLC` struct | Separate SparkplugSection top-level struct | Sparkplug IS the MQTT publish path -- putting GroupID/EdgeNodeID in MQTTSection keeps config locality; Tags belong to PLC because they define what to read |

---

## 6. Sequence Diagrams

### 6.1 Initial Connection: NDEATH (Will) + NBIRTH + DBIRTH

```
server.Run     EdgeNode       mqtt.Client(paho)    Broker
    |               |               |                 |
    | Start(ctx) -->|               |                 |
    |               | build NDEATH payload            |
    |               | SetWill(NDEATH)                 |
    |               | Connect(ctx) ->|                |
    |               |               | TCP+MQTT ------>|
    |               |               | Will registered |
    |               |               |<-- CONNACK -----|
    |               |               |                 |
    |               | [OnConnect callback fires]      |
    |               | state = CONNECTING              |
    |               | seq.Reset()                     |
    |               | BuildNBIRTH(seq=0)              |
    |               | Publish(NBIRTH) ->              |
    |               |               | NBIRTH -------->|
    |               |               |                 |
    |               | for each PLC:                   |
    |               |   BuildDBIRTH(seq)              |
    |               |   Publish(DBIRTH) ->            |
    |               |               | DBIRTH -------->|
    |               |               |                 |
    |               | state = ONLINE                  |
    |<-- nil -------|               |                 |
```

### 6.2 Scan Tick -> DDATA Publish

```
ticker       Manager.runWorker   Driver    TagCallback    EdgeNode      mqtt.Client
  |               |                 |           |             |              |
  | tick -------->|                 |           |             |              |
  |               | ReadTag(t1) -->|           |             |              |
  |               |<-- val --------|           |             |              |
  |               | callback(TagUpdate) ------>|             |              |
  |               |                 |           | send to ch->|              |
  |               | ReadTag(t2) -->|           |             |              |
  |               |<-- val --------|           |             |              |
  |               | callback(TagUpdate) ------>|             |              |
  |               |                 |           |             | [publisher goroutine]
  |               |                 |           |             | drain channel |
  |               |                 |           |             | BuildDDATA(updates)
  |               |                 |           |             | Publish() -->|
  |               |                 |           |             |              | DDATA ->broker
```

### 6.3 Disconnect -> NDEATH -> Reconnect -> NBIRTH Replay

```
Broker         mqtt.Client(paho)    EdgeNode            SeqTracker
  |                  |                  |                    |
  | [TCP drops]      |                  |                    |
  | Will fires:      |                  |                    |
  | NDEATH broadcast |                  |                    |
  |                  |                  |                    |
  |                  | [paho auto-reconnect kicks in]        |
  |                  | TCP+MQTT ------->|                    |
  |                  |<-- CONNACK ------|                    |
  |                  |                  |                    |
  |                  | [OnConnect fires]|                    |
  |                  |                  | state = CONNECTING |
  |                  |                  | bdSeq++            |
  |                  |                  | seq.Reset() ------>|
  |                  |                  |<-- 0 --------------|
  |                  |                  | BuildNBIRTH(seq=0) |
  |                  | <-- Publish(NBIRTH)                   |
  | NBIRTH <---------|                  |                    |
  |                  |                  | for each PLC:      |
  |                  |                  | BuildDBIRTH(seq)   |
  |                  | <-- Publish(DBIRTH)                   |
  | DBIRTH <---------|                  |                    |
  |                  |                  | state = ONLINE     |
```

### 6.4 Hot-Reload Config Change

```
config.Watcher   server.onChange   EdgeNode          Manager
     |                |               |                |
     | change ------->|               |                |
     |                | Stop() ------>|                |
     |                |               | DDEATH per PLC |
     |                |               | Disconnect()   |
     |                |               | state=OFFLINE  |
     |                |<-- nil -------|                |
     |                | Reload(cfg) --|--------------->|
     |                |               |                | drain old, start new
     |                |<-- nil -------|----------------|
     |                | reconfigure EdgeNode with new PLCs/tags
     |                | Start(ctx) -->|                |
     |                |               | reconnect, NBIRTH, DBIRTH
     |                |<-- nil -------|                |
```

---

## 7. Config Schema Additions

```go
// MQTTSection holds MQTT broker and Sparkplug B settings.
type MQTTSection struct {
    BrokerURL    string `koanf:"brokerURL"`
    ClientID     string `koanf:"clientID"`
    Username     string `koanf:"username"`
    Password     string `koanf:"password"     secret:"true"`
    PasswordFile string `koanf:"passwordFile" secret:"true"`
    GroupID      string `koanf:"groupID"`      // Sparkplug B group ID
    EdgeNodeID   string `koanf:"edgeNodeID"`   // Sparkplug B edge node ID
    QoS          int    `koanf:"qos"`          // 0 or 1; default 0
    KeepAlive    string `koanf:"keepAlive"`    // Go duration; default "30s"
    CleanSession *bool  `koanf:"cleanSession"` // pointer for default-true detection
}

// PLC gains a Tags field for Sparkplug metric mapping.
type PLC struct {
    Name          string   `koanf:"name"`
    Address       string   `koanf:"address"`
    Slot          int      `koanf:"slot"`
    SocketTimeout string   `koanf:"socketTimeout"`
    ScanRate      string   `koanf:"scanRate"`
    KeepAlive     bool     `koanf:"keepAlive"`
    Path          string   `koanf:"path"`
    Tags          []TagDef `koanf:"tags"`        // NEW: Sparkplug tag mappings
}

// TagDef defines a PLC tag to Sparkplug metric mapping.
type TagDef struct {
    Name          string `koanf:"name"`          // Sparkplug metric name
    Address       string `koanf:"address"`       // PLC tag address
    SparkplugType string `koanf:"sparkplugType"` // e.g. "Float", "Int32", "Boolean"
}
```

Validation additions to `Config.Validate()`:

- `mqtt.brokerURL`: MUST be non-empty when `mqtt.groupID` is set
- `mqtt.groupID`: MUST be non-empty when any PLC has tags configured
- `mqtt.edgeNodeID`: MUST be non-empty when `mqtt.groupID` is set
- `mqtt.qos`: MUST be 0 or 1
- `mqtt.keepAlive`: MUST parse as valid `time.Duration` when non-empty
- `plcs[i].tags[j].name`: MUST be non-empty
- `plcs[i].tags[j].address`: MUST be non-empty
- `plcs[i].tags[j].sparkplugType`: MUST be one of the Phase 1 supported types

---

## 8. Error Model

New sentinels in `internal/errors/errors.go`:

```go
// MQTT-domain sentinels.
var (
    ErrMQTTConnect    = errors.New("mqtt connect failed")
    ErrMQTTPublish    = errors.New("mqtt publish failed")
    ErrMQTTDisconnect = errors.New("mqtt disconnect failed")
)

// Sparkplug-domain sentinels.
var (
    ErrSparkplugEncode    = errors.New("sparkplug encode failed")
    ErrSparkplugState     = errors.New("sparkplug invalid state")
    ErrSparkplugNotOnline = errors.New("sparkplug not online")
)
```

Translation rules in `internal/mqtt/client.go`:

| paho error | Sentinel | Wrapping |
|------------|----------|----------|
| `paho.Token.Error()` on Connect | `ErrMQTTConnect` | `fmt.Errorf("mqtt: connect: %w: %w", ErrMQTTConnect, origErr)` |
| `paho.Token.Error()` on Publish | `ErrMQTTPublish` | same pattern |
| Disconnect error | `ErrMQTTDisconnect` | wraps original |
| Timeout waiting for token | `ErrMQTTPublish` | wraps original with timeout context |

All sentinels follow the existing pattern and support `errors.Is` traversal.

---

## 9. Server Wiring

`server.Server` gains an optional `SparkplugNode` field alongside the existing `PLCManager`:

```go
func New(cfg *config.Config, log *slog.Logger, checks []doctor.Check,
         plcMgr PLCManager, sparkplugNode SparkplugNode) *Server
```

In `Server.Run(ctx)` -- order matters:

1. Start `sparkplugNode` FIRST (it connects MQTT and registers Will). This is non-fatal; if MQTT is unconfigured, sparkplugNode is nil.
2. Start `plcMgr` SECOND (scan loop starts emitting TagUpdates; EdgeNode consumes them if ONLINE).
3. Serve HTTP.
4. On ctx cancellation: stop `plcMgr` first (stops tag reads), then stop `sparkplugNode` (publishes DDEATH, disconnects MQTT), then shutdown HTTP.

In `cmd/lgb/cmd/server.go`:

```go
// Build the Sparkplug EdgeNode when MQTT + GroupID are configured.
var sparkplugNode server.SparkplugNode
if cfg.MQTT.GroupID != "" {
    mqttClient := mqtt.NewClient(mqtt.Options{...})
    edgeNode := sparkplug.NewEdgeNode(sparkplug.EdgeNodeConfig{
        GroupID: cfg.MQTT.GroupID,
        NodeID:  cfg.MQTT.EdgeNodeID,
        Client:  mqttClient,
        Devices: buildDeviceConfigs(cfg),
        Log:     logger,
    })
    sparkplugNode = edgeNode
    // Wire the TagCallback into the PLC Manager
    tagCallback = edgeNode.HandleTagUpdate
}
plcMgr = plc.NewManager(cfg, logger, nil, tagCallback)
```

---

## 10. Testing Strategy

| Layer | What | Approach |
|-------|------|----------|
| Unit | `SeqTracker` wrap, reset, concurrency | Table-driven; `go test -race` |
| Unit | `StateMachine` transitions, invalid transitions ignored | Table-driven; verify callback on transition |
| Unit | Payload builders (NBIRTH, NDEATH, DBIRTH, DDATA) | Build payload, marshal, unmarshal, assert fields match |
| Unit | Metric encoder (12 scalar types + unsupported type error) | Table-driven; assert Sparkplug DataType enum |
| Unit | `EdgeNode` lifecycle (start, stop, HandleTagUpdate) | Mock `mqtt.Client`; verify publish calls, topic strings, state transitions |
| Unit | `mqtt.Client` adapter (`SetOrderMatters`, Will config) | Inject fake paho options; verify settings applied |
| Unit | Config validation for new MQTTSection + TagDef fields | Table-driven in `config_test.go` |
| Unit | TagCallback wiring in Manager scan loop | Mock Driver returning known values; assert callback invoked with correct TagUpdate |
| Integration | MQTT connect + publish + subscribe round-trip | `//go:build integration`; Mosquitto from `docker-compose.dev.yml` |
| Integration | EdgeNode full lifecycle (NBIRTH -> DDATA -> disconnect -> NDEATH -> reconnect -> NBIRTH) | `//go:build integration`; subscribe to `spBv1.0/#`, verify message sequence |

Strict TDD: first commit in each work-unit is a failing test. Test runner: `go test ./... -race -count=1`.

---

## 11. File Changes

| Path | Action | Description |
|------|--------|-------------|
| `proto/sparkplug_b.proto` | Create | Vendored Sparkplug B proto schema |
| `internal/sparkplug/pb/sparkplug_b.pb.go` | Create | Generated protobuf Go types (committed) |
| `internal/sparkplug/seq.go` | Create | SeqTracker: atomic uint64 mod 256 |
| `internal/sparkplug/state.go` | Create | StateMachine: OFFLINE/CONNECTING/ONLINE |
| `internal/sparkplug/payload.go` | Create | BuildNBIRTH, BuildNDEATH, BuildDBIRTH, BuildDDATA, BuildDDEATH |
| `internal/sparkplug/metric.go` | Create | GoToSparkplugType mapping, metric encoder |
| `internal/sparkplug/edge_node.go` | Create | EdgeNode: orchestrator of state + seq + payloads + publish |
| `internal/sparkplug/doc.go` | Create | Package documentation |
| `internal/sparkplug/seq_test.go` | Create | SeqTracker unit tests |
| `internal/sparkplug/state_test.go` | Create | StateMachine unit tests |
| `internal/sparkplug/payload_test.go` | Create | Payload builder unit tests |
| `internal/sparkplug/metric_test.go` | Create | Metric encoder unit tests |
| `internal/sparkplug/edge_node_test.go` | Create | EdgeNode lifecycle unit tests |
| `internal/mqtt/client.go` | Create | Client interface + pahoClient adapter |
| `internal/mqtt/options.go` | Create | Options struct |
| `internal/mqtt/doc.go` | Create | Package documentation |
| `internal/mqtt/client_test.go` | Create | Client adapter unit tests |
| `internal/mqtt/client_integration_test.go` | Create | Mosquitto integration tests |
| `internal/config/config.go` | Modify | Extend MQTTSection + PLC.Tags + TagDef; add validation |
| `internal/plc/manager.go` | Modify | Add TagUpdate, TagCallback; wire tag reads + callback into scan loop |
| `internal/errors/errors.go` | Modify | Add MQTT + Sparkplug sentinels |
| `internal/server/server.go` | Modify | Add SparkplugNode interface; accept in New(); lifecycle in Run() |
| `cmd/lgb/cmd/server.go` | Modify | Build EdgeNode + mqtt.Client; wire TagCallback into PLCManager |
| `cmd/lgb/cmd/root.go` | Modify | Add SparkplugNodeFactory to Deps (testability) |
| `Makefile` | Modify | Update `generate` target for sparkplug proto path |
| `go.mod` | Modify | Add `github.com/eclipse/paho.mqtt.golang`, `google.golang.org/protobuf` |
| `docker-compose.dev.yml` | Modify | Add Mosquitto config volume for listener settings if needed |

---

## 12. Non-Functional Considerations

| Concern | Bound | Rationale |
|---------|-------|-----------|
| Publish throughput | 100 tags/sec at 1s scan rate across 16 PLCs = 1600 DDATA publishes/sec max | Each DDATA is ~200 bytes protobuf; well within paho's capacity |
| Memory per EdgeNode | ~8 KB (state machine + seq + tag cache + channel buffer) | Single EdgeNode per gateway; negligible |
| DDATA channel buffer | 256 entries (configurable) | Prevents scan loop blocking on slow MQTT; overflow drops + WARN log |
| Reconnect timing | paho AutoReconnect with default backoff (1s initial, 2min max) | Adequate for broker restarts; no custom retry needed (paho handles TCP recovery) |
| Protobuf serialization | ~2 microseconds per DDATA payload (10 metrics) | Measured from google.golang.org/protobuf benchmarks; no allocation concern |
| Will Message (NDEATH) | Published by broker within MQTT keep-alive timeout (~30s default) | Broker-managed; no action needed from client on crash |

---

## 13. Migration / Rollout

Additive change. No data migration. No feature flags.

- When `mqtt.groupID` is empty (default), the entire Sparkplug pipeline is inert: no MQTT client created, no EdgeNode started, TagCallback is nil, scan loop runs without publishing.
- PLC Manager scan loop behavior changes: it now reads tags on each tick instead of the no-op heartbeat. This is observable but non-breaking -- if no tags are configured, ReadTag is never called.
- Rollback: revert the feature branch; PLC Manager returns to no-op heartbeat; remove `internal/sparkplug/` and `internal/mqtt/`; revert config/error/server additions. All safe because new fields have zero-value defaults.

---

## 14. Open Questions

None -- all decisions needed for `sdd-tasks` are locked in this document.
