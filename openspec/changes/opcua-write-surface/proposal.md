# Proposal: OPC UA write surface through the shared write guard

## Intent

The `write-acl` change (PRs #50–#56) built the gateway's write path with two surfaces — HTTP (role×tag ACL) and
Sparkplug DCMD (per-tag `dcmd_enabled`) — and deliberately left OPC UA out of scope. The Phase 1 roadmap exit
criteria require **bidirectional** writes, so this change wires the third surface: OPC UA tag writes go through the
same `internal/writeguard.Guard` the other two surfaces use. No duplicated authorization.

Today the OPC UA server (`internal/opcua`) is a read-only `TagSource`. Worse, gopcua's default Write service is
still registered: a client write is silently accepted, replaces the node's value callback (breaking reads for that
tag) and never reaches the PLC. This change replaces that behaviour with a guarded, audited write path.

## Scope

### In Scope

- `internal/writeguard`: a third enforcement entry point, `AuthorizeOPCUA`, next to `AuthorizeHTTP` and
  `AuthorizeDCMD`. Master switch `Writable` first, then the OPC UA per-tag opt-in. No ACL lookup.
- `internal/config` + `internal/plcstore`: a new per-tag flag `opcua_write_enabled` (default `false`), persisted
  like `dcmd_enabled` and round-tripped through the PLC API.
- `internal/opcua`: a gateway-owned OPC UA **Write** handler that resolves the target node to `(plc, tag)`, asks the
  guard, converts the value to the tag's declared type, writes through the PLC driver and audits every attempt.
  Node `AccessLevel` reflects the effective opt-in so clients see a read-only node unless the tag is writable.
- Wiring in `cmd/lgb/cmd/server.go`: one shared guard instance for HTTP, DCMD and OPC UA.
- Docs: OpenAPI schema, the frontend PLC editor, and an ADR documenting the trust model.

### Out of Scope

- OPC UA **user authentication / session identity** (username+password, certificate user tokens) and any mapping of
  OPC UA sessions to gateway roles. The gateway's OPC UA server runs anonymous user tokens; adding identity is a
  separate change with its own threat model.
- Change-on-Value / subscriptions for write events, OPC UA alarms & conditions, UDT/complex types.
- Role-based authorization for OPC UA writes (see ADR-0010 for the reasoning and the path back to it).

## Capabilities

### New Capabilities

- none

### Modified Capabilities

- `tag-write-acl`: a third enforcement function (`AuthorizeOPCUA`), a third write surface, and audit events whose
  `source` may now be `opcua`.
- `plc`: `TagDef` gains `OPCUAEnabled` (`opcua_write_enabled`), persisted in `plc_tags.opcua_write_enabled`.

## Approach

1. Guard first: `AuthorizeOPCUA` mirrors `AuthorizeDCMD` — master switch, then the per-tag opt-in, no ACL.
2. Flag second: `opcua_write_enabled` rides the same config/store path as `dcmd_enabled`, so the deny-by-default
   posture is identical on a fresh install and on an existing database.
3. Surface third: the gateway registers its own Write handler on the gopcua server **before** `Start`, because
   gopcua's `RegisterHandler` never overrides an already-registered handler and `initHandlers()` runs inside
   `Start`. The handler never mutates the address space; a write either reaches the PLC through the guard or is
   rejected with a UA status code.
4. Wiring last: `cmd` builds one guard and hands the same instance to the HTTP server and to the OPC UA server.

## Affected Areas

| Area | Impact | Description |
|------|--------|-------------|
| `internal/writeguard/guard.go` | Modified | `SourceOPCUA`, `TagMeta.OPCUAEnabled`, `AuthorizeOPCUA` |
| `internal/config/config.go` | Modified | `TagDef.OPCUAEnabled` (`koanf:"opcua_write_enabled"`) |
| `internal/plcstore/store.go` | Modified | `plc_tags.opcua_write_enabled` column + idempotent migration |
| `internal/opcua/` | Modified, New | Write handler, node→tag map, value conversion, audit, AccessLevel |
| `internal/server/api_plcs.go`, `api_write.go`, `server.go` | Modified | API DTO, tag reader adapter, guard wiring |
| `cmd/lgb/cmd/server.go` | Modified | Build one guard; pass it to HTTP server and OPC UA server |
| `frontend/src/` | Modified | `opcua_write_enabled` in the PLC tag editor |
| `docs/api/openapi.yaml`, `docs/adr/0010-*.md` | Modified, New | Schema + trust model |

## Risks

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| A write reaches a tag the operator did not opt in | Low | Deny-by-default flag; guard is the only write path; audit records every attempt |
| gopcua's default Write handler stays registered (handler override silently ignored) | Medium | Register before `Start`; regression test asserts the gateway handler answers a Write request |
| Clients break because `AccessLevel` says read-only | Medium | AccessLevel mirrors the effective opt-in from the same store metadata the guard reads |
| Value typing mismatch silently corrupts the PLC value | Medium | Explicit conversion to the tag's declared type; mismatch is denied and audited |
| Anonymous OPC UA sessions mean "who wrote this" is unknown | Certain (by decision) | Documented in ADR-0010; deployments that need identity must enable OPC UA user auth (out of scope here) |

## Verification

- `go test ./... -race -count=1`
- `go vet ./...`
- `go build ./...` (pure Go, `CGO_ENABLED=0`)
