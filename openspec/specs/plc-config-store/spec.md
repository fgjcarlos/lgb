---
domain: plc-config-store
date: 2026-05-30
status: active
type: new
---

# PLC Config Store Specification

## Purpose

Defines the `internal/plcstore` package: SQLite-backed persistence for PLC configuration, seed-on-first-run from YAML, audited REST CRUD endpoints under `/api/plcs` (reads viewer+, mutations admin-only), and the store-as-source-of-truth read path. This package is the authoritative runtime source for all PLC definitions after first boot. PLC `name` is the natural/public key; no surrogate `id` is exposed in the API.

---

## Requirements

### [PCS-STORE-1.1] SQLite store — schema and open

The `internal/plcstore` package MUST expose a `Store` type backed by `modernc.org/sqlite` (pure-Go). On `Open(ctx, path)` the store MUST issue `PRAGMA foreign_keys = ON` and run migrate-on-open, creating two tables if they do not exist:

| Table | Key columns |
|-------|-------------|
| `plcs` | `id INTEGER PK AUTOINCREMENT`, `name TEXT UNIQUE NOT NULL`, `address TEXT NOT NULL`, `slot INTEGER`, `socket_timeout TEXT`, `scan_rate TEXT`, `keep_alive INTEGER`, `path TEXT`, `created_at INTEGER`, `updated_at INTEGER` |
| `plc_tags` | `id INTEGER PK AUTOINCREMENT`, `plc_id INTEGER FK plcs(id) ON DELETE CASCADE`, `name TEXT NOT NULL`, `type TEXT NOT NULL`, `writable INTEGER DEFAULT 0`, `UNIQUE(plc_id, name)` |

`SetMaxOpenConns(1)` MUST be called after open to serialise writes (and to keep the `PRAGMA foreign_keys = ON` setting stable on the single connection). `CGO_ENABLED=0` MUST remain valid. The surrogate `id` is an internal autoincrement primary key used only for the `plc_tags` foreign-key relationship; it is NOT exposed in any API response.

#### Scenario: Tables created on first open

- GIVEN no `plcs.db` file exists at `path`
- WHEN `Open(ctx, path)` is called
- THEN the file is created
- AND both `plcs` and `plc_tags` tables exist in the schema

#### Scenario: Open is idempotent

- GIVEN `plcs.db` already exists with both tables
- WHEN `Open(ctx, path)` is called again
- THEN it returns without error and existing rows are intact

---

### [PCS-STORE-1.2] Seed-on-first-run from YAML

The system MUST seed the store exactly once from `cfg.PLCs` when the store is empty at startup. The store MUST expose `IsEmpty(ctx) (bool, error)` and `Seed(ctx, plcs)`. `Seed` MUST be a no-op when `plcs` is empty OR when the store is already non-empty (idempotent). After seeding, subsequent boots MUST NOT overwrite or re-import from YAML even if `plcs.db` is present.

#### Scenario: Empty store seeds from YAML

- GIVEN the store contains zero PLC rows
- AND `cfg.PLCs` has two PLC entries
- WHEN `Seed(ctx, cfg.PLCs)` is called
- THEN the store contains exactly two PLCs with their tags

#### Scenario: Non-empty store ignores seed call

- GIVEN the store contains at least one PLC row
- AND `cfg.PLCs` has different PLC entries
- WHEN `Seed(ctx, cfg.PLCs)` is called
- THEN the store rows are unchanged (seed is a no-op)

#### Scenario: Deleting plcs.db re-seeds on next boot

- GIVEN `plcs.db` is deleted
- WHEN the gateway restarts with `cfg.PLCs` containing one entry
- THEN the store is seeded from YAML on that boot
- AND subsequent boots do not re-seed

---

### [PCS-STORE-1.3] CRUD — Create PLC

The store MUST expose `Create(ctx, p config.PLC) error`. On success it MUST insert both the PLC row and its associated `plc_tags` rows in a single transaction. Duplicate `name` MUST return the sentinel `ErrPLCAlreadyExists`. `Create` returns only `error`; callers that need the canonical stored form re-fetch via `Get`.

#### Scenario: Create a valid PLC

- GIVEN the store is empty
- WHEN `Create` is called with a valid PLC
- THEN it returns nil
- AND `List` includes the new PLC with its tags

#### Scenario: Duplicate name returns ErrPLCAlreadyExists

- GIVEN a PLC named `"PLC-A"` exists
- WHEN `Create` is called with `name="PLC-A"`
- THEN it returns an error satisfying `errors.Is(err, ErrPLCAlreadyExists)`

---

### [PCS-STORE-1.4] CRUD — Read PLCs

The store MUST expose:
- `List(ctx) ([]config.PLC, error)` — returns all PLCs ordered by name with their tags populated (tags ordered by insertion / `plc_tags.id`)
- `Get(ctx, name string) (config.PLC, error)` — returns one PLC by name with tags; missing name returns `ErrPLCNotFound`

#### Scenario: List returns all rows with tags

- GIVEN two PLCs each with two tags in the store
- WHEN `List(ctx)` is called
- THEN the returned slice has length 2
- AND each PLC's `Tags` slice has length 2

#### Scenario: Get unknown name returns ErrPLCNotFound

- GIVEN no PLC named `"ghost"` exists
- WHEN `Get(ctx, "ghost")` is called
- THEN it returns `ErrPLCNotFound`

---

### [PCS-STORE-1.5] CRUD — Update PLC

The store MUST expose `Update(ctx, name string, p config.PLC) error`. The update MUST replace all scalar fields AND replace the PLC's tags atomically (delete old tags, insert new tags) in one transaction. Missing name MUST return `ErrPLCNotFound`. A rename collision (the new `name` already belongs to a different PLC) MUST return `ErrPLCAlreadyExists`. `Update` returns only `error`; callers re-fetch via `Get` for the canonical form.

#### Scenario: Update scanRate persists correctly

- GIVEN a PLC named `"Silo-1"` with `scanRate="1s"`
- WHEN `Update(ctx, "Silo-1", p)` is called with `scanRate="500ms"`
- THEN `Get(ctx, "Silo-1")` returns the PLC with `scanRate="500ms"`
- AND the tag set reflects any tag changes passed in the update

#### Scenario: Update non-existent PLC returns ErrPLCNotFound

- GIVEN no PLC named `"ghost"`
- WHEN `Update(ctx, "ghost", ...)` is called
- THEN it returns `ErrPLCNotFound`

---

### [PCS-STORE-1.6] CRUD — Delete PLC

The store MUST expose `Delete(ctx, name string) error`. Deletion MUST cascade to `plc_tags` (via `ON DELETE CASCADE` plus an explicit child delete inside the transaction as a belt-and-suspenders guard). Missing name MUST return `ErrPLCNotFound`.

#### Scenario: Delete removes PLC and its tags

- GIVEN PLC `"Silo-1"` with three tags
- WHEN `Delete(ctx, "Silo-1")` is called
- THEN `Get(ctx, "Silo-1")` returns `ErrPLCNotFound`
- AND no rows in `plc_tags` reference the deleted PLC's id

#### Scenario: Delete unknown name returns ErrPLCNotFound

- GIVEN no PLC named `"ghost"`
- WHEN `Delete(ctx, "ghost")` is called
- THEN it returns `ErrPLCNotFound`

---

### [PCS-STORE-1.7] Validation runs in the API mutation handler

The store does NOT validate PLC input. Validation is the responsibility of the API mutation handler, which MUST call `config.ValidatePLC(p)` before any store write (`Create` or `Update`). On a validation failure the handler MUST return `400 Bad Request` with error code `invalid_plc` and MUST NOT call the store. `config.ValidatePLC` applies the following per-PLC rules:

| Rule | Condition |
|------|-----------|
| `address` non-empty | Always required |
| `scanRate` valid duration > 0 | If non-empty |
| `socketTimeout` valid duration > 0 | If non-empty |
| `slot` in range 0–15 | Always |
| Each tag `name` non-empty | Per-tag |
| Each tag `type` non-empty / valid Sparkplug scalar type | Per-tag |

Tag `writable` is accepted without validation (stored, not enforced — see PCS-CFG-5.1). There is NO `ErrValidation` sentinel in `plcstore`; validation errors come from `config.ValidatePLC` and wrap `config.ErrConfigInvalid`.

#### Scenario: Empty address is rejected at the handler

- GIVEN admin token and a body with `address=""`
- WHEN `POST /api/plcs` (or `PUT /api/plcs/{name}`) is called
- THEN the handler returns `400 Bad Request` with code `invalid_plc`
- AND the store is NOT written

#### Scenario: Invalid scanRate is rejected at the handler

- GIVEN admin token and a body with `scanRate="not-a-duration"`
- WHEN `POST /api/plcs` is called
- THEN the handler returns `400 Bad Request` with code `invalid_plc`

#### Scenario: Slot out of range 0–15 is rejected at the handler

- GIVEN admin token and a body with `slot=16`
- WHEN `POST /api/plcs` is called
- THEN the handler returns `400 Bad Request` with code `invalid_plc`

#### Scenario: Tag with empty name is rejected at the handler

- GIVEN admin token and a body with a tag whose `name=""`
- WHEN `POST /api/plcs` is called
- THEN the handler returns `400 Bad Request` with code `invalid_plc`

---

### [PCS-API-2.1] GET /api/plcs — list all PLCs (viewer+)

The system MUST expose `GET /api/plcs`, gated by `auth.RequireRole(RoleViewer, RoleOperator, RoleAdmin)` (RequireRole is an exact-match allowlist, NOT rank-based — every permitted role is enumerated). Response: `{"data":[<PLC>...]}`. An empty store MUST return `{"data":[]}` (not null). Each PLC object MUST include its `tags` array. No `id` field is present in any PLC object.

#### Scenario: Authenticated viewer lists PLCs

- GIVEN a viewer token and two PLCs in the store
- WHEN `GET /api/plcs` with `Authorization: Bearer <token>`
- THEN response is `200 OK`
- AND `data` has length 2 with tags populated

#### Scenario: Unauthenticated request returns 401

- GIVEN no Authorization header
- WHEN `GET /api/plcs`
- THEN response is `401 Unauthorized`

#### Scenario: Empty store returns empty array

- GIVEN the store has zero PLCs
- WHEN `GET /api/plcs`
- THEN `data` is `[]`

---

### [PCS-API-2.2] GET /api/plcs/{name} — get one PLC (viewer+)

The system MUST expose `GET /api/plcs/{name}`, gated viewer+. Non-existent name MUST return `404` with code `plc_not_found`. Response: `{"data":<PLC>}`. The PLC object is keyed by `name`; no `id` is returned.

#### Scenario: Viewer retrieves existing PLC

- GIVEN PLC `"Silo-1"` exists
- WHEN `GET /api/plcs/Silo-1` with a valid viewer token
- THEN response is `200 OK` and `data.name == "Silo-1"`

#### Scenario: Unknown name returns 404

- GIVEN no PLC named `"ghost"`
- WHEN `GET /api/plcs/ghost`
- THEN response is `404 Not Found` with code `plc_not_found`

---

### [PCS-API-2.3] POST /api/plcs — create PLC (admin only, audited)

The system MUST expose `POST /api/plcs`, gated by `auth.RequireRole(RoleAdmin)`. The handler MUST call `config.ValidatePLC` before the store write. On success: `201 Created` with `{"data":<PLC>}` (the handler re-fetches via `Get` to echo the canonical stored form). Validation failure: `400 Bad Request` code `invalid_plc`. Malformed body: `400` code `bad_request`. Duplicate name: `409 Conflict` code `duplicate_plc`. Each successful mutation MUST emit an audit event (nil-safe `if s.auditLog != nil`). After a successful create the system MUST call `reloadPLCsFromStore`, which reloads the always-constructed `plcMgr`.

#### Scenario: Admin creates a PLC

- GIVEN admin token, no PLC named `"Silo-1"` exists
- WHEN `POST /api/plcs` with a valid PLC body
- THEN response is `201 Created`
- AND `data.name` is `"Silo-1"`
- AND an audit event with action `plc.create` and detail `"Silo-1"` is written

#### Scenario: Non-admin is rejected with 403

- GIVEN a viewer token
- WHEN `POST /api/plcs` with a valid body
- THEN response is `403 Forbidden`

#### Scenario: Validation error returns 400

- GIVEN admin token and body with `address=""`
- WHEN `POST /api/plcs`
- THEN response is `400 Bad Request` with code `invalid_plc`

#### Scenario: Duplicate name returns 409

- GIVEN a PLC named `"Silo-1"` already exists
- WHEN `POST /api/plcs` with `name="Silo-1"`
- THEN response is `409 Conflict` with code `duplicate_plc`

---

### [PCS-API-2.4] PUT /api/plcs/{name} — update PLC (admin only, audited)

The system MUST expose `PUT /api/plcs/{name}`, admin-only. The handler MUST call `config.ValidatePLC` before the store write. On success: `200 OK` with `{"data":<PLC>}` (re-fetched canonical form). Unknown name: `404` code `plc_not_found`. Validation failure: `400` code `invalid_plc`. Rename collision: `409` code `duplicate_plc`. Each mutation MUST audit (action `plc.update`, detail = path `{name}`). After success the system MUST call `reloadPLCsFromStore`.

#### Scenario: Admin updates scanRate

- GIVEN PLC `"Silo-1"` with `scanRate="1s"` and admin token
- WHEN `PUT /api/plcs/Silo-1` with body containing `scanRate="500ms"`
- THEN response is `200 OK`
- AND `data.scanRate` is `"500ms"`
- AND an audit event with action `plc.update` is written

#### Scenario: Non-admin is rejected with 403

- GIVEN a viewer token
- WHEN `PUT /api/plcs/Silo-1`
- THEN response is `403 Forbidden`

#### Scenario: Unknown name returns 404

- GIVEN no PLC named `"ghost"`
- WHEN `PUT /api/plcs/ghost` with admin token
- THEN response is `404 Not Found` with code `plc_not_found`

---

### [PCS-API-2.5] DELETE /api/plcs/{name} — delete PLC (admin only, audited)

The system MUST expose `DELETE /api/plcs/{name}`, admin-only. On success: `204 No Content`. Unknown name: `404` code `plc_not_found`. Each mutation MUST audit (action `plc.delete`, detail = path `{name}`). After success the system MUST call `reloadPLCsFromStore`.

#### Scenario: Admin deletes a PLC

- GIVEN PLC `"Silo-1"` exists and admin token
- WHEN `DELETE /api/plcs/Silo-1`
- THEN response is `204 No Content`
- AND `GET /api/plcs/Silo-1` subsequently returns `404`
- AND an audit event with action `plc.delete` is written

#### Scenario: Non-admin is rejected with 403

- GIVEN a viewer token
- WHEN `DELETE /api/plcs/Silo-1`
- THEN response is `403 Forbidden`

---

### [PCS-API-2.6] Read path — GET /api/config/mappings reflects store state

`GET /api/config/mappings` (`handleConfigMappings`) MUST source PLC data from `*plcstore.Store` when present, falling back to `s.cfg` only when no store is wired (tests / no-store deployments). After any mutation the endpoint MUST return the updated PLC list without a process restart.

#### Scenario: Mapping reflects post-create state

- GIVEN an admin creates a new PLC via `POST /api/plcs`
- WHEN `GET /api/config/mappings` is called immediately after
- THEN the response includes the newly created PLC
- AND does NOT require a process restart

#### Scenario: Mapping reflects post-delete state

- GIVEN PLC `"Silo-1"` exists and is then deleted via `DELETE /api/plcs/{name}`
- WHEN `GET /api/config/mappings` is called
- THEN `"Silo-1"` is absent from the response

---

### [PCS-RELOAD-3.1] Always-constructed empty manager; mutations reload it

The gateway MUST ALWAYS construct a `plc.Manager` at startup, even when the store contains zero PLCs (the historical `len(cfg.PLCs) > 0` guard is removed). `plcMgr` is NEVER nil after startup. An empty manager runs no scan workers and leaks no goroutines. Every mutation handler calls `reloadPLCsFromStore`, which lists the store's PLCs, builds `&config.Config{PLCs: storePLCs}`, and calls `plcMgr.Reload(ctx, cfg)` directly (bypassing the file watcher — no self-trigger). Zero PLCs means a running Manager with zero workers, not a nil or stopped manager.

#### Scenario: Zero-PLC boot constructs an empty manager

- GIVEN the gateway boots with no PLCs in the store
- WHEN startup completes
- THEN `plcMgr` is constructed (non-nil) and holds zero workers
- AND no scan goroutine is started

#### Scenario: First PLC added to a zero-PLC gateway starts a worker

- GIVEN the gateway booted with an empty (zero-worker) manager
- WHEN `POST /api/plcs` creates the first PLC and `reloadPLCsFromStore` runs
- THEN `plcMgr.Reload` is invoked with the new PLC
- AND a scan worker begins for that PLC

#### Scenario: Deleting the last PLC leaves a running empty manager

- GIVEN exactly one PLC exists with a running worker
- WHEN `DELETE /api/plcs/{name}` removes it and `reloadPLCsFromStore` runs
- THEN the worker stops (no goroutine leak)
- AND `plcMgr` remains a running manager with zero workers (NOT nil, NOT stopped)

---

### [PCS-AUDIT-4.1] Mutation audit trail

Every successful `POST`, `PUT`, or `DELETE` on `/api/plcs` MUST write an audit event via `internal/auth` (nil-safe `if s.auditLog != nil`). The event MUST include at minimum: actor username (from auth claims, or `"unknown"`), action (`plc.create` | `plc.update` | `plc.delete`), and the PLC `name` as the `Detail` field. Audit fires only AFTER a successful store write; failed mutations (validation, duplicate, not-found) MUST NOT emit an audit event.

#### Scenario: Create audit event is written

- GIVEN admin user `alice` creates a PLC named `"Silo-1"`
- WHEN the creation succeeds
- THEN an audit event with `action="plc.create"`, actor `"alice"`, detail `"Silo-1"` is recorded

#### Scenario: Failed mutations do not emit audit events

- GIVEN admin user `alice` attempts to create a PLC with an invalid body
- WHEN the server returns `400 Bad Request`
- THEN no audit event is emitted for that attempt

---

## Acceptance Test Matrix

| Req ID | Scenario | Test Type |
|--------|----------|-----------|
| PCS-STORE-1.1 | Tables created on first open | Unit (tmp file) |
| PCS-STORE-1.1 | Open is idempotent | Unit (tmp file) |
| PCS-STORE-1.2 | Empty store seeds from YAML | Unit |
| PCS-STORE-1.2 | Non-empty store ignores seed | Unit |
| PCS-STORE-1.3 | Create valid PLC | Unit |
| PCS-STORE-1.3 | Duplicate name returns ErrPLCAlreadyExists | Unit |
| PCS-STORE-1.4 | List returns all with tags | Unit |
| PCS-STORE-1.4 | Get unknown name returns ErrPLCNotFound | Unit |
| PCS-STORE-1.5 | Update scanRate persists | Unit |
| PCS-STORE-1.5 | Update non-existent name returns ErrPLCNotFound | Unit |
| PCS-STORE-1.6 | Delete removes PLC and tags | Unit |
| PCS-STORE-1.6 | Delete unknown name returns ErrPLCNotFound | Unit |
| PCS-STORE-1.7 | Empty address rejected at handler | Unit (httptest) |
| PCS-STORE-1.7 | Invalid scanRate rejected at handler | Unit (httptest) |
| PCS-STORE-1.7 | Slot out of range rejected at handler | Unit (httptest) |
| PCS-STORE-1.7 | Empty tag name rejected at handler | Unit (httptest) |
| PCS-API-2.1 | Viewer lists PLCs | Unit (httptest) |
| PCS-API-2.1 | Unauthenticated returns 401 | Unit (httptest) |
| PCS-API-2.1 | Empty store returns `[]` | Unit (httptest) |
| PCS-API-2.2 | Viewer retrieves one PLC by name | Unit (httptest) |
| PCS-API-2.2 | Unknown name returns 404 | Unit (httptest) |
| PCS-API-2.3 | Admin creates PLC | Unit (httptest) |
| PCS-API-2.3 | Non-admin returns 403 | Unit (httptest) |
| PCS-API-2.3 | Validation error returns 400 | Unit (httptest) |
| PCS-API-2.3 | Duplicate name returns 409 | Unit (httptest) |
| PCS-API-2.4 | Admin updates scanRate | Unit (httptest) |
| PCS-API-2.4 | Non-admin returns 403 | Unit (httptest) |
| PCS-API-2.4 | Unknown name returns 404 | Unit (httptest) |
| PCS-API-2.5 | Admin deletes PLC | Unit (httptest) |
| PCS-API-2.5 | Non-admin returns 403 | Unit (httptest) |
| PCS-API-2.6 | Mapping reflects post-create state | Unit (httptest) |
| PCS-API-2.6 | Mapping reflects post-delete state | Unit (httptest) |
| PCS-RELOAD-3.1 | Zero-PLC boot constructs empty manager | Unit (cmd) |
| PCS-RELOAD-3.1 | Reload called after mutation | Unit (httptest) |
| PCS-RELOAD-3.1 | Empty manager Reload no-op/no-panic | Unit (httptest) |
| PCS-AUDIT-4.1 | Create audit event written | Unit |
| PCS-AUDIT-4.1 | Failed mutation no audit event | Unit |
</content>
</invoke>
