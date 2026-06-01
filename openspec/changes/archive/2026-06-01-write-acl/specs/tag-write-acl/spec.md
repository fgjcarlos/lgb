---
change: write-acl
phase: spec
domain: tag-write-acl
date: 2026-05-31
status: draft
type: new
---

# Tag Write ACL Specification

## Purpose

Defines the `internal/aclstore` package (SQLite role×tag ACL store), the shared `Authorize` enforcement core, the HTTP tag-write endpoint, the Sparkplug DCMD write surface, and the admin ACL management API. This capability is the first live write path for the LGB gateway. All write attempts — regardless of surface — go through one enforcement path and are audited.

---

## Requirements

### [TWA-STORE-1.1] ACL store — schema and open

The `internal/aclstore` package MUST expose a `Store` type backed by `modernc.org/sqlite` (pure-Go). On `Open(ctx, path)` the store MUST:

1. Issue `PRAGMA foreign_keys = ON`.
2. Run migrate-on-open, creating the `tag_acl` table if it does not exist.
3. Call `SetMaxOpenConns(1)` to serialise writes and keep the PRAGMA stable.

| Table | Columns |
|-------|---------|
| `tag_acl` | `id INTEGER PK AUTOINCREMENT`, `role TEXT NOT NULL`, `plc TEXT NOT NULL`, `tag TEXT NOT NULL`, `allow_write INTEGER NOT NULL DEFAULT 0`, `UNIQUE(role, plc, tag)` |

`CGO_ENABLED=0` MUST remain valid. The surrogate `id` is internal; it is NOT exposed in any API response.

#### Scenario: Table created on first open

- GIVEN no `acl.db` file exists at `path`
- WHEN `Open(ctx, path)` is called
- THEN the file is created
- AND the `tag_acl` table exists in the schema

#### Scenario: Open is idempotent

- GIVEN `acl.db` already exists with the `tag_acl` table
- WHEN `Open(ctx, path)` is called again
- THEN it returns without error and existing rows are intact

#### Scenario: In-memory seam works for tests

- GIVEN `path` is `":memory:"`
- WHEN `Open(ctx, ":memory:")` is called
- THEN it returns a fully functional store with zero rows
- AND all CRUD operations work without filesystem access

---

### [TWA-STORE-1.2] IsEmpty and Seed

The store MUST expose `IsEmpty(ctx) (bool, error)` and `Seed(ctx, rules []ACLRule) error`. `Seed` MUST be a no-op when `rules` is empty OR when the store is already non-empty (idempotent).

---

### [TWA-STORE-1.3] CRUD — Create rule

The store MUST expose `CreateRule(ctx, r ACLRule) error`. Duplicate `(role, plc, tag)` MUST return `ErrRuleAlreadyExists`. Role MUST be one of `admin`, `operator`, `viewer`; an invalid role MUST return `ErrInvalidRole`.

---

### [TWA-STORE-1.4] CRUD — Read rules

The store MUST expose `ListRules`, `GetRule`, `ListRulesByRole`.

---

### [TWA-STORE-1.5] CRUD — Update and Delete rule

The store MUST expose `UpdateRule` and `DeleteRule`.

---

### [TWA-STORE-1.6] CanWrite lookup

The store MUST expose `CanWrite(ctx, role, plc, tag string) (bool, error)`. Returns `(true, nil)` only when an exact-match row with `allow_write=1` exists.

---

### [TWA-ENFORCE-2.1] Authorize — source-dispatched enforcement core

(See full spec at openspec/specs/tag-write-acl/spec.md — this is the original delta artifact.)

---

### [TWA-ENFORCE-2.2] Deny-by-default (empty ACL — HTTP path)

With no ACL rules configured, every HTTP write MUST be denied.

---

### [TWA-ENFORCE-2.3] Tag existence validation

Before calling `Authorize`, the write handler MUST verify that the requested tag exists. Unknown tag → 404 `tag_not_found`.

---

### [TWA-HTTP-3.1] HTTP write endpoint

`POST /api/plcs/{plc}/tags/{tag}/write`, gated by `auth.RequireRole(RoleAdmin, RoleOperator, RoleViewer)`.

---

### [TWA-DCMD-3.2] Sparkplug DCMD write surface

Gate: `Writable=true AND DCMDEnabled=true`. No ACL consultation. Silent drop on deny. Full audit.

---

### [TWA-AUDIT-4.1] Audit every write attempt

Every attempt emits `AuditEvent{Action:"tag.write", Detail:"plc=... tag=... value=... outcome=... source=..."}`. Flat key=value, NOT JSON.

---

### [TWA-API-5.1] Admin ACL CRUD API

`/api/acl/rules` (5 endpoints, admin-only, audit on mutations).
