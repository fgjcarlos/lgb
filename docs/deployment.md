# Deployment Guide

This guide deploys the current checkout using the production Dockerfile, persistent state, HTTPS, and authenticated MQTT over TLS. Compose binds HTTPS to loopback and does not publish the broker. It explicitly mounts `docker/production/lgb.yaml`, matching the image command, and builds source rather than using a stale published image.

## Prerequisites and secrets

Install Docker Engine with Compose plugin, OpenSSL, and Mosquitto utilities (`mosquitto_passwd`). Keep credentials and certificate material outside the checkout. The examples below use documentation-only names and generated ephemeral material: replace `lgb.example.test` with a DNS name you control; never copy a real private key into the repository.

```bash
umask 077
export LGB_HOME="$HOME/.config/lgb"
mkdir -p "$LGB_HOME/https" "$LGB_HOME/mqtt-tls" "$LGB_HOME/mqtt-secret"
chmod 700 "$LGB_HOME" "$LGB_HOME/https" "$LGB_HOME/mqtt-tls" "$LGB_HOME/mqtt-secret"
sudo install -d -m 0700 -o 65532 -g 65532 "$LGB_HOME/data"
openssl rand -hex 32
openssl rand -hex 24
```

Create `$LGB_HOME/.env` (mode `0600`) with unique generated values; never reuse secrets:

```dotenv
LGB_AUTH_JWTSECRET=<generated-32-byte-secret>
LGB_AUTH_ADMIN_USER=admin
LGB_AUTH_ADMIN_PASSWORD=<generated-password-at-least-12-characters>
LGB_TLS_DIR=/absolute/path/to/.config/lgb/https
LGB_MQTT_TLS_DIR=/absolute/path/to/.config/lgb/mqtt-tls
LGB_MQTT_SECRET_DIR=/absolute/path/to/.config/lgb/mqtt-secret
LGB_DATA_DIR=/absolute/path/to/.config/lgb/data
```

The MQTT identity is intentionally fixed as `lgb`, matching `docker/production/acl`; it is not configurable through an environment variable. Generate a distinct MQTT password and save it in a password manager. Compose consumes it from `mqtt-secret/lgb-password` while Mosquitto consumes its hashed equivalent from `mqtt-secret/passwd`:

```bash
umask 077
printf '%s\n' '<generated-mqtt-password>' > "$LGB_HOME/mqtt-secret/lgb-password"
mosquitto_passwd -c "$LGB_HOME/mqtt-secret/passwd" lgb
chmod 640 "$LGB_HOME/mqtt-secret/lgb-password" "$LGB_HOME/mqtt-secret/passwd"
```

Enter the same generated MQTT password when prompted by `mosquitto_passwd`. Protect `.env` with `chmod 600 "$LGB_HOME/.env"`; do not print it in troubleshooting output or put it in the repository.

## TLS certificates and permissions

Create a private local CA and server certificates with SANs matching the endpoints clients actually use. The following recipe produces a CA and one certificate usable for local HTTPS and the broker: add all actual DNS/IP SANs as needed. For public or production deployments, use your organization-approved CA and renew/rotate according to its policy.

```bash
mkdir -p "$LGB_HOME/pki"
cd "$LGB_HOME/pki"
openssl req -x509 -newkey rsa:3072 -nodes -days 3650 -keyout ca.key -out ca.crt \
  -subj '/CN=LGB local documentation CA'
openssl req -newkey rsa:3072 -nodes -keyout broker.key -out broker.csr \
  -subj '/CN=lgb.example.test'
printf 'subjectAltName=DNS:lgb.example.test,DNS:localhost,DNS:mosquitto,IP:127.0.0.1\nextendedKeyUsage=serverAuth\n' > server.ext
openssl x509 -req -in broker.csr -CA ca.crt -CAkey ca.key -CAcreateserial \
  -out broker.crt -days 365 -sha256 -extfile server.ext
install -m 0644 broker.crt "$LGB_HOME/https/server.crt"
install -m 0640 broker.key "$LGB_HOME/https/server.key"
install -m 0644 ca.crt "$LGB_HOME/https/ca.crt"
install -m 0644 ca.crt "$LGB_HOME/mqtt-tls/ca.crt"
install -m 0644 broker.crt "$LGB_HOME/mqtt-tls/broker.crt"
install -m 0640 broker.key "$LGB_HOME/mqtt-tls/broker.key"
cd -
```

The certificate must have `subjectAltName` for every HTTPS hostname/IP clients use, and `DNS:mosquitto` because LGB verifies the broker at that Compose hostname. For a real deployment, issue separate HTTPS and broker certificates with only their required SANs. The compose references above need `server.crt`/`server.key` in the HTTPS directory and `ca.crt`/`broker.crt`/`broker.key` in the MQTT TLS directory. The example CA key and signing files stay outside both mounted runtime directories; remove them securely according to your host policy after issuance.

The LGB container runs as UID/GID `65532`; Mosquitto runs as `1883:1883`. Read-only file mounts avoid exposing unrelated private files from shared certificate/secret directories. Ensure the LGB process can read the HTTPS key and MQTT password, and Mosquitto can read the broker key and hashed password. A portable group-based setup is:

```bash
sudo chown 65532:65532 "$LGB_HOME/https/server.key" "$LGB_HOME/mqtt-secret/lgb-password"
sudo chmod 640 "$LGB_HOME/https/server.key" "$LGB_HOME/mqtt-secret/lgb-password"
sudo chown 1883:1883 "$LGB_HOME/mqtt-tls/broker.key" "$LGB_HOME/mqtt-secret/passwd"
sudo chmod 640 "$LGB_HOME/mqtt-tls/broker.key" "$LGB_HOME/mqtt-secret/passwd"
sudo chmod 644 "$LGB_HOME/https/server.crt" "$LGB_HOME/mqtt-tls/ca.crt" "$LGB_HOME/mqtt-tls/broker.crt"
namei -l "$LGB_HOME/https/server.key" "$LGB_HOME/mqtt-tls/broker.key" "$LGB_HOME/mqtt-secret/lgb-password" "$LGB_HOME/mqtt-secret/passwd"
```

Adjust group ownership/ACLs for your host rather than making private keys or passwords world-readable. The Docker daemon must be able to resolve the source paths. These UID/GID steps assume a rootful Linux daemon without user-namespace remapping; use the daemon's mapped IDs if applicable. Compose does not repair bind-mount permissions.

## First start and bootstrap

On an empty data directory, LGB requires `LGB_AUTH_ADMIN_PASSWORD` (minimum 12 characters, maximum 72 bytes) to create the first admin. Once created, remove the admin password from `.env`; the Compose setting defaults to empty, so existing installations can start without it. Do not delete persisted data to reset credentials. Recreate LGB after removing the bootstrap password so it no longer appears in the container environment.

Run from repository root:

```bash
docker compose --env-file "$LGB_HOME/.env" config --quiet
docker compose --env-file "$LGB_HOME/.env" build lgb
docker compose --env-file "$LGB_HOME/.env" up -d
```

The UI is `https://localhost:8443/`; use a name covered by the HTTPS certificate in production. Check status/logs and verify TLS without disabling certificate checks:

```bash
docker compose --env-file "$LGB_HOME/.env" ps
docker compose --env-file "$LGB_HOME/.env" logs --tail=100 lgb mosquitto
curl --fail --show-error --cacert "$LGB_HOME/https/ca.crt" https://localhost:8443/health
```

For HTTPS hostnames other than localhost, use that hostname in the URL and configure DNS/hosts accordingly. The broker is internal-only; external consumers should use a separately managed secured broker or a reviewed override with firewall restrictions, per-user credentials, TLS, and narrow ACLs. The ACL grants only user `lgb` read/write under `spBv1.0/#`; consumers need separate users and narrow topic rules.

## Behind a reverse proxy

By default lgb trusts nobody: the login rate limit (20 attempts per minute per IP, IPv6 grouped by /64) and audit events use the TCP peer address, and `X-Forwarded-For` / `Forwarded` headers are ignored. Behind nginx, Traefik, or a load balancer every client would then share the proxy's address and its rate-limit bucket.

List the proxies' addresses as CIDRs in `server.trustedProxies` (or `LGB_SERVER_TRUSTEDPROXIES`, comma-separated):

```yaml
server:
  trustedProxies:
    - "10.0.0.0/8"
    - "fd00::/8"
```

Only when the peer is inside one of these CIDRs does lgb read the forwarding header, walking it right to left and using the first hop that is not itself a trusted proxy. `X-Forwarded-For` takes precedence; `Forwarded` (RFC 7239 `for=`) is used only when `X-Forwarded-For` is absent, so the proxy must append to or overwrite the header lgb reads (nginx `proxy_add_x_forwarded_for`, Traefik's default) and must not pass a client-supplied `X-Forwarded-For` through untouched while setting only `Forwarded`. An unparseable hop (for example `unknown`) stops the walk at the last trusted address. Never list a range that untrusted clients can connect from: any such client could then choose its own IP. The setting is read at startup; restart after changing it.

## Persistent data and operations

The host directory `LGB_DATA_DIR` is mounted at `/var/lib/lgb` and holds gateway state and the historian. Mosquitto persistence uses `mosquitto-data`. Back up both and test restores before upgrades. `docker compose down` retains data; `down -v` removes the broker volume. Renew external certificates with permissions preserved and recreate the relevant service so replaced files are bind-mounted again. Review [Backup and restore](backup-restore.md) and the [repository security policy](https://github.com/fgjcarlos/lgb/blob/main/SECURITY.md) before production use.
