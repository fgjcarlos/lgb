# ADR-0010: OPC UA write trust model — per-tag opt-in, not session identity

**Date**: 2026-11-10
**Status**: Proposed

## Decision

Authorize OPC UA tag writes by **configuration, not identity**: a write succeeds only when the target tag has
`writable == true` AND the new per-tag flag `opcua_write_enabled == true`. The OPC UA write path runs through the
shared `internal/writeguard.Guard` (`AuthorizeOPCUA`), exactly like the HTTP and Sparkplug DCMD surfaces, and it
never consults the role×tag ACL.

The `opcua_write_enabled` flag is independent of `dcmd_enabled` and of the ACL: enabling one surface never enables
another, and an operator's HTTP ACL rule never authorizes an OPC UA write.

## Context

Issue #58 asks to "Decide the OPC UA trust/identity model (like DCMD, OPC UA may be a non-JWT principal — choose: a
dedicated per-tag opt-in à la `dcmd_enabled`, or map authenticated OPC UA sessions to roles)".

The gateway's OPC UA server is created with `opcserver.EnableAuthMode(ua.UserTokenTypeAnonymous)`
(`internal/opcua/server.go`), so an OPC UA session carries **no authenticated principal**. `SecurityMode` may be
`None`, `Sign` or `SignAndEncrypt`; the latter two authenticate and encrypt the *channel* with application
certificates, but they do not produce a gateway user identity either. Mapping sessions to gateway roles would
therefore require a new authentication capability (user tokens, certificate-to-user mapping, a role table) that
does not exist yet.

## Options Considered

| Option | Pros | Cons |
|--------|------|------|
| Per-tag opt-in `opcua_write_enabled` (chosen) | Deny-by-default; consistent with the DCMD path; no new identity system; auditable and cheap to reason about | "Who wrote this" is unknown — the audit event carries no actor |
| Map authenticated OPC UA sessions to roles | Real attribution; per-user permissions | Requires OPC UA user authentication (out of scope here); role mapping table; certificate lifecycle; larger threat surface |
| Reuse `dcmd_enabled` for OPC UA | No new flag | Conflates two surfaces with different trust properties; a DCMD opt-in would silently open an OPC UA write path |
| Reuse the role×tag ACL for OPC UA | One permission model | The ACL is keyed by gateway role; an anonymous OPC UA session has no role, so the ACL could not be evaluated without faking a principal |
| Keep OPC UA read-only | Simplest | Phase 1 requires bidirectional writes |

## Rationale

An authorization decision needs an input. With anonymous user tokens the only inputs available are configuration
and the transport. Configuration is the one the gateway already treats as the safety root for writes: `Writable` is
the master switch on every surface, and `dcmd_enabled` is the precedent for a per-surface opt-in that cannot be
lifted by another surface's permission. Mirroring that precedent keeps one mental model for operators — "a tag is
writable over OPC UA only if I say so" — and keeps the fail-safe property that a fresh or upgraded install exposes no
new write path until an operator opts in explicitly.

Silently reusing `dcmd_enabled` or the role×tag ACL would produce the opposite property: a permission granted for
one surface would open a second surface. That is the failure mode this ADR exists to prevent.

The cost is honest and must be stated plainly: **the gateway cannot attribute an OPC UA write to a person.** The
audit event records `source=opcua` with an empty actor, exactly like DCMD.

## Consequences

- **Accepted**: OPC UA writes are authorized by `Writable && opcua_write_enabled`; no ACL lookup on this path.
- **Accepted**: audit events for OPC UA writes carry no actor.
- **Operational security**: deployments that expose OPC UA writes to a network should use
  `securityMode: Sign` or `SignAndEncrypt` with real certificates. This change does not enforce that recommendation
  or emit a dedicated warning; with `securityMode: None`, any client able to reach the port can write opted-in tags.
- **Consequence**: an operator who enables the flag accepts "any client that can reach the OPC UA port may write this
  tag". The per-tag flag is the only control; the flag must be set on as few tags as possible.
- **Revisit**: when OPC UA user authentication lands, revisit this ADR. The migration path is to add a session→role
  mapping as a *third* gate in `AuthorizeOPCUA`, keeping `opcua_write_enabled` as the surface-level opt-in; the
  per-tag flag does not need to be removed.

## References

- Issue #58 — `feat(opcua): wire OPC UA write surface through the write guard`
- ADR-0005 — OPC UA library (`gopcua/opcua` v0.8.0)
- `openspec/specs/tag-write-acl/spec.md` — the shared enforcement core and its audit contract
- `openspec/changes/opcua-write-surface/` — proposal, design and spec deltas
