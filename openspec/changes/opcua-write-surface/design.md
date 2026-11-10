# Design: OPC UA write surface

## 1. Trust model (the decision the issue asks for)

The issue offers two options: a dedicated per-tag opt-in à la `dcmd_enabled`, or mapping authenticated OPC UA
sessions to roles.

**Decision: dedicated per-tag opt-in (`opcua_write_enabled`), deny-by-default.**

Why role mapping is not available today:

- The gateway's OPC UA server is created with `opcserver.EnableAuthMode(ua.UserTokenTypeAnonymous)`
  (`internal/opcua/server.go`), so a session carries **no authenticated principal**. There is nothing to map to a
  gateway role.
- `SecurityMode` may be `None`, `Sign` or `SignAndEncrypt`; the latter two authenticate and encrypt the *channel*
  (application certificates) but still do not produce a gateway user identity.
- Introducing OPC UA user authentication (user tokens, certificate mapping, a role table) is a separate capability
  with its own threat model and its own change.

Consequences of the decision (all deliberate):

- An OPC UA write is authorized by **configuration**, not by identity: `Writable == true` AND
  `opcua_write_enabled == true`.
- The flag is independent from `dcmd_enabled` and from the role×tag ACL. An HTTP ACL rule never authorizes an OPC UA
  write; `dcmd_enabled` never authorizes an OPC UA write.
- Audit records `source=opcua` with an empty actor, exactly like the DCMD path: there is no principal to record.
- A deployment that exposes OPC UA writes to the network MUST use `securityMode: Sign|SignAndEncrypt` with
  certificates. The gateway logs a WARN at startup when OPC UA writes are enabled while `securityMode: None`.

The path back to identity-based authorization is documented in `docs/adr/0010-opcua-write-trust-model.md`.

## 2. Enforcement core

`internal/writeguard` gains a third top-level function, mirroring the existing two-function shape (the type
signatures make it impossible to call the wrong gate for a surface):

```go
// AuthorizeOPCUA enforces the OPC UA write gate. There is NO actor, NO role,
// and NO ACL lookup for this path — the OPC UA server runs anonymous user
// tokens, so there is no principal to authorize.
func (g *Guard) AuthorizeOPCUA(ctx context.Context, plcName, tagName string) Decision
```

- Gate 1 — master switch: `!meta.Writable` → `Decision{Allowed: false, Reason: "tag not writable"}`.
- Gate 2 — surface gate: `!meta.OPCUAEnabled` → `Decision{Allowed: false, Reason: "opcua not enabled"}`.
- Unknown tag → deny (`Reason: "tag not writable"`), like the other two paths.
- `aclstore.CanWrite` is never called on this path.

`TagMeta` gains `OPCUAEnabled bool`; `Source` gains `SourceOPCUA` (the audit `source=` string is `opcua`).

## 3. The per-tag flag

| Layer | Change |
|-------|--------|
| `internal/config` | `TagDef.OPCUAEnabled bool` with `koanf:"opcua_write_enabled"`, default `false` |
| `internal/plcstore` | `plc_tags.opcua_write_enabled INTEGER NOT NULL DEFAULT 0`; idempotent `ALTER TABLE` (same "duplicate column name" tolerance as `dcmd_enabled`); `listTags`/`insertTags` round-trip |
| `internal/server/api_plcs.go` | `plcTagResponse` gains `opcua_write_enabled`; request → `config.TagDef` mapping carries it |
| `frontend/src` | `opcua_write_enabled` in the tag schema, the form default and the per-tag checkbox |

Existing rows are **not** rewritten: the added column defaults to `0`, so every existing tag stays read-only over
OPC UA until an operator opts in.

## 4. The OPC UA write surface

### 4.1 Why a custom Write handler

gopcua v0.8.0 registers the default Write service (`AttributeService.Write`) in `initHandlers()`, which runs inside
`Server.Start(ctx)`. Two facts shape the implementation:

1. `Server.RegisterHandler` **does not override** an existing entry (`if _, ok := s.handlers[typeID]; !ok`). The
   gateway must therefore register its handler **before** `srv.Start(ctx)`; `initHandlers()` then skips the default.
2. The default handler does `ns.SetAttribute(...)`, which replaces the node's value callback. A client write today
   is accepted, silently breaks the read path of that tag, and never reaches the PLC. Replacing it removes that
   hazard.

```go
srv.RegisterHandler(id.WriteRequest_Encoding_DefaultBinary, s.handleWriteRequest)
```

### 4.2 Node → (plc, tag) resolution

The address space is built in `populateAddressSpace()`: one namespace per PLC (`urn:lgb:plc:<name>`), one variable
node per tag, with `ua.NewStringNodeID(ns.ID(), tag.Name)`. Population also records, per namespace id, the PLC
name; the node's string identifier is the tag name. The write handler resolves a `WriteValue` to `(plc, tag)` from
that map — no string parsing of namespace URIs at write time.

| Write target | Status |
|--------------|--------|
| Unknown node id | `ua.StatusBadNodeIDUnknown` |
| Attribute other than `AttributeIDValue` | `ua.StatusBadAttributeIDInvalid` |
| Value not convertible to the tag type | `ua.StatusBadTypeMismatch` |
| Guard denies | `ua.StatusBadUserAccessDenied` |
| PLC write error | `ua.StatusBadInternalError` |
| Allowed and written | `ua.StatusOK` |

### 4.3 Value conversion

The OPC UA variant carries a Go value (`float64`, `int64`, `bool`, `string`, …). Before it reaches the PLC driver it
is converted to the tag's declared type (`Boolean`, `Int8`…`UInt64`, `Float`, `Double`, `String` — the same set
`internal/config` validates). A value that does not convert is denied (`ua.StatusBadTypeMismatch`), audited as a
deny with `reason="value type mismatch"`, and never reaches the PLC.

### 4.4 Audit

One `auth.AuditEvent` per attempt, allow and deny alike, emitted **before** the response is returned:

```
Action:   "tag.write"
Username: ""            // no authenticated principal (ADR-0010)
Detail:   "plc=<plc> tag=<tag> value=<value> outcome=<allow|deny> source=opcua [reason=<reason>]"
```

This extends the `tag-write-acl` audit contract: `source` may now be `http`, `dcmd` or `opcua`.

### 4.5 AccessLevel

Each variable node gets an explicit `AccessLevel` attribute:

- `CurrentRead` when the tag is not OPC UA-writable,
- `CurrentRead | CurrentWrite` when it is.

The level is derived from the same metadata the guard reads (the PLC config store), so a client that inspects the
address space sees exactly what the gateway will accept. The guard remains the enforcement point; AccessLevel is a
client hint.

### 4.6 Dependencies and nil-safety

`opcua.New` gains an options struct (or equivalent) carrying:

| Dependency | Type | Nil behaviour |
|------------|------|---------------|
| Write guard | `*writeguard.Guard` | no guard → every write denied (`ua.StatusBadUserAccessDenied`) + audit deny |
| PLC writer | `writeguard.TagWriter` | no writer → every write denied + audit deny |
| Audit sink | `writeguard.AuditSink` | nil → audit calls are no-ops (as elsewhere) |

The read-only server keeps working exactly as before when these are absent.

## 5. Wiring

`cmd/lgb/cmd/server.go` already opens `plcStore` and `aclStore` before it creates the OPC UA server, so it can build
the single shared guard there and hand the same instance to both consumers:

```go
guard := writeguard.NewGuard(server.NewPLCStoreTagReader(plcStore), aclStore)
// server.New(..., server.Opts{WriteGuard: guard, ...})
// opcua.New(cfg, tagSrc, logger, opcua.Options{Guard: guard, Writer: plcMgr, Audit: auditLog})
```

`internal/server` exports the store→`writeguard.TagReadable` adapter (`plcstoreTagReader` becomes
`NewPLCStoreTagReader`) so `cmd` can build the guard without duplicating the adapter. `server.New` keeps its
auto-construction fallback for tests that do not inject a guard.

## 6. Test strategy

Test-first, behaviour-level, through the public surface of each package:

| Behaviour | Test |
|-----------|------|
| `Writable=false` denies an OPC UA write | `internal/writeguard` unit test |
| `opcua_write_enabled=false` denies even with `Writable=true` | `internal/writeguard` unit test |
| `Writable=true` + opt-in → allow, and `acl.CanWrite` is never called | `internal/writeguard` unit test |
| The flag defaults to false, loads from YAML and round-trips | `internal/config`, `internal/plcstore` tests |
| The column is added to a legacy database without data loss | `internal/plcstore` test (mirrors the `dcmd_enabled` test) |
| A Write request for a writable tag reaches the PLC writer | `internal/opcua` test |
| A Write request for a non-opted-in tag is denied and audited | `internal/opcua` test |
| An unknown node id and a bad attribute id are rejected | `internal/opcua` test |
| A value that does not match the tag type is denied | `internal/opcua` test |
| The API exposes `opcua_write_enabled` | `internal/server` test |

The OPC UA tests drive the handler through an exported seam (a method that takes a `*ua.WriteRequest` and returns a
`*ua.WriteResponse`), so they assert real behaviour without a live OPC UA session.
