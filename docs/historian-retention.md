# Historian retention

The historian deletes samples older than their retention once an hour. Retention is
set at three levels; the most specific one wins for each tag:

| Level | Setting | 0 / unset means |
|-------|---------|-----------------|
| Tag | `plcs[i].tags[j].retentionDays` (API: `retention_days`) | inherit the PLC default |
| PLC | `plcs[i].retentionDays` (API: `retention_days`) | inherit the global default |
| Global | `historian.retentionDays` (default `90`) | historian disabled |

```yaml
historian:
  retentionDays: 90          # global default
plcs:
  - name: line1
    address: 10.0.0.5:44818
    retentionDays: 30        # every line1 tag without its own value
    tags:
      - name: Motor.Speed    # high-frequency: keep 1 day
        type: Float
        retentionDays: 1
      - name: OEE.Daily      # slow KPI: keep 5 years
        type: Float
        retentionDays: 1825
```

- An override may be shorter or longer than its parent level.
- Samples of PLCs or tags that are no longer configured follow the global default.
- Values must be between 0 and 36500 days; `lgb config validate`, startup and the PLC
  API reject anything else.
- PLC and tag settings live in the PLC store (`plcs.db`; YAML only seeds it on first
  boot). Edit them on the **PLCs** page or with `PUT /api/plcs/{name}`; the next
  hourly run applies them without a restart. Changing `historian.retentionDays`
  requires a restart.
- If the PLC store cannot be read, that hourly run deletes nothing.

## Viewing the effective retention

The **Historian** page lists every PLC and tag with its effective retention and the
level that set it (`tag`, `plc` or `global`). The same data is available from
`GET /api/historian/retention` (any authenticated role; `503` when the historian is
disabled).
