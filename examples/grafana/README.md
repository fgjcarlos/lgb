# Grafana with LGB Sparkplug B

LGB publishes Sparkplug B protobuf, not JSON. The tiny Go bridge in this directory is an LGB-specific decoder for **named numeric DDATA metrics** using the repository's Sparkplug protobuf types. It is not a generic Sparkplug alias decoder: it deliberately ignores aliases, string/boolean metrics, and all non-DDATA messages. Use Ignition MQTT Engine (see [`../ignition/README.md`](../ignition/README.md)) for a general Sparkplug decoder.

## Run the read-only bridge

Use Go from the repository root so the bridge can import LGB's internal protobuf package. Give it a dedicated MQTT identity with subscribe-only ACL for `spBv1.0/+/DDATA/+/+`; grant no publish/NCMD/DCMD permission. Supply a trusted broker CA and TLS broker URL, for example:

```sh
MQTT_BROKER=ssl://broker.example.net:8883 MQTT_CA=/etc/lgb/broker-ca.pem \
MQTT_USER=lgb-metrics MQTT_PASSWORD='...' go run ./examples/grafana
```

The HTTP scrape listener binds only to `127.0.0.1:9108` and serves `/metrics`; proxy it securely if Prometheus is remote. The process keeps the latest numeric value for each group, edge node, device, and metric. It exports a stable `lgb_sparkplug_metric` gauge with those identifiers as escaped Prometheus labels; stale series are not removed because this minimal example does not consume birth/death lifecycle messages. The broker URL must be `ssl://hostname[:port]` with no userinfo, and certificates are verified against `MQTT_CA` and the broker hostname; never disable verification in production.

## Prometheus and Grafana

A local Prometheus can scrape the bridge:

```yaml
scrape_configs:
  - job_name: lgb-sparkplug
    scrape_interval: 15s
    static_configs:
      - targets: ["127.0.0.1:9108"]
```

Add a Prometheus datasource in Grafana pointing to Prometheus (commonly `http://localhost:9090` for a local setup, or the secured service URL in a deployment). Query the gauge by metric label, e.g. `lgb_sparkplug_metric{metric="Temperature"}`; narrow by `group`, `node`, and `device` when needed. Protect both services and keep the bridge endpoint local unless intentionally secured.
