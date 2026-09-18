---
status: done
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

- [x] Fix the mandatory module layout: `schema.go` (models, typed IDs, table consts, errors),
      `store.go`, `service.go` (business logic + options), `handler.go` (HTTP), `module.go`
      (Feature/Module entry + wiring). One-time move; `Feature`/`Module` types go to `module.go`.
- [x] Every settings-consuming module exposes `FromMergedValues(values map[string]string) T`
      (or a constructor taking `appconfig.MergedValues`) in its own package; move
      `mapLDAPSettings` and peers out of the registry.
- [x] `internal/registry/features.go`: reduce to pure assembly (constructors + wiring, no field
      mapping). Target: registry imports only module packages and config, no per-module logic.
- [x] Confirm no admin-visible behavior change on the application-configuration endpoints
      (spot-check `GET /api/application-configuration` and `sync-ldap` with curl).

## What moved where

- `customclaim.Feature` + `scimsync.Feature`: `schema.go` → new `module.go` (layout rule).
- `mapLDAPSettings` → `ldapsync.FromMergedValues`; `envLDAPSettings` → `ldapsync.SettingsFromEnv`
  (new `modules/identity/ldapsync/settings.go` + tests moved there from registry_test.go).
- `MailerSettingsFromValues` → `mailer.SettingsFromValues` (new `internal/mailer/settings.go`,
  test moved alongside; `cmd/launcher/serve.go` calls it directly).
- `appConfigEnvDefaults` → `appconfig.EnvDefaults` (new `modules/appconfig/env.go`, which also
  owns the CIMD allowlist parsing the registry getter used to inline).
- SCIM snapshot SQL (~120 LOC: `scimSnapshotSource`, `scimUserRows`, `toAny`) →
  `scimsync.IdentitySnapshotSource` (new `modules/federation/scimsync/snapshot.go`) — the
  registry no longer builds identity-table SQL.
- AGENTS.md "Add a config key" convention updated: env layer lives in `modules/appconfig/env.go`,
  not `internal/registry`.

`internal/registry/features.go`: 624 → 393 lines; what remains is constructors, adapters
(federation audit, email verification, default picture), and the appconfig late-binding ref —
pure assembly.

## Validation

- `task test:go`: 533 pass, 1 skipped, 0 fail. `task test:go:debug`: 35 pass.
  `go test -tags release ./...`: all ok. golangci-lint: 0 issues. vet + gofmt + `task typecheck`
  clean.
- Live curl (scratch DB + debug binary): `GET /api/application-configuration` public view ok;
  `/all` shows env-folded values (smtp_host=localhost, smtp_port=1025 via `appconfig.EnvDefaults`);
  `sync-ldap` 401 anonymous / 409 admin with LDAP disabled; `test-email` 202 queued.

## Progress Log

- 2026-09-15 Phase planned.
- 2026-09-15 Implemented: settings mapping + SCIM snapshot SQL moved into owning packages;
  registry reduced to assembly; layout rule applied to the two schema.go Feature strays.
