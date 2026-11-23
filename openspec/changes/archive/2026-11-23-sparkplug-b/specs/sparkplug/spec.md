---
change: sparkplug-b
phase: spec
domain: sparkplug
date: 2026-05-23
status: draft
type: new
---

# Sparkplug Domain Specification

## Purpose

Defines `internal/sparkplug/`: protobuf codegen pipeline, payload builders for all BIRTH/DEATH/DATA message types, sequence number tracking, metric encoding for Phase 1 scalar types, and the Sparkplug B state machine governing the edge node lifecycle.

---

## Requirements

### [SPK-1.1] Protobuf codegen from sparkplug_b.proto

The project MUST vendor the official `sparkplug_b.proto` schema and generate Go types into `internal/sparkplug/pb/` via `make generate`. The generated files MUST be committed. The `make generate` target MUST invoke `protoc --go_out` with `protoc-gen-go`. No CGO MUST be required at runtime (`google.golang.org/protobuf` is pure-Go).

#### Scenario: make generate produces Go types

- GIVEN `sparkplug_b.proto` is present in the vendor location
- WHEN `make generate` is run
- THEN `internal/sparkplug/pb/*.pb.go` files exist with no build errors
- AND `CGO_ENABLED=0 go build ./internal/sparkplug/...` succeeds

---

### [SPK-1.2] Sequence number tracker

The `internal/sparkplug` package MUST provide a sequence number tracker that:

1. Starts at 0 on first use.
2. Increments by 1 on each `Next()` call.
3. Wraps from 255 back to 0 (values 0–255 inclusive, modulo 256).
4. Resets to 0 on `Reset()`.
5. Is safe for concurrent use via `sync/atomic` or equivalent.

| Operation | Precondition | Result |
|-----------|-------------|--------|
| `Next()` | counter = 0 | returns 0, counter becomes 1 |
| `Next()` | counter = 255 | returns 255, counter wraps to 0 |
| `Reset()` | any value | counter becomes 0 |

#### Scenario: Sequence wraps at 256

- GIVEN the counter is at 255
- WHEN `Next()` is called
- THEN it returns 255
- AND the next call to `Next()` returns 0

#### Scenario: Reset clears counter

- GIVEN the counter is at 42
- WHEN `Reset()` is called
- THEN the next `Next()` returns 0

#### Scenario: Concurrent access is race-free

- GIVEN 50 goroutines each call `Next()` concurrently
- WHEN `go test -race` is run
- THEN no data race is reported

---

### [SPK-1.3] State machine — OFFLINE / CONNECTING / ONLINE

The `internal/sparkplug` package MUST define a state machine with exactly three states:

| State | Description |
|-------|-------------|
| `OFFLINE` | Initial state; no MQTT connection |
| `CONNECTING` | MQTT connect in progress |
| `ONLINE` | NBIRTH acknowledged; DDATA may be published |

Valid transitions:

| From | Event | To |
|------|-------|----|
| OFFLINE | ConnectAttempt | CONNECTING |
| CONNECTING | ConnectSuccess | ONLINE |
| CONNECTING | ConnectFail | OFFLINE |
| ONLINE | Disconnect | OFFLINE |

Any other transition MUST be a no-op (ignored, not an error).

The state MUST be readable without holding a lock (use `sync/atomic` for state storage). State changes MUST be observable via a callback or channel for testing.

#### Scenario: Successful connect sequence

- GIVEN the state machine is OFFLINE
- WHEN ConnectAttempt then ConnectSuccess are applied
- THEN state is ONLINE

#### Scenario: Failed connect returns to OFFLINE

- GIVEN the state machine is CONNECTING
- WHEN ConnectFail is applied
- THEN state is OFFLINE

#### Scenario: Invalid transition is ignored

- GIVEN the state machine is OFFLINE
- WHEN ConnectSuccess is applied (invalid from OFFLINE)
- THEN state remains OFFLINE

---

### [SPK-1.4] NBIRTH payload

The `internal/sparkplug` package MUST provide a builder that produces a valid Sparkplug B NBIRTH payload. The payload MUST:

1. Set `timestamp` to the current Unix epoch time in milliseconds.
2. Set `seq` to the value from the sequence tracker at time of call; MUST call `Reset()` then `Next()` before constructing the payload (Sparkplug B spec: seq=0 for NBIRTH).
3. Include one metric per configured tag, encoding name and Sparkplug B datatype.
4. Produce a valid protobuf binary that deserializes back to the same field values.

#### Scenario: NBIRTH seq is always 0

- GIVEN the sequence tracker is at any value
- WHEN `BuildNBIRTH(tags)` is called
- THEN the returned payload's `seq` field is 0
- AND the internal counter is reset then advanced

#### Scenario: NBIRTH round-trips through protobuf

- GIVEN a set of tag definitions
- WHEN `BuildNBIRTH(tags)` is serialized and deserialized
- THEN all metric names and datatypes are preserved

---

### [SPK-1.5] NDEATH payload

The NDEATH payload MUST be pre-built at startup (used as MQTT Will Message). It MUST:

1. Contain no metrics.
2. Set `bdSeq` metric: a `uint64` metric named `bdSeq` carrying the birth/death sequence counter value at time of registration.
3. Produce valid protobuf binary.

#### Scenario: NDEATH carries bdSeq metric

- GIVEN bdSeq value is 3
- WHEN `BuildNDEATH(bdSeq)` is called
- THEN the returned payload contains exactly one metric named `bdSeq` with value 3

---

### [SPK-1.6] DBIRTH and DDEATH payloads

The package MUST provide builders for DBIRTH and DDEATH payloads per connected PLC (device):

- DBIRTH: includes all configured tag metrics with current values, `seq` from sequence tracker.
- DDEATH: empty metrics list, `seq` from sequence tracker.

#### Scenario: DBIRTH contains all device metrics

- GIVEN a PLC with 3 configured tags
- WHEN `BuildDBIRTH(plcName, tagValues, seq)` is called
- THEN the payload contains 3 metrics with names and values matching the tags

---

### [SPK-1.7] DDATA payload

The package MUST provide a builder that encodes one or more tag updates as a DDATA payload. DDATA MUST:

1. Include only changed or newly-read tags (not all tags).
2. Set `seq` from the sequence tracker.
3. Set `timestamp` per metric to the read time.
4. MUST NOT be published while state machine is not ONLINE.

#### Scenario: DDATA blocked when not ONLINE

- GIVEN the state machine is OFFLINE or CONNECTING
- WHEN `BuildDDATA(updates)` is called
- THEN it returns an error or zero-length payload

#### Scenario: DDATA encodes tag values correctly

- GIVEN a TagUpdate with name="Motor.Speed", value=float32(1200.5), timestamp=T
- WHEN `BuildDDATA([update])` produces a payload
- THEN the metric has name "Motor.Speed", float32 datatype, value 1200.5, timestamp T

---

### [SPK-1.8] Phase 1 scalar metric types

The metric encoder MUST support the following Go → Sparkplug B type mappings:

| Go type | Sparkplug B DataType |
|---------|---------------------|
| `bool` | Boolean |
| `int8` | Int8 |
| `int16` | Int16 |
| `int32` | Int32 |
| `int64` | Int64 |
| `uint8` | UInt8 |
| `uint16` | UInt16 |
| `uint32` | UInt32 |
| `uint64` | UInt64 |
| `float32` | Float |
| `float64` | Double |
| `string` | String |

Any other Go type MUST return `ErrSparkplugEncode`.

#### Scenario: Unsupported type returns error

- GIVEN a tag value of type `[]byte`
- WHEN the metric encoder is called
- THEN it returns an error wrapping `ErrSparkplugEncode`

---

### [SPK-1.9] Thread safety

All exported types in `internal/sparkplug/` MUST be safe for concurrent use from multiple goroutines. Internal state (sequence counter, state machine) MUST use `sync/atomic` or `sync.Mutex`. No `data race` MUST be reported under `go test -race`.
