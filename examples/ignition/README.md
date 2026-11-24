# Ignition MQTT Engine — read-only Sparkplug B

This walkthrough configures Cirrus Link MQTT Engine to decode LGB Sparkplug B traffic into Ignition tags. Validate it first against an isolated broker and LGB simulator; do not deploy against a live control network as a test.

## 1. Broker and credentials

Create a dedicated MQTT identity for Ignition with subscribe-only broker ACLs for the Sparkplug telemetry topics below. Deny publish access, especially to `NCMD` and `DCMD`. Use a separate certificate/identity from LGB's publisher.

In Ignition Gateway, install/configure Cirrus Link MQTT Transmission/Engine as licensed for your version. Under **MQTT Engine → Servers**, add the broker using its TLS listener (typically port 8883), set a unique client ID and the read-only username/password, enable TLS, and configure the trusted CA certificate that issued the broker certificate. Keep hostname verification enabled; do not accept untrusted certificates or disable validation in production. The exact labels vary by MQTT Engine release.

## 2. Namespace and birth handling

Under **MQTT Engine → Settings / Namespaces** (label varies by release), enable the Sparkplug B namespace `spBv1.0`. Configure the namespace to use the server created above and subscribe to the telemetry set:

- `spBv1.0/+/NBIRTH/+`, `spBv1.0/+/NDEATH/+`
- `spBv1.0/+/DBIRTH/+/+`, `spBv1.0/+/DDEATH/+/+`
- `spBv1.0/+/DDATA/+/+`

Allow Engine to process births before data; NBIRTH/DBIRTH provide the online state and metric definitions, while NDEATH/DDEATH mark the corresponding node/device offline. Do not configure command publishing or subscribe to `NCMD` / `DCMD` for this read-only connection.

LGB topic structure is `spBv1.0/<groupID>/<message-type>/<edgeNodeID>[/<deviceID>]`, using the `groupID` and `edgeNodeID` configured on LGB. Each PLC is a Sparkplug device; its configured PLC `name` is the device ID, and configured tag `name` values are the metric names. For example, with group `Factory`, edge node `LGB`, PLC `Line1`, and tag `Temperature`, expect device traffic such as `spBv1.0/Factory/DBIRTH/LGB/Line1` and `spBv1.0/Factory/DDATA/LGB/Line1`. LGB uses Sparkplug B protobuf payloads, not JSON. DBIRTH carries metric types/current values, and subsequent DDATA updates metric values. Bad-quality updates are encoded with Sparkplug `IsNull`.

## 3. Verify tags, without writing

In Ignition Designer, inspect the MQTT Engine tag provider (commonly named `MQTT Engine`) and browse to the configured namespace's group/edge-node/device tree. Confirm the node and device show online after births; inspect the device's `Temperature` (or your configured tag name), verify its value changes as the isolated PLC simulator scans, then stop the simulator/gateway and verify the device becomes stale/offline on death or broker loss. Folder spelling and provider display layout vary by Engine release; the Sparkplug identifiers above are authoritative.

Use tag reads and history/diagnostics only. Do not test writes, create an MQTT publisher, or grant the Ignition identity write/command ACLs. LGB's Sparkplug command channel can trigger PLC writes when separately enabled and authorized; this example intentionally has no such access.

## Troubleshooting

- No devices: check MQTT Engine server connectivity, subscription ACLs, exact group/node IDs, and whether NBIRTH/DBIRTH arrive.
- Device but no metrics: verify the Sparkplug B namespace is enabled and DBIRTH decodes; do not apply a JSON payload transform.
- Certificate failure: provision the correct CA and DNS name/SAN; do not work around it by disabling certificate validation.
- DDATA without a birth: inspect broker retention/session policy and ensure births are delivered before data after reconnect.
