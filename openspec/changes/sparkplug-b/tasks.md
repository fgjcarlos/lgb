---
change: sparkplug-b
phase: tasks
date: 2026-05-23
status: draft
---

# Tasks: Sparkplug B Edge Node

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | ~1 450 (additions + deletions) |
| 400-line budget risk | High |
| Chained PRs recommended | Yes |
| Suggested split | PR 1 proto+errors+config → PR 2 sparkplug-core → PR 3 mqtt-client → PR 4 edge-node → PR 5 plc-tags → PR 6 wiring+integration |
| Delivery strategy | ask-on-risk |
| Chain strategy | stacked-to-main |

Decision needed before apply: Yes
Chained PRs recommended: Yes
Chain strategy: stacked-to-main
400-line budget risk: High

### Suggested Work Units

| Unit | Goal | Likely PR | ~Lines | Base | Self-mergeable |
|------|------|-----------|--------|------|----------------|
| 1 | Vendor proto, run codegen, add MQTT+Sparkplug sentinels, extend config + validation | `feat/spb-proto-errors-config` | ~150 | `main` | Yes — additive; all existing tests stay green |
| 2 | `internal/sparkplug/` core: SeqTracker, StateMachine, metric encoder, payload builders + unit tests | `feat/spb-sparkplug-core` | ~350 | PR 1 | Yes — no external wiring; compiles + unit tests pass |
| 3 | `internal/mqtt/` Client interface + pahoClient adapter + reconnect loop + unit tests | `feat/spb-mqtt-client` | ~250 | PR 2 | Yes — no EdgeNode wiring yet |
| 4 | `internal/sparkplug/edge_node.go` orchestrator: state + seq + payloads + publish + TagUpdate channel + unit tests | `feat/spb-edge-node` | ~300 | PR 3 | Yes — wires sparkplug + mqtt; no PLC scan loop wiring yet |
| 5 | PLC Manager: TagUpdate/TagCallback types, replace no-op scan loop, wire tag reads + callback + unit tests | `feat/spb-plc-tags` | ~200 | PR 4 | Yes — Manager emits TagUpdates; EdgeNode is an independent consumer |
| 6 | Server wiring, cmd wiring, integration tests against Mosquitto | `feat/spb-server-wiring` | ~200 | PR 5 | Yes — completes end-to-end pipeline |

Each PR targets the immediate previous branch. Only PR 1 targets `main`.

---

## Slice 1 — `feat/spb-proto-errors-config` (~150 lines)

Vendored proto, codegen, new error sentinels, config struct extensions + validation. No new packages. All existing tests must stay green.

### Group A — Protobuf codegen

- [x] **T-1.01** `chore` — Vendor `sparkplug_b.proto` to `proto/sparkplug_b.proto` (Eclipse Sparkplug B v3.0.0 canonical schema). Update `Makefile` `generate` target to invoke `protoc --go_out=internal/sparkplug/pb --go_opt=paths=source_relative proto/sparkplug_b.proto`; add `google.golang.org/protobuf` to `go.mod`/`go.sum` via `go get`. Commit generated `internal/sparkplug/pb/sparkplug_b.pb.go`.
  - **Files**: `proto/sparkplug_b.proto`, `internal/sparkplug/pb/sparkplug_b.pb.go`, `Makefile`, `go.mod`, `go.sum`
  - **Reqs**: SPK-1.1
  - **Design**: §2 (package layout), §5 decision #6
  - **Deps**: none
  - **DoD**: `make generate` exits 0; `CGO_ENABLED=0 go build ./internal/sparkplug/pb/...` exits 0.

### Group B — Error sentinels

- [x] **T-1.02** `test` — **[RED]** Extend `internal/errors/errors_test.go`: assert `ErrMQTTConnect`, `ErrMQTTPublish`, `ErrMQTTSubscribe`, `ErrSparkplugEncode` are distinct non-nil errors; assert each is identifiable via `errors.Is` when wrapped; assert none equals any existing PLC/config sentinel.
  - **Files**: `internal/errors/errors_test.go`
  - **Reqs**: SPK-ERR-4.1
  - **Design**: §8 (error model)
  - **Deps**: none
  - **DoD**: `go test ./internal/errors/...` FAILS (sentinels absent).

- [x] **T-1.03** `impl` — **[GREEN]** Add labeled blocks to `internal/errors/errors.go`: `// MQTT-domain sentinels` with `ErrMQTTConnect`, `ErrMQTTPublish`, `ErrMQTTSubscribe`; `// Sparkplug-domain sentinels` with `ErrSparkplugEncode`.
  - **Files**: `internal/errors/errors.go`
  - **Reqs**: SPK-ERR-4.1
  - **Design**: §8
  - **Deps**: T-1.02
  - **DoD**: `go test ./internal/errors/...` passes; four new sentinels exported.

### Group C — Config extensions

- [x] **T-1.04** `test` — **[RED]** Extend `internal/config/config_test.go`: table-driven cases for (a) YAML with `mqtt.groupID`, `mqtt.edgeNodeID`, `mqtt.qos`, `mqtt.keepAlive`, `mqtt.cleanSession` → fields match; (b) `mqtt.qos: 3` → error wrapping `ErrConfigInvalid` mentioning `mqtt.qos`; (c) `mqtt.keepAlive: "not-a-duration"` → `ErrConfigInvalid`; (d) `mqtt.brokerURL` non-empty + `mqtt.groupID` empty → `ErrConfigInvalid` mentioning `mqtt.groupID`; (e) `mqtt.brokerURL` empty → no error for empty groupID; (f) PLC with two `tags` entries → `cfg.PLCs[0].Tags` length 2, correct `Name` and `Type`; (g) tag with empty `name` → `ErrConfigInvalid`; (h) tag with `type: "UDT"` → `ErrConfigInvalid`; (i) PLC with no `tags` field → no error. Update `testdata/sample.yaml` with Sparkplug fields.
  - **Files**: `internal/config/config_test.go`, `internal/config/testdata/sample.yaml`
  - **Reqs**: SPK-CFG-2.1–2.7, PLC-CFG-1.7
  - **Design**: §7 (config schema)
  - **Deps**: T-1.03
  - **DoD**: `go test ./internal/config/...` FAILS (new fields absent).

- [x] **T-1.05** `impl` — **[GREEN]** Extend `MQTTSection` in `internal/config/config.go` with `GroupID`, `EdgeNodeID string`, `QoS int`, `KeepAlive string`, `CleanSession bool` with koanf tags. Add `TagDef struct` with `Name`, `Type string`. Extend `PLC` struct with `Tags []TagDef`. Extend `Validate()` with: (a) `mqtt.qos` not in [0,2]; (b) `mqtt.keepAlive` invalid duration; (c) `mqtt.brokerURL` non-empty + `groupID` or `edgeNodeID` empty; (d) per-tag `name`/`type` empty; (e) `type` not in Phase 1 scalar set. Default `QoS=1`, `KeepAlive="30s"`, `CleanSession=true` in loader defaults map.
  - **Files**: `internal/config/config.go`, `internal/config/loader.go`
  - **Reqs**: SPK-CFG-2.1–2.7, PLC-CFG-1.7
  - **Design**: §7
  - **Deps**: T-1.04
  - **DoD**: `go test ./internal/config/...` passes; `CGO_ENABLED=0 go build ./...` exits 0.

---

## Slice 2 — `feat/spb-sparkplug-core` (~350 lines)

New `internal/sparkplug/` package: SeqTracker, StateMachine, metric encoder, payload builders, doc. No MQTT wiring.

### Group A — Package skeleton + SeqTracker

- [x] **T-2.01** `test` — **[RED]** Create `internal/sparkplug/seq_test.go` (package `sparkplug_test`): (a) fresh tracker `Next()` returns 0 then 1; (b) at 255 → `Next()` returns 255, next call returns 0; (c) `Reset()` at 42 → next `Next()` returns 0; (d) 50 goroutines each call `Next()` concurrently → `go test -race` no data race.
  - **Files**: `internal/sparkplug/seq_test.go`
  - **Reqs**: SPK-1.2
  - **Design**: §2, §5 decision #7
  - **Deps**: T-1.01
  - **DoD**: `go test ./internal/sparkplug/...` FAILS (package absent).

- [x] **T-2.02** `impl` — **[GREEN]** Create `internal/sparkplug/doc.go` (package doc). Create `internal/sparkplug/seq.go`: `SeqTracker` with `atomic.Uint64`; `Next() uint64` returns `counter.Add(1) - 1 mod 256` pattern; `Reset()` stores 0; re-export `ErrSparkplugEncode` from `internal/errors`.
  - **Files**: `internal/sparkplug/doc.go`, `internal/sparkplug/seq.go`
  - **Reqs**: SPK-1.2, SPK-ERR-4.1
  - **Design**: §2, §5 decision #7
  - **Deps**: T-2.01
  - **DoD**: `go test -race ./internal/sparkplug/...` passes (seq tests).

### Group B — StateMachine

- [x] **T-2.03** `test` — **[RED]** Create `internal/sparkplug/state_test.go`: (a) OFFLINE → ConnectAttempt → CONNECTING → ConnectSuccess → ONLINE; (b) CONNECTING → ConnectFail → OFFLINE; (c) OFFLINE → ConnectSuccess (invalid) → remains OFFLINE; (d) ONLINE → Disconnect → OFFLINE; (e) concurrent reads of `State()` under `go test -race`.
  - **Files**: `internal/sparkplug/state_test.go`
  - **Reqs**: SPK-1.3
  - **Design**: §2
  - **Deps**: T-2.02
  - **DoD**: `go test ./internal/sparkplug/...` FAILS (StateMachine absent).

- [x] **T-2.04** `impl` — **[GREEN]** Create `internal/sparkplug/state.go`: `State` enum (`Offline/Connecting/Online`) backed by `atomic.Int32`; `StateMachine` with `Transition(event)` applying valid transition table (no-op on invalid); `State() State` lock-free read.
  - **Files**: `internal/sparkplug/state.go`
  - **Reqs**: SPK-1.3, SPK-1.9
  - **Design**: §2
  - **Deps**: T-2.03
  - **DoD**: `go test -race ./internal/sparkplug/...` passes (state tests).

### Group C — Metric encoder

- [x] **T-2.05** `test` — **[RED]** Create `internal/sparkplug/metric_test.go`: table-driven for all 12 Phase 1 Go → Sparkplug type mappings (bool→Boolean, int8→Int8, …, string→String); `[]byte` value → error wrapping `ErrSparkplugEncode`; encoded metric round-trips through protobuf marshal/unmarshal with correct DataType.
  - **Files**: `internal/sparkplug/metric_test.go`
  - **Reqs**: SPK-1.8, SPK-ERR-4.1
  - **Design**: §2
  - **Deps**: T-2.04
  - **DoD**: `go test ./internal/sparkplug/...` FAILS (metric.go absent).

- [x] **T-2.06** `impl` — **[GREEN]** Create `internal/sparkplug/metric.go`: `EncodeMetric(name string, value any, ts time.Time) (*pb.Payload_Metric, error)` type-switching on 12 Phase 1 types; sets DataType, value oneof, Name, Timestamp; returns `ErrSparkplugEncode` for unsupported types.
  - **Files**: `internal/sparkplug/metric.go`
  - **Reqs**: SPK-1.8
  - **Design**: §2
  - **Deps**: T-2.05
  - **DoD**: `go test -race ./internal/sparkplug/...` passes (metric tests).

### Group D — Payload builders

- [x] **T-2.07** `test` — **[RED]** Create `internal/sparkplug/payload_test.go`: (a) `BuildNBIRTH` with non-zero seq tracker → returned payload `seq==0`, tracker reset+advanced; round-trip through protobuf; (b) `BuildNDEATH(bdSeq=3)` → exactly one metric named `bdSeq` with value 3; (c) `BuildDBIRTH(plcName, tagValues, seq)` with 3 tags → 3 metrics; (d) `BuildDDATA([update])` → correct metric name, datatype, value, timestamp; (e) `BuildDDEATH` → empty metrics list with seq set.
  - **Files**: `internal/sparkplug/payload_test.go`
  - **Reqs**: SPK-1.4–1.7
  - **Design**: §2
  - **Deps**: T-2.06
  - **DoD**: `go test ./internal/sparkplug/...` FAILS (payload.go absent).

- [x] **T-2.08** `impl` — **[GREEN]** Create `internal/sparkplug/payload.go`: `BuildNBIRTH(seq *SeqTracker, devices []DeviceConfig) ([]byte, error)` — calls `seq.Reset()` then `seq.Next()`, sets seq=0, encodes all tag metrics, marshals to proto bytes; `BuildNDEATH(bdSeq uint64) ([]byte, error)`; `BuildDBIRTH(deviceID string, tagValues map[string]any, seq uint64) ([]byte, error)`; `BuildDDATA(updates []TagUpdate, seq uint64) ([]byte, error)`; `BuildDDEATH(deviceID string, seq uint64) ([]byte, error)`. All call `proto.Marshal`.
  - **Files**: `internal/sparkplug/payload.go`
  - **Reqs**: SPK-1.4–1.7
  - **Design**: §2, §6.1
  - **Deps**: T-2.07
  - **DoD**: `go test -race ./internal/sparkplug/...` passes all payload tests; `CGO_ENABLED=0 go build ./internal/sparkplug/...` exits 0.

---

## Slice 3 — `feat/spb-mqtt-client` (~250 lines)

New `internal/mqtt/` package: Client interface, pahoClient adapter, reconnect loop, paho dep. No EdgeNode wiring.

### Group A — Package skeleton + interface

- [x] **T-3.01** `test` — **[RED]** Create `internal/mqtt/client_test.go` (package `mqtt_test`): (a) compile-time assertion `var _ Client = (*pahoClient)(nil)`; (b) `NewClient(opts)` with capture-options spy → `SetOrderMatters(false)` called; (c) `SetAutoReconnect(false)` called; (d) Will topic equals `spBv1.0/{group}/{edge_node}/NDEATH`; (e) WillQoS is 1, WillRetain false, WillPayload non-empty; (f) `Connect` on already-cancelled ctx → returns `ErrMQTTConnect`; (g) `Publish` when not connected → returns `ErrMQTTConnect`.
  - **Files**: `internal/mqtt/client_test.go`
  - **Reqs**: MQTT-1.1–1.4, MQTT-1.7, SPK-ERR-4.1
  - **Design**: §4 (interface definitions), §5 decisions #1, #2, #5
  - **Deps**: T-1.03, T-2.08
  - **DoD**: `go test ./internal/mqtt/...` FAILS (package absent).

- [x] **T-3.02** `impl` — **[GREEN]** Add `github.com/eclipse/paho.mqtt.golang v1.5.1` to `go.mod`. Create `internal/mqtt/doc.go`. Create `internal/mqtt/options.go`: `Options` struct with `BrokerURL`, `ClientID`, `Username`, `Password`, `QoS`, `KeepAlive`, `CleanSession`, `WillTopic`, `WillPayload`, `WillQoS`, `WillRetain`. Create `internal/mqtt/client.go`: `Client` interface (`Connect`, `Disconnect`, `Publish`, `IsConnected`, `SetOnConnect`); `pahoClient` struct; `NewClient(opts Options) Client` — builds `paho.ClientOptions` with `SetOrderMatters(false)`, `SetAutoReconnect(false)`, `SetWill(...)`, sets `OnConnect` handler dispatch; re-exports `ErrMQTTConnect`, `ErrMQTTPublish`, `ErrMQTTSubscribe` from `internal/errors`.
  - **Files**: `internal/mqtt/doc.go`, `internal/mqtt/options.go`, `internal/mqtt/client.go`, `go.mod`, `go.sum`
  - **Reqs**: MQTT-1.1–1.4, MQTT-1.6–1.8, SPK-ERR-4.1–4.2
  - **Design**: §4, §5 decisions #1, #2, #5, §8
  - **Deps**: T-3.01
  - **DoD**: `go test -race ./internal/mqtt/...` passes unit tests; `CGO_ENABLED=0 go build ./internal/mqtt/...` exits 0.

### Group B — Cross-platform build gate

- [x] **T-3.03** `chore` — Verify `CGO_ENABLED=0 go build ./internal/mqtt/...` on all four targets (`linux/amd64`, `linux/arm64`, `darwin/arm64`, `windows/amd64`). Record results in PR description. No code change expected.
  - **Files**: (none — verification only)
  - **Reqs**: MQTT-1.8
  - **Design**: §12 (non-functional)
  - **Deps**: T-3.02
  - **DoD**: All four cross-builds exit 0 with `CGO_ENABLED=0`.

---

## Slice 4 — `feat/spb-edge-node` (~300 lines)

`internal/sparkplug/edge_node.go`: orchestrator wiring state + seq + payload builders + MQTT publish + TagUpdate channel + TagCallback types + unit tests with mock Client.

### Group A — EdgeNode + TagUpdate

- [ ] **T-4.01** `test` — **[RED]** Create `internal/sparkplug/edge_node_test.go` (package `sparkplug_test`): define `mockMQTTClient` implementing `mqtt.Client`; (a) `NewEdgeNode` returns non-nil; (b) `Start(ctx)` → NBIRTH published to `spBv1.0/{group}/NBIRTH/{node}`, DBIRTH per device, state → ONLINE; (c) NBIRTH topic, QoS 1; (d) `Stop()` → DDEATH per device published, Disconnect called, state → Offline; (e) `HandleTagUpdate` when ONLINE → DDATA published on publisher goroutine; (f) `HandleTagUpdate` when OFFLINE → update dropped, no publish; (g) seq resets to 0 on each Start; (h) concurrent `HandleTagUpdate` calls → `go test -race` no data race.
  - **Files**: `internal/sparkplug/edge_node_test.go`
  - **Reqs**: SPK-1.3–1.7, MQTT-1.3, MQTT-1.5
  - **Design**: §4 (EdgeNode interface), §6.1–6.3, §5 decisions #4, #8, #9, #11
  - **Deps**: T-3.02
  - **DoD**: `go test ./internal/sparkplug/...` FAILS (EdgeNode absent).

- [ ] **T-4.02** `impl` — **[GREEN]** Create `internal/sparkplug/edge_node.go`: `TagUpdate`, `TagCallback`, `TagDef`, `DeviceConfig`, `EdgeNodeConfig` structs; `EdgeNode` struct with `StateMachine`, `SeqTracker`, `mqtt.Client`, unbuffered `chan TagUpdate` (256 buffer), bdSeq `atomic.Uint64`. Implement `NewEdgeNode(cfg EdgeNodeConfig) *EdgeNode`. `Start(ctx)`: build+register NDEATH as Will (via `Options.WillTopic/WillPayload`), call `client.Connect`, register `onConnect` callback — in callback: `state→Connecting`, `seq.Reset()`, publish NBIRTH (seq=0), publish DBIRTH per device, `state→Online`. `Stop()`: publish DDEATH per device, `client.Disconnect`, `state→Offline`. `HandleTagUpdate(u TagUpdate)`: non-blocking send to channel (drop+log WARN if full). Internal publisher goroutine drains channel, encodes DDATA, publishes (drops if not ONLINE). Topic helpers: `nodeTopic(group, node, verb)`, `deviceTopic(group, node, device, verb)`.
  - **Files**: `internal/sparkplug/edge_node.go`
  - **Reqs**: SPK-1.3–1.7, MQTT-1.1–1.5, SPK-ERR-4.1
  - **Design**: §4, §5 decisions #4, #8, #9, #11, §6.1–6.3, §9
  - **Deps**: T-4.01
  - **DoD**: `go test -race ./internal/sparkplug/...` passes all edge_node tests; `CGO_ENABLED=0 go build ./internal/sparkplug/...` exits 0.

---

## Slice 5 — `feat/spb-plc-tags` (~200 lines)

PLC Manager: accept `TagCallback`, replace no-op scan loop with actual tag reads, emit TagUpdates per configured tag.

### Group A — TagCallback wiring in Manager

- [ ] **T-5.01** `test` — **[RED]** Extend `internal/plc/manager_test.go`: (a) `NewManager` accepts fourth `TagCallback` param; (b) scan tick with one configured tag → callback called once with correct `TagUpdate` (`PLCName`, `Tag`, `Value`, `Timestamp`); (c) nil callback → no panic on tick; (d) read error on tag 2 of 3 → callback called for tags 1 and 3, not tag 2; (e) `go test -race` no data race across concurrent manager start+callback invocation.
  - **Files**: `internal/plc/manager_test.go`
  - **Reqs**: SPK-PLC-3.1–3.3, PLC-DRV-2.1–2.2
  - **Design**: §4 (TagUpdate, TagCallback), §5 decision #4, §6.2
  - **Deps**: T-4.02
  - **DoD**: `go test ./internal/plc/...` FAILS (TagCallback param absent from NewManager).

- [ ] **T-5.02** `impl` — **[GREEN]** Add `TagUpdate` and `TagCallback` types to `internal/plc/manager.go` (or a new `internal/plc/tags.go`). Update `NewManager` signature to accept `tagCb TagCallback` (nil is valid). Store `tagCb` on `Manager`. Update `runWorker`: replace no-op heartbeat with loop over `plcCfg.Tags` — for each tag allocate typed dest from `tag.Type`, call `d.ReadTag(tag.Name, &dest)`, on success call `m.tagCb(TagUpdate{PLCName: name, Tag: tag.Name, Value: dest, Timestamp: time.Now()})`, on error log WARN and continue. Update `defaultDriverFactory` and all callers of `NewManager` (`cmd/lgb/cmd/server.go`) to pass nil callback for now.
  - **Files**: `internal/plc/manager.go` (or `internal/plc/tags.go`), `cmd/lgb/cmd/server.go`
  - **Reqs**: SPK-PLC-3.1–3.3, PLC-DRV-2.1–2.2
  - **Design**: §4, §6.2
  - **Deps**: T-5.01
  - **DoD**: `go test -race ./internal/plc/...` passes; `CGO_ENABLED=0 go build ./...` exits 0.

---

## Slice 6 — `feat/spb-server-wiring` (~200 lines)

SparkplugNode interface in server, cmd wiring, integration tests against Mosquitto.

### Group A — Server wiring

- [ ] **T-6.01** `test` — **[RED]** Extend `internal/server/server_test.go`: (a) `mockSparkplugNode` with `Start`/`Stop`; (b) `server.New` accepts fifth `sparkplugNode SparkplugNode` param; (c) `Run(ctx)` calls `sparkplugNode.Start` BEFORE `plcMgr.Start`; (d) on ctx cancel: `plcMgr.Stop` called BEFORE `sparkplugNode.Stop`; (e) nil `sparkplugNode` → no panic (backward-compat).
  - **Files**: `internal/server/server_test.go`
  - **Reqs**: SPK-1.3, MQTT-1.1–1.3
  - **Design**: §9 (server wiring), §6.1
  - **Deps**: T-5.02
  - **DoD**: `go test ./internal/server/...` FAILS (New does not accept sparkplugNode).

- [ ] **T-6.02** `impl` — **[GREEN]** Add `SparkplugNode interface { Start(context.Context) error; Stop() error }` to `internal/server/server.go`. Update `New` to accept `sparkplugNode SparkplugNode` (fifth param, nil-safe). In `Run(ctx)`: start sparkplugNode first, plcMgr second; on shutdown stop plcMgr first, sparkplugNode second. Update all `server.New` call-sites.
  - **Files**: `internal/server/server.go`
  - **Reqs**: SPK-1.3
  - **Design**: §9
  - **Deps**: T-6.01
  - **DoD**: `go test ./internal/server/...` passes.

### Group B — cmd wiring

- [ ] **T-6.03** `test` — **[RED]** Extend `cmd/lgb/cmd/server_test.go`: (a) config with `mqtt.groupID` non-empty → `Deps.SparkplugNodeFactory` called, returned node passed to `server.New`; (b) `mqtt.groupID` empty → sparkplugNode passed as nil; (c) TagCallback wired from EdgeNode to `NewManager`.
  - **Files**: `cmd/lgb/cmd/server_test.go`
  - **Reqs**: SPK-PLC-3.2, MQTT-1.1, SPK-1.3
  - **Design**: §9
  - **Deps**: T-6.02
  - **DoD**: `go test ./cmd/lgb/cmd/...` FAILS (cmd wiring absent).

- [ ] **T-6.04** `impl` — **[GREEN]** Update `cmd/lgb/cmd/server.go`: when `cfg.MQTT.GroupID != ""` build `mqtt.NewClient(mqtt.Options{...})` and `sparkplug.NewEdgeNode(sparkplug.EdgeNodeConfig{..., Devices: buildDeviceConfigs(cfg)})`, capture `edgeNode.HandleTagUpdate` as `tagCallback`; else `tagCallback = nil`. Pass `tagCallback` as fourth arg to `plc.NewManager`. Pass `edgeNode` (or nil) as fifth arg to `server.New`. Add `buildDeviceConfigs(cfg *config.Config) []sparkplug.DeviceConfig` helper. Log INFO `component="sparkplug-edge-node"` when node is built.
  - **Files**: `cmd/lgb/cmd/server.go`
  - **Reqs**: MQTT-1.1–1.3, SPK-1.3, SPK-PLC-3.2
  - **Design**: §9
  - **Deps**: T-6.03
  - **DoD**: `go test ./cmd/lgb/cmd/...` passes; `CGO_ENABLED=0 go build -tags no_embed ./cmd/lgb` exits 0.

### Group C — Integration tests

- [ ] **T-6.05** `test` — **[integration]** Create `internal/mqtt/client_integration_test.go` (`//go:build integration`): (a) `TestIntegration_ConnectPublish` — connect to Mosquitto (`localhost:1883`), subscribe to `spBv1.0/#`, publish NBIRTH, verify subscriber receives on correct topic; (b) `TestIntegration_SetOrderMatters` — high-throughput publish (100 messages QoS 1) → no deadlock within 10 s; (c) `TestIntegration_Reconnect` — disconnect broker (pause container), reconnect fires OnConnect, NBIRTH re-published.
  - **Files**: `internal/mqtt/client_integration_test.go`
  - **Reqs**: MQTT-1.1–1.3, MQTT-1.7
  - **Design**: §10 (testing strategy, integration layer)
  - **Deps**: T-6.04
  - **DoD**: `go test -tags=integration -race ./internal/mqtt/...` passes with Mosquitto running.

- [ ] **T-6.06** `test` — **[integration]** Create `internal/sparkplug/edge_node_integration_test.go` (`//go:build integration`): subscribe to `spBv1.0/#` on Mosquitto; (a) `TestIntegration_FullLifecycle` — Start EdgeNode → verify NBIRTH received → simulate TagUpdate → verify DDATA topic and payload → Stop → verify DDEATH; (b) `TestIntegration_ReconnectReplaysNBIRTH` — drop connection → verify NBIRTH+DBIRTH replayed on reconnect, seq=0; (c) `go test -race`.
  - **Files**: `internal/sparkplug/edge_node_integration_test.go`
  - **Reqs**: SPK-1.3–1.7, MQTT-1.3, MQTT-1.5
  - **Design**: §10
  - **Deps**: T-6.05
  - **DoD**: `go test -tags=integration -race ./internal/sparkplug/...` passes with Mosquitto running.

### Group D — Final build gate

- [ ] **T-6.07** `chore` — Verify `CGO_ENABLED=0 go build -tags no_embed ./...` on all four targets. Verify `go test ./... -race -count=1` (non-integration) passes. Record results in PR description.
  - **Files**: (none — verification only)
  - **Reqs**: SPK-1.1, MQTT-1.8
  - **Design**: §12
  - **Deps**: T-6.06
  - **DoD**: All four cross-builds and unit test suite exit 0.

---

## Cross-slice dependency summary

```
T-1.01 (proto codegen) ─────────────────────────────> T-2.01
T-1.02 -> T-1.03 (error sentinels)
T-1.03 -> T-1.04 -> T-1.05 (config struct + validation)

T-1.01 + T-1.05 -> T-2.01 -> T-2.02 (SeqTracker)
T-2.02 -> T-2.03 -> T-2.04 (StateMachine)
T-2.04 -> T-2.05 -> T-2.06 (metric encoder)
T-2.06 -> T-2.07 -> T-2.08 (payload builders)

T-1.03 + T-2.08 -> T-3.01 -> T-3.02 (mqtt client)
T-3.02 -> T-3.03 (cross-platform gate)

T-3.02 -> T-4.01 -> T-4.02 (EdgeNode)

T-4.02 -> T-5.01 -> T-5.02 (PLC Manager tag reads)

T-5.02 -> T-6.01 -> T-6.02 (server wiring)
T-6.02 -> T-6.03 -> T-6.04 (cmd wiring)
T-6.04 -> T-6.05 (mqtt integration tests)
T-6.05 -> T-6.06 (edge node integration tests)
T-6.06 -> T-6.07 (final build gate)
```

Parallel opportunities within a slice:
- Slice 1: T-1.01 (chore) and T-1.02/T-1.03 (sentinels) can start in parallel; T-1.04 requires both.
- Slice 2: Groups A→B→C→D are strictly sequential (each group builds on the previous types).
- Slice 6: T-6.01/T-6.02 (server) and T-6.03/T-6.04 (cmd) are sequential; T-6.05 and T-6.06 can run in parallel after T-6.04.

---

## Risk Acknowledgements

| Risk | Mitigation |
|------|------------|
| paho v1 `OnConnect` fires in paho's internal goroutine; blocking publishes inside it may deadlock if `SetOrderMatters(true)` | `SetOrderMatters(false)` set unconditionally in T-3.02; unit test asserts in T-3.01 |
| NBIRTH/DDATA ordering: DDATA may be published before NBIRTH completes if publisher goroutine drains channel during OnConnect | State machine gates publisher goroutine to ONLINE only after OnConnect finishes publishing NBIRTH+DBIRTH (T-4.02) |
| `NewManager` signature change (add TagCallback param) breaks cmd caller | T-5.02 updates all call-sites atomically in the same commit; existing callers pass nil |
| `server.New` fifth param addition breaks existing tests | T-6.02 updates all call-sites and tests in same commit |
| Mosquitto in `docker-compose.dev.yml` may need listener config for unauthenticated test connections | Add `allow_anonymous true` + `listener 1883` to Mosquitto config volume in T-6.04 if needed |
| Protobuf codegen requires `protoc` + `protoc-gen-go` in CI; not in current CI | Document in Makefile comment; generated file is committed so `go build` works without protoc; add CI step for `make generate` diff check |
| SeqTracker `Next()` must return 0 on first call (Sparkplug spec: NBIRTH seq=0 after Reset) | Reset is always called before BuildNBIRTH; seq_test.go asserts reset→0 in T-2.01 |
