# Security Policy

LGB is early-stage software intended for industrial environments. Please avoid using it as the only bridge between PLCs and production IIoT systems until the security model has matured. In particular, do not expose LGB to untrusted networks during early releases.

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
