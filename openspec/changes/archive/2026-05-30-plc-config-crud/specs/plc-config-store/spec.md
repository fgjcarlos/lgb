---
change: plc-config-crud
phase: spec
domain: plc-config-store
date: 2026-05-29
status: draft
type: new
---

# PLC Config Store Specification

## Purpose

Defines the `internal/plcstore` package: SQLite-backed persistence for PLC configuration, seed-on-first-run from YAML, admin-gated audited REST CRUD endpoints under `/api/plcs`, and the store-as-source-of-truth read path. This package becomes the authoritative runtime source for all PLC definitions after first boot.

---

## Requirements

### [PCS-STORE-1.1] SQLite store — schema and open

The `internal/plcstore` package MUST expose a `Store` type backed by `modernc.org/sqlite` (pure-Go). On `Open(ctx, path)` the store MUST run migrate-on-open, creating two tables if they do not exist:

| Table | Key columns |
|-------|-------------|
| `plcs` | `id INTEGER PK AUTOINCREMENT`, `name TEXT UNIQUE NOT NULL`, `address TEXT NOT NULL`, `slot INTEGER`, `socket_timeout TEXT`, `scan_rate TEXT`, `keep_alive INTEGER`, `path TEXT` |
| `plc_tags` | `id INTEGER PK AUTOINCREMENT`, `plc_id INTEGER FK plcs(id) ON DELETE CASCADE`, `name TEXT NOT NULL`, `type TEXT NOT NULL`, `writable INTEGER DEFAULT 0` |

`SetMaxOpenConns(1)` MUST be called after open to serialise writes. `CGO_ENABLED=0` MUST remain valid.

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

The system MUST seed the store exactly once from `cfg.PLCs` when the store is empty at startup. After seeding, subsequent boots MUST NOT overwrite or re-import from YAML even if `plcs.db` is present.

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

The store MUST expose `CreatePLC(ctx, plc PLC) (PLC, error)`. On success it MUST insert both the PLC row and its associated `plc_tags` rows in a single transaction. Duplicate `name` MUST return a sentinel `ErrDuplicate`.

#### Scenario: Create a valid PLC

- GIVEN the store is empty
- WHEN `CreatePLC` is called with a valid PLC (address non-empty, scanRate valid, slot 0–15, tags non-empty names/types)
- THEN it returns the PLC with a non-zero `id`
- AND `ListPLCs` includes the new PLC

#### Scenario: Duplicate name returns ErrDuplicate

- GIVEN a PLC named `"PLC-A"` exists
- WHEN `CreatePLC` is called with `name="PLC-A"`
- THEN it returns an error satisfying `errors.Is(err, ErrDuplicate)`

---

### [PCS-STORE-1.4] CRUD — Read PLCs

The store MUST expose:
- `ListPLCs(ctx) ([]PLC, error)` — returns all PLCs with their tags populated
- `GetPLC(ctx, id int64) (PLC, error)` — returns one PLC with tags; missing id returns `ErrNotFound`

#### Scenario: ListPLCs returns all rows with tags

- GIVEN two PLCs each with two tags in the store
- WHEN `ListPLCs(ctx)` is called
- THEN the returned slice has length 2
- AND each PLC's `Tags` slice has length 2

#### Scenario: GetPLC unknown id returns ErrNotFound

- GIVEN no PLC with `id=999` exists
- WHEN `GetPLC(ctx, 999)` is called
- THEN it returns `ErrNotFound`

---

### [PCS-STORE-1.5] CRUD — Update PLC

The store MUST expose `UpdatePLC(ctx, id int64, plc PLC) (PLC, error)`. The update MUST replace all scalar fields AND replace the PLC's tags atomically (delete old tags, insert new tags) in one transaction. Missing id MUST return `ErrNotFound`.

#### Scenario: Update scanRate persists correctly

- GIVEN a PLC with `scanRate="1s"`
- WHEN `UpdatePLC` is called with `scanRate="500ms"`
- THEN `GetPLC` returns the same PLC with `scanRate="500ms"`
- AND the tag set reflects any tag changes passed in the update

#### Scenario: Update non-existent PLC returns ErrNotFound

- GIVEN no PLC with `id=999`
- WHEN `UpdatePLC(ctx, 999, ...)` is called
- THEN it returns `ErrNotFound`

---

### [PCS-STORE-1.6] CRUD — Delete PLC

The store MUST expose `DeletePLC(ctx, id int64) error`. Deletion MUST cascade to `plc_tags` (via `ON DELETE CASCADE` or explicit delete). Missing id MUST return `ErrNotFound`.

#### Scenario: Delete removes PLC and its tags

- GIVEN PLC `id=1` with three tags
- WHEN `DeletePLC(ctx, 1)` is called
- THEN `GetPLC(ctx, 1)` returns `ErrNotFound`
- AND no rows in `plc_tags` have `plc_id=1`

#### Scenario: Delete unknown id returns ErrNotFound

- GIVEN no PLC with `id=999`
- WHEN `DeletePLC(ctx, 999)` is called
- THEN it returns `ErrNotFound`

---

### [PCS-STORE-1.7] Input validation mirrors config validation

The store MUST apply the same per-PLC validation rules as `internal/config` before any write (Create or Update):

| Rule | Condition |
|------|-----------|
| `address` non-empty | Always required |
| `scanRate` valid duration > 0 | If non-empty |
| `socketTimeout` valid duration > 0 | If non-empty |
| `slot` in range 0–15 | Always |
| Each tag `name` non-empty | Per-tag |
| Each tag `type` non-empty | Per-tag |

Violations MUST return an error wrapping `ErrValidation`. All violations for a single call MUST be aggregated via `errors.Join`.

#### Scenario: Empty address is rejected

- GIVEN a PLC with `address=""`
- WHEN `CreatePLC` or `UpdatePLC` is called
- THEN it returns an error satisfying `errors.Is(err, ErrValidation)`
- AND the error message references `address`

#### Scenario: Invalid scanRate is rejected

- GIVEN a PLC with `scanRate="not-a-duration"`
- WHEN `CreatePLC` or `UpdatePLC` is called
- THEN it returns an error satisfying `errors.Is(err, ErrValidation)`

#### Scenario: Slot out of range 0–15 is rejected

- GIVEN a PLC with `slot=16`
- WHEN `CreatePLC` or `UpdatePLC` is called
- THEN it returns an error satisfying `errors.Is(err, ErrValidation)`

#### Scenario: Tag with empty name is rejected

- GIVEN a PLC tag with `name=""`
- WHEN `CreatePLC` or `UpdatePLC` is called
- THEN it returns an error satisfying `errors.Is(err, ErrValidation)`

#### Scenario: Multiple violations are reported together

- GIVEN a PLC with `address=""` and `slot=99`
- WHEN `CreatePLC` is called
- THEN the error contains messages for BOTH violations

---

### [PCS-API-2.1] GET /api/plcs — list all PLCs (viewer+)

The system MUST expose `GET /api/plcs`, gated by `auth.Middleware` (viewer or higher). Response: `{"data":[<PLC>...]}`. An empty store MUST return `{"data":[]}` (not null). Each PLC object MUST include its `tags` array.

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

### [PCS-API-2.2] GET /api/plcs/{id} — get one PLC (viewer+)

The system MUST expose `GET /api/plcs/{id}`, gated by `auth.Middleware` (viewer+). Non-existent id MUST return `404`. Response: `{"data":<PLC>}`.

#### Scenario: Viewer retrieves existing PLC

- GIVEN PLC `id=3` exists
- WHEN `GET /api/plcs/3` with a valid viewer token
- THEN response is `200 OK` and `data.id == 3`

#### Scenario: Unknown id returns 404

- GIVEN no PLC with `id=999`
- WHEN `GET /api/plcs/999`
- THEN response is `404 Not Found`

---

### [PCS-API-2.3] POST /api/plcs — create PLC (admin only, audited)

The system MUST expose `POST /api/plcs`, gated by `auth.RequireRole(RoleAdmin)`. On success: `201 Created` with `{"data":<PLC>}`. Validation failure: `400 Bad Request`. Duplicate name: `409 Conflict`. Each successful mutation MUST emit an audit event via `internal/auth/audit.go`.

After a successful create the system MUST call `plcMgr.Reload` (or initialize `plcMgr` if nil) to start the new worker.

#### Scenario: Admin creates a PLC

- GIVEN admin token, no PLC named `"Silo-1"` exists
- WHEN `POST /api/plcs` with a valid PLC body
- THEN response is `201 Created`
- AND `data.name` is `"Silo-1"`
- AND an audit event is written to `events.jsonl`

#### Scenario: Non-admin is rejected with 403

- GIVEN a viewer token
- WHEN `POST /api/plcs` with a valid body
- THEN response is `403 Forbidden`

#### Scenario: Validation error returns 400

- GIVEN admin token and body with `address=""`
- WHEN `POST /api/plcs`
- THEN response is `400 Bad Request`
- AND body contains `{"error":{"code":"bad_request","message":"..."}}`

#### Scenario: Duplicate name returns 409

- GIVEN a PLC named `"Silo-1"` already exists
- WHEN `POST /api/plcs` with `name="Silo-1"`
- THEN response is `409 Conflict`

---

### [PCS-API-2.4] PUT /api/plcs/{id} — update PLC (admin only, audited)

The system MUST expose `PUT /api/plcs/{id}`, admin-only. On success: `200 OK` with `{"data":<PLC>}`. Unknown id: `404`. Validation failure: `400`. Each mutation MUST audit. After success the system MUST call `plcMgr.Reload`.

#### Scenario: Admin updates scanRate

- GIVEN PLC `id=1` with `scanRate="1s"` and admin token
- WHEN `PUT /api/plcs/1` with body containing `scanRate="500ms"`
- THEN response is `200 OK`
- AND `data.scanRate` is `"500ms"`
- AND an audit event is written

#### Scenario: Non-admin is rejected with 403

- GIVEN a viewer token
- WHEN `PUT /api/plcs/1`
- THEN response is `403 Forbidden`

#### Scenario: Unknown id returns 404

- GIVEN no PLC with `id=999`
- WHEN `PUT /api/plcs/999` with admin token
- THEN response is `404 Not Found`

---

### [PCS-API-2.5] DELETE /api/plcs/{id} — delete PLC (admin only, audited)

The system MUST expose `DELETE /api/plcs/{id}`, admin-only. On success: `204 No Content`. Unknown id: `404`. Each mutation MUST audit. After success the system MUST call `plcMgr.Reload`.

#### Scenario: Admin deletes a PLC

- GIVEN PLC `id=2` exists and admin token
- WHEN `DELETE /api/plcs/2`
- THEN response is `204 No Content`
- AND `GET /api/plcs/2` subsequently returns `404`
- AND an audit event is written

#### Scenario: Non-admin is rejected with 403

- GIVEN a viewer token
- WHEN `DELETE /api/plcs/1`
- THEN response is `403 Forbidden`

---

### [PCS-API-2.6] Read path — GET /api/config/mappings reflects store state

`GET /api/config/mappings` (`handleConfigMappings`) MUST source PLC data from `*plcstore.Store`, not from `s.cfg` (the frozen startup config). After any mutation the endpoint MUST return the updated PLC list without a process restart.

#### Scenario: Mapping reflects post-create state

- GIVEN an admin creates a new PLC via `POST /api/plcs`
- WHEN `GET /api/config/mappings` is called immediately after
- THEN the response includes the newly created PLC
- AND does NOT require a process restart

#### Scenario: Mapping reflects post-delete state

- GIVEN PLC `"Silo-1"` exists and is then deleted via `DELETE /api/plcs/{id}`
- WHEN `GET /api/config/mappings` is called
- THEN `"Silo-1"` is absent from the response

---

### [PCS-RELOAD-3.1] Zero-PLC boot — lazy manager initialisation

When the gateway starts with zero PLCs in the store, `plcMgr` MUST be `nil`. When a PLC is added via `POST /api/plcs`, the mutation path MUST construct and start a new `Manager` if `plcMgr` is currently `nil`, then call `Reload` on it.

#### Scenario: First PLC added to zero-PLC gateway starts a worker

- GIVEN the gateway booted with no PLCs (`plcMgr == nil`)
- WHEN `POST /api/plcs` creates the first PLC
- THEN a Manager is started with that PLC
- AND the scan worker begins running within one `ScanRate` interval

#### Scenario: Deleting the last PLC stops the manager

- GIVEN exactly one PLC exists and its worker is running
- WHEN `DELETE /api/plcs/{id}` removes it
- THEN the Manager's worker stops (no goroutine leak)
- AND `plcMgr` returns to a nil or stopped state

---

### [PCS-AUDIT-4.1] Mutation audit trail

Every successful `POST`, `PUT`, or `DELETE` on `/api/plcs` MUST write an audit event to `{dataDir}/events.jsonl` via `internal/auth/audit.go`. The event MUST include at minimum: timestamp, actor username, action (`plc.create` | `plc.update` | `plc.delete`), and the PLC `id` or `name`.

#### Scenario: Create audit event is written

- GIVEN admin user `alice` creates a PLC
- WHEN the creation succeeds
- THEN `events.jsonl` contains an entry with `action="plc.create"` and actor `"alice"`

#### Scenario: Failed mutations do not emit audit events

- GIVEN admin user `alice` attempts to create a PLC with an invalid body
- WHEN the server returns `400 Bad Request`
- THEN no audit event is appended to `events.jsonl` for that attempt

---

## Acceptance Test Matrix

| Req ID | Scenario | Test Type |
|--------|----------|-----------|
| PCS-STORE-1.1 | Tables created on first open | Unit (tmp file) |
| PCS-STORE-1.1 | Open is idempotent | Unit (tmp file) |
| PCS-STORE-1.2 | Empty store seeds from YAML | Unit |
| PCS-STORE-1.2 | Non-empty store ignores seed | Unit |
| PCS-STORE-1.3 | Create valid PLC | Unit |
| PCS-STORE-1.3 | Duplicate name returns ErrDuplicate | Unit |
| PCS-STORE-1.4 | ListPLCs returns all with tags | Unit |
| PCS-STORE-1.4 | GetPLC unknown id returns ErrNotFound | Unit |
| PCS-STORE-1.5 | Update scanRate persists | Unit |
| PCS-STORE-1.5 | Update non-existent PLC | Unit |
| PCS-STORE-1.6 | Delete removes PLC and tags | Unit |
| PCS-STORE-1.6 | Delete unknown id | Unit |
| PCS-STORE-1.7 | Empty address rejected | Unit |
| PCS-STORE-1.7 | Invalid scanRate rejected | Unit |
| PCS-STORE-1.7 | Slot out of range rejected | Unit |
| PCS-STORE-1.7 | Empty tag name rejected | Unit |
| PCS-STORE-1.7 | Multiple violations aggregated | Unit |
| PCS-API-2.1 | Viewer lists PLCs | Unit (httptest) |
| PCS-API-2.1 | Unauthenticated returns 401 | Unit (httptest) |
| PCS-API-2.1 | Empty store returns `[]` | Unit (httptest) |
| PCS-API-2.2 | Viewer retrieves one PLC | Unit (httptest) |
| PCS-API-2.2 | Unknown id returns 404 | Unit (httptest) |
| PCS-API-2.3 | Admin creates PLC | Unit (httptest) |
| PCS-API-2.3 | Non-admin returns 403 | Unit (httptest) |
| PCS-API-2.3 | Validation error returns 400 | Unit (httptest) |
| PCS-API-2.3 | Duplicate name returns 409 | Unit (httptest) |
| PCS-API-2.4 | Admin updates scanRate | Unit (httptest) |
| PCS-API-2.4 | Non-admin returns 403 | Unit (httptest) |
| PCS-API-2.4 | Unknown id returns 404 | Unit (httptest) |
| PCS-API-2.5 | Admin deletes PLC | Unit (httptest) |
| PCS-API-2.5 | Non-admin returns 403 | Unit (httptest) |
| PCS-API-2.6 | Mapping reflects post-create state | Unit (httptest) |
| PCS-API-2.6 | Mapping reflects post-delete state | Unit (httptest) |
| PCS-RELOAD-3.1 | First PLC on zero-PLC gateway | Unit |
| PCS-RELOAD-3.1 | Last PLC deleted stops manager | Unit |
| PCS-AUDIT-4.1 | Create audit event written | Unit |
| PCS-AUDIT-4.1 | Failed mutation no audit event | Unit |
