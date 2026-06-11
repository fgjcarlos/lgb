# Deployment Guide

This document covers production deployment considerations for LGB (Logix Gateway Bridge).

## Transport Security — TLS (R72)

LGB can serve its HTTP API over TLS. TLS is strongly recommended for any deployment
that is reachable from outside a strictly controlled local network.

### Enabling TLS

Set the following environment variables (or the equivalent YAML fields):

```bash
LGB_SERVER_TLSENABLED=true
LGB_SERVER_TLSCERTFILE=/path/to/server.crt
LGB_SERVER_TLSKEYFILE=/path/to/server.key
```

Or in `lgb.yaml`:

```yaml
server:
  tlsEnabled: true
  tlsCertFile: /path/to/server.crt
  tlsKeyFile: /path/to/server.key
```

LGB validates at startup that both `tlsCertFile` and `tlsKeyFile` are non-empty when
`tlsEnabled: true`. If either file path is missing the server exits immediately with
an error before binding the socket (fail-fast gate).

### Allowed Origins

When TLS is enabled the frontend and any external clients connect over HTTPS/WSS.
Update `server.allowedOrigins` accordingly — use `https://` prefixes, **not** `http://`:

```bash
LGB_SERVER_ALLOWEDORIGINS=https://dashboard.example.com,https://grafana.example.com
```

Or in `lgb.yaml`:

```yaml
server:
  allowedOrigins:
    - https://dashboard.example.com
    - https://grafana.example.com
```

> Note: the frontend auto-selects `wss://` when served over HTTPS — no manual
> configuration is required on the frontend side.

### Certificate Sources

LGB loads cert/key files directly via Go's `crypto/tls` package. Any PEM-format
X.509 certificate is supported:

- Self-signed certs (development / closed networks)
- Certificates from a private CA
- Certificates from a public CA (e.g. Let's Encrypt via certbot or acme.sh)

For automated renewal with Let's Encrypt, use a reverse proxy (Nginx, Caddy, Traefik)
in front of LGB and terminate TLS there, or use a tool such as
[autocert](https://pkg.go.dev/golang.org/x/crypto/acme/autocert) via a custom build.

### Running Without TLS (Development Only)

When `tlsEnabled: false` (the default), LGB logs a warning at startup:

```
WARN server running WITHOUT TLS — plaintext HTTP addr=:8080
```

This warning is intentional. Plaintext HTTP is acceptable on loopback or in a fully
isolated development environment but should never be used in production.

## Container Deployment

The production Docker image (`docker/Dockerfile`) embeds the compiled `lgb` binary.
Mount your TLS cert and key as container secrets or volumes:

```bash
docker run -d \
  -e LGB_SERVER_TLSENABLED=true \
  -e LGB_SERVER_TLSCERTFILE=/run/secrets/server.crt \
  -e LGB_SERVER_TLSKEYFILE=/run/secrets/server.key \
  -v /path/to/certs:/run/secrets:ro \
  -p 8443:8080 \
  lgb:latest
```

## Data Directory

LGB stores its SQLite historian database and other persistent data in the directory
set by `gateway.dataDir` (or `LGB_GATEWAY_DATADIR`). Ensure this directory is:

- Persistent across container restarts (bind mount or named volume)
- Not world-readable (SQLite files contain tag history)
