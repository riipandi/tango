---
status: planned
updated: 2026-09-15
---

# Arch Phase 4 — Module Layout & Registry Slimming

Module entry types are scattered inconsistently (`Feature` inside `schema.go` for customclaim and
scimsync; entries in `handler.go`, `service.go`, or `module.go` elsewhere), and
`internal/registry/features.go` (619 LOC) contains per-module settings-mapping logic
(`mapLDAPSettings` and peers) that belongs to the module owning the settings.

## Evidence

- `Feature`/`APIFeature` structs defined in `schema.go`: `identity/customclaim/schema.go:85`,
  `federation/scimsync/schema.go:68` — schema files should hold models/IDs/errors only.
- Entry types live in `handler.go` (most modules), `module.go` (appconfig), `service.go`
  (apiaccess, user, usergroup, customclaim, apikey) — no single rule for "where is the module
  entry point".
- `internal/registry/features.go:73` `mapLDAPSettings` (and the SMTP/appconfig peers) hard-code
  each module's settings field mapping inside the registry.

## Tasks

- [ ] Fix the mandatory module layout: `schema.go` (models, typed IDs, table consts, errors),
      `store.go`, `service.go` (business logic + options), `handler.go` (HTTP), `module.go`
      (Feature/Module entry + wiring). One-time move; `Feature`/`Module` types go to `module.go`.
- [ ] Every settings-consuming module exposes `FromMergedValues(values map[string]string) T`
      (or a constructor taking `appconfig.MergedValues`) in its own package; move
      `mapLDAPSettings` and peers out of the registry.
- [ ] `internal/registry/features.go`: reduce to pure assembly (constructors + wiring, no field
      mapping). Target: registry imports only module packages and config, no per-module logic.
- [ ] Confirm no admin-visible behavior change on the application-configuration endpoints
      (spot-check `GET /api/application-configuration` and `sync-ldap` with curl).

## Validation

`task test` (all suites), `task lint`, `task check`, curl spot-checks recorded in the progress log.

## Progress Log

- 2026-09-15 Phase planned.
