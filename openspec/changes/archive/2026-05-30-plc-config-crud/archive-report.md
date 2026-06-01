---
change: plc-config-crud
phase: archive-report
date: 2026-05-30
status: archived
---

# Archive Report: plc-config-crud

## Change Archived

**Change**: plc-config-crud
**Archived to**: `openspec/changes/archive/2026-05-30-plc-config-crud/`
**Verification verdict**: PASS WITH WARNINGS (PR1b verify-report)
**Mode**: Strict TDD

## Change Summary

UI-managed PLC configuration CRUD backed by a dedicated SQLite store
(`internal/plcstore`). The store becomes the runtime source of truth for PLC
definitions; YAML `plcs[]` is reclassified as a one-time bootstrap seed
(seed-on-empty, idempotent). Admin-gated REST CRUD under `/api/plcs` (reads
viewer+, mutations admin-only, audited) mutates the store and reloads the
always-constructed PLC `Manager` directly via a field-level drain-and-swap.
A new admin-only `/plcs` frontend page provides list + create/edit/delete.

## Implementation (3 PRs)

1. **#47** — `internal/plcstore` SQLite store + `config.TagDef.Writable` /
   `config.ValidatePLC` + `Manager.Reload` field-level drain-and-swap
   (store + config + Reload). Merged to main.
2. **#48** — `/api/plcs` CRUD handlers (`api_plcs.go`), `plc.*` audit events,
   `handleConfigMappings` store redirect, server/cmd wiring (always-construct
   manager, seed-on-first-run, watcher store-wins), OpenAPI documentation.
   Merged to main.
3. **#49** — Frontend `/plcs` admin page: `usePLCs`/`usePLC` + mutation hooks
   in `useApi.ts`, `PLCs.tsx` (RHF + Zod + TanStack Query), `/plcs` route with
   `requiredRole: "admin"`. Merged to main.

## Spec Reconciliations Applied (delta → as-built)

The delta specs were authored before implementation and diverged from what was
built. Each divergence was reconciled to the AS-BUILT reality (verified against
`internal/plcstore/store.go`, `internal/server/api_plcs.go`,
`internal/server/api.go`, `internal/config/{config.go,validate_plc.go}`,
`internal/plc/manager.go`, `cmd/lgb/cmd/server.go`):

| # | Stale delta | Reconciled to as-built |
|---|-------------|------------------------|
| 1 | Methods `CreatePLC`/`ListPLCs`/`GetPLC(id)`/`UpdatePLC(id)`/`DeletePLC(id)` | `Create(ctx,p)`, `List(ctx)`, `Get(ctx,name)`, `Update(ctx,name,p)`, `Delete(ctx,name)`, plus `IsEmpty(ctx)`, `Seed(ctx,plcs)` |
| 2 | `Create`/`Update` return `(PLC, error)` | They return only `error`; the API handler re-fetches via `Get` to echo the canonical stored form |
| 3 | Sentinels `ErrDuplicate`/`ErrNotFound`/`ErrValidation` | Only `ErrPLCNotFound` and `ErrPLCAlreadyExists` exist in `plcstore`; there is NO `ErrValidation` |
| 4 | Store validates input (PCS-STORE-1.7) | The store does NOT validate. Validation is `config.ValidatePLC` called by the API mutation handler before any store write; failure → HTTP 400 `invalid_plc`. Validation rules retained, requirement relocated to the handler |
| 5 | Routes `/api/plcs/{id}`, examples `id=3`, `data.id` | Routes are `/api/plcs/{name}`; `name` is the natural/public key. No `id` field appears in any API response; `id` is an internal autoincrement PK only |
| 6 | PCS-RELOAD-3.1 "plcMgr nil when zero PLCs; construct if nil; deleting last returns to nil/stopped" | LOCKED: gateway ALWAYS constructs an empty `plc.Manager`; `plcMgr` is NEVER nil. Mutations call `reloadPLCsFromStore` → `plcMgr.Reload(ctx, &config.Config{PLCs: storePLCs})`. Zero PLCs = running manager with zero workers. All nil-manager language dropped |
| 7 | Acceptance Test Matrix rows referencing `id` | Rewritten to reference `name` |
| 8 | PLC-CFG-1.1 / PLC-CFG-1.7 "YAML plcs[] authoritative" | Reclassified: YAML `plcs[]` is seed-only; `plcstore.Store` is the runtime source of truth |
| 9 | PLC-DRV-2.3 "compare by name; watcher-only" | Field-level diff via `reflect.DeepEqual` over the full `config.PLC` (incl. Tags); `Reload` callable directly from the mutation path |

Note: the schema (PCS-STORE-1.1, `PRAGMA foreign_keys = ON`, `SetMaxOpenConns(1)`,
`plcs` + `plc_tags` with `ON DELETE CASCADE`) was already accurate in the delta
and was kept. The PR1b verify-report flagged W1 (GET routes admin-gated instead
of viewer+); the as-built `internal/server/api.go` (lines 152–169) now gates GET
viewer+ via `RequireRole(RoleViewer, RoleOperator, RoleAdmin)` and mutations
admin-only — so W1 was resolved before final merge and the active spec reflects
viewer+ reads.

## Specs Synced

| Domain | Action | Details |
|--------|--------|---------|
| plc-config-store | Created | New capability spec at `openspec/specs/plc-config-store/spec.md` — PCS-STORE-1.1–1.7, PCS-API-2.1–2.6, PCS-RELOAD-3.1, PCS-AUDIT-4.1, reconciled to as-built (method names, `{name}` routes, real sentinels, validation-in-handler, always-constructed manager) |
| config | Updated | Added PCS-CFG-5.1 (`TagDef.Writable`, stored-not-enforced); modified PLC-CFG-1.1 and PLC-CFG-1.7 to reclassify YAML `plcs[]` as seed-only / store-is-source-of-truth |
| plc | Updated | Replaced PLC-DRV-2.3 with field-level drain-and-swap (`reflect.DeepEqual` over full `config.PLC` incl. Tags; `Reload` callable directly from the mutation path) |

## Source of Truth Updated

- `openspec/specs/plc-config-store/spec.md` — new capability (status: active)
- `openspec/specs/config/spec.md` — PCS-CFG-5.1 added; PLC-CFG-1.1 / PLC-CFG-1.7 modified
- `openspec/specs/plc/spec.md` — PLC-DRV-2.3 replaced

## Archive Contents

- explore.md
- proposal.md
- specs/ (3 delta specs: plc-config-store, config, plc)
- design.md
- tasks.md
- apply-progress.md
- verify-report.md (PASS WITH WARNINGS — PR1b)
- archive-report.md (this file)

## SDD Cycle Complete

The change has been fully planned, implemented (PRs #47, #48, #49, all merged to
main), verified, and archived. Ready for the next change.
</content>
