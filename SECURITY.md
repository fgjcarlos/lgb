# Security Policy

LGB is early-stage software intended for industrial environments. Please avoid using it as the only bridge between PLCs and production IIoT systems until the security model has matured. In particular, do not expose LGB to untrusted networks during early releases.

## Transport Security (TLS)

**Production deployments must enable TLS.** Plaintext HTTP exposes authentication
tokens and PLC tag data to anyone on the network path.

Enable TLS via environment variables or YAML:

```bash
LGB_SERVER_TLSENABLED=true
LGB_SERVER_TLSCERTFILE=/path/to/server.crt
LGB_SERVER_TLSKEYFILE=/path/to/server.key
```

When TLS is on, set `LGB_SERVER_ALLOWEDORIGINS` to `https://` prefixes only:

```bash
LGB_SERVER_ALLOWEDORIGINS=https://your-dashboard.example.com
```

Using `http://` patterns when the server serves `wss://` will cause WebSocket origin
checks to fail — every upgrade will be rejected.

LGB enforces a fail-fast gate: if `tlsEnabled: true` but either cert or key file is
empty, the server exits before binding the socket. For certificate sources (Let's
Encrypt, internal CA, self-signed for dev) and full setup steps, see
[docs/deployment.md](docs/deployment.md).

## OPC UA Certificates

`opcua.securityMode: None` is an explicit insecure opt-in. For networked
deployments, use `Sign` or `SignAndEncrypt` and provide an RSA certificate/key
pair. Secure OPC UA modes fail fast when either path is empty or missing.

Example self-signed certificate for local testing:

```bash
openssl req -x509 -nodes -newkey rsa:2048 \
  -keyout opcua-server.key \
  -out opcua-server.crt \
  -days 365 \
  -subj "/CN=lgb-opcua"
```

Then configure LGB:

```yaml
opcua:
  enabled: true
  host: "0.0.0.0"
  port: 4840
  securityMode: "SignAndEncrypt"
  certFile: "/path/to/opcua-server.crt"
  keyFile: "/path/to/opcua-server.key"
```

Use certificates issued by your site CA in production and distribute trust to OPC
UA clients according to your plant's trust-list process. Keep the private key
readable only by the LGB process user.

## HTTP Security Headers

Every HTTP response from LGB includes the following security headers, applied by
`internal/server/middleware_security.go` regardless of route (API, SPA, health,
metrics, WebSocket upgrade):

| Header | Value | Purpose |
|--------|-------|---------|
| `X-Content-Type-Options` | `nosniff` | Prevents browsers from MIME-sniffing a response away from the declared Content-Type, mitigating drive-by download attacks. |
| `X-Frame-Options` | `DENY` | Blocks the LGB UI from being embedded in an iframe, preventing clickjacking attacks. |
| `Referrer-Policy` | `strict-origin-when-cross-origin` | Limits the Referer header to the origin only when crossing security boundaries, reducing referrer-based information leakage. |
| `Content-Security-Policy` | `default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self' ws: wss:; font-src 'self'; object-src 'none'; frame-ancestors 'none'` | Restricts resource loading to the same origin, permits WebSocket connections (ws:/wss:), and denies frame embedding via `frame-ancestors 'none'`. |

These headers are set unconditionally — they apply to both plaintext and TLS
deployments alike. No opt-in or configuration is required.

Implementation: `internal/server/middleware_security.go` — `securityHeadersMiddleware`.

## Reporting a vulnerability

Please do not open a public GitHub issue for security vulnerabilities.

Instead, contact the maintainer privately with:

- A short description of the issue.
- Reproduction steps or proof of concept, if safe to share.
- Affected version, commit, or deployment mode.
- Potential impact, especially anything affecting PLC writes or audit integrity.

If no private contact channel is listed in the repository profile, open a minimal public issue asking for a private security contact without disclosing technical details.

## Scope

Security-sensitive areas include:

- Authentication and session handling.
- Admin user management and role-based access control.
- Per-tag write ACLs and command propagation to PLCs.
- Audit trail integrity.
- OPC UA security modes, certificate handling, and trust lists.
- MQTT Sparkplug B authentication, TLS, and Last Will & Testament behavior.
- Backup repository credentials and encryption keys (restic).
- SQLite database file permissions.
- Container and deployment defaults.

Out of scope:

- Vulnerabilities in upstream dependencies that are not reachable from LGB's exposed surface (please report upstream).
- Issues in the user's PLC ladder logic or external SCADA system.

## Expectations

The maintainer will triage reports as availability allows. Coordinated disclosure is appreciated. LGB controls writes to real industrial equipment — please prioritize responsible disclosure accordingly.
