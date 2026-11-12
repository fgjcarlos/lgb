# Change-on-value (COV)

LGB scans every configured tag at the PLC's `scanRate`. Change-on-value lets each
consumer receive a tag update only when its value changed, optionally beyond a
numeric deadband.

## Per-consumer evaluation

Every consumer owns its own COV state: "the last value delivered to me".

| Consumer | Toggle | Default |
|----------|--------|---------|
| Sparkplug DDATA | `changeOnValue.sparkplug` | `true` |
| Realtime UI feed (`/api/ws/tags`), one state per WebSocket client | `changeOnValue.realtime` | `true` |
| Historian samples | `changeOnValue.historian` | `false` (one sample per scan) |

Because the state is per consumer, one consumer never affects another: if a slow
WebSocket client's buffer is full, the update it missed is offered again on the
next scan, while other clients are unaffected. A dropped delivery is never
recorded as delivered.

An update is always delivered when:

- it is the first value the consumer sees for that tag;
- its quality changed (`good` ↔ `bad`);
- a `Boolean` or `String` value changed, or the value type changed.

Sparkplug resets its state on every birth (connect, reconnect, `Node Control/Rebirth`),
so current values are re-sent as DDATA after NBIRTH/DBIRTH. A realtime client's
state resets when it (re)subscribes.

OPC UA is not affected: its clients poll or use their own monitored-item deadbands.

## Deadband

Numeric tags accept an optional deadband:

```yaml
changeOnValue:
  sparkplug: true
  realtime: true
  historian: false

plcs:
  - name: line1
    address: 10.0.0.5:44818
    tags:
      - name: Motor.Speed
        type: Float
        deadband:
          absolute: 0.5   # engineering units
          percent: 1      # % of the last delivered value
```

A numeric update is delivered when

```
|new - lastDelivered| > max(absolute, percent / 100 × |lastDelivered|)
```

so when both are set the change must exceed both. The comparison is against the
last *delivered* value, so slow drift accumulates until it crosses the deadband.
With no deadband (both 0, the default) any change is delivered.

Validation (`lgb config validate`, the PLC API and the UI):

- `absolute` must be a finite number ≥ 0;
- `percent` must be between 0 and 100;
- a non-zero deadband is only allowed on numeric types
  (`Int8`…`Int64`, `UInt8`…`UInt64`, `Float`, `Double`).

The deadband is stored with the tag in `plcs.db` and edited in the PLC editor or
through the `deadband` object of each tag in `/api/plcs` (see `docs/api/openapi.yaml`).
Changing a tag's deadband restarts that PLC's scan worker, like any tag change.
