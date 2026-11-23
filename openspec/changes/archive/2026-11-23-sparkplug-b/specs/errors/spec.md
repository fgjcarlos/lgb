---
change: sparkplug-b
phase: spec
domain: errors
date: 2026-05-23
status: draft
type: delta
---

# Delta for Errors

## ADDED Requirements

### [SPK-ERR-4.1] New sentinel errors for MQTT and Sparkplug B

The `internal/errors` package MUST define the following new sentinel error values using `errors.New`:

| Sentinel | Package exported from | Meaning |
|----------|-----------------------|---------|
| `ErrMQTTConnect` | `internal/errors`, re-exported in `internal/mqtt` | MQTT broker connection cannot be established or client is not connected |
| `ErrMQTTPublish` | `internal/errors`, re-exported in `internal/mqtt` | MQTT publish operation failed (broker rejection, token timeout) |
| `ErrMQTTSubscribe` | `internal/errors`, re-exported in `internal/mqtt` | MQTT subscribe operation failed |
| `ErrSparkplugEncode` | `internal/errors`, re-exported in `internal/sparkplug` | Sparkplug B payload encoding failed (unsupported type, protobuf marshal error) |

All sentinels MUST follow the `Err{Domain}{Condition}` naming pattern (MVP-FND-5.1).

Re-exports MUST follow the existing pattern in `internal/plc/driver.go` — a package-level `var` block re-exporting from `internal/errors` for ergonomic use.

#### Scenario: ErrMQTTConnect identified with errors.Is

- GIVEN `internal/mqtt` returns a wrapped `ErrMQTTConnect`
- WHEN a caller checks `errors.Is(err, mqtt.ErrMQTTConnect)`
- THEN it returns `true`
- AND `errors.Is(err, mqtt.ErrMQTTPublish)` returns `false`

#### Scenario: ErrSparkplugEncode identified with errors.Is

- GIVEN a metric encoder returns an error for an unsupported type
- WHEN a caller checks `errors.Is(err, sparkplug.ErrSparkplugEncode)`
- THEN it returns `true`

#### Scenario: Sentinels are in the global sentinel table

- GIVEN the errors spec sentinel table (MVP-FND-5.1)
- WHEN the sparkplug-b change is applied
- THEN the table MUST include all four new sentinels with their package and meaning

---

### [SPK-ERR-4.2] Error wrapping convention for new packages

`internal/mqtt` and `internal/sparkplug` MUST follow the existing wrapping convention (MVP-FND-5.2):

1. Wrap errors with `fmt.Errorf("context: %w", err)` — lowercase, no trailing period.
2. MUST wrap the appropriate sentinel as the first `%w` argument when a domain error occurs.
3. The underlying paho or protobuf error MUST be preserved in the chain as a second `%w` or via `errors.Join`.

#### Scenario: MQTT publish error preserves chain

- GIVEN paho returns a token error `tokenErr` on publish
- WHEN the wrapper returns the error
- THEN `errors.Is(err, ErrMQTTPublish)` returns `true`
- AND `errors.As(err, &tokenErr)` or string-contains check can surface the original cause

---

## MODIFIED Requirements

### [MVP-FND-5.1] Sentinel errors per package

Every `internal/` package that produces domain-specific errors MUST export named sentinel error values using `errors.New`. Callers MUST compare errors using `errors.Is`, never by string comparison. Sentinel names MUST follow the pattern `Err{Domain}{Condition}`.

| Package | Sentinel | Meaning |
|---------|----------|---------|
| `internal/config` | `ErrConfigInvalid` | Schema or constraint violation |
| `internal/config` | `ErrConfigMissing` | Config file not found |
| `internal/config` | `ErrConfigPermission` | File not readable |
| `internal/datadir` | `ErrDataDirInvalid` | Path exists but is not a directory |
| `internal/datadir` | `ErrDataDirPermission` | Path not writable by running user |
| `internal/doctor` | `ErrCheckFailed` | A doctor check returned FAIL status |
| `internal/retry` | `ErrMaxAttempts` | Retry exhausted max attempts |
| `internal/errors` | `ErrPLCConnect` | TCP/CIP connection to PLC cannot be established |
| `internal/errors` | `ErrPLCRead` | Tag read operation failed |
| `internal/errors` | `ErrPLCWrite` | Tag write operation failed |
| `internal/errors` | `ErrPLCTimeout` | PLC operation exceeded SocketTimeout deadline |
| `internal/errors` | `ErrMQTTConnect` | MQTT broker connection cannot be established or client not connected |
| `internal/errors` | `ErrMQTTPublish` | MQTT publish operation failed |
| `internal/errors` | `ErrMQTTSubscribe` | MQTT subscribe operation failed |
| `internal/errors` | `ErrSparkplugEncode` | Sparkplug B payload encoding failed |

PLC sentinels are re-exported in `internal/plc`. MQTT sentinels are re-exported in `internal/mqtt`. Sparkplug sentinels are re-exported in `internal/sparkplug`.

Future packages MUST add sentinels to this list in their own specs before apply.

(Previously: table did not include ErrMQTTConnect, ErrMQTTPublish, ErrMQTTSubscribe, or ErrSparkplugEncode.)

#### Scenario: Sentinel error identified with errors.Is

- GIVEN `internal/config` returns a wrapped `ErrConfigInvalid`
- WHEN a caller checks `errors.Is(err, config.ErrConfigInvalid)`
- THEN it returns `true`
- AND `errors.Is(err, config.ErrConfigMissing)` returns `false`

#### Scenario: New MQTT sentinels satisfy errors.Is

- GIVEN `internal/mqtt` returns a wrapped `ErrMQTTPublish`
- WHEN a caller checks `errors.Is(err, mqtt.ErrMQTTPublish)`
- THEN it returns `true`
