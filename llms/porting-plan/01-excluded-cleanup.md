---
status: planned
updated: 2026-09-15
---

# Excluded-Feature Cleanup

Goal: remove the existing LDAP and Application Images implementation from the runtime and keep
only shared code that is still required by in-scope features.

Prerequisites: read `00-scope.md`; inspect `internal/registry`, `modules/appconfig`, migrations,
compose files, and active `rg` references. Do not delete shared storage, mailer, profile-picture,
or configuration code until its callers are checked.

## Tasks

1. **Inventory dependencies and freeze removal scope**

   Trace LDAP and Application Images imports, route mounts, config keys, environment variables,
   migrations, compose services, Yaak requests, tests, and documentation. Identify shared storage,
   mailer, profile-picture, and appconfig code that must remain. Commit:
   `docs: define excluded-feature removal scope`.

2. **Remove LDAP runtime wiring**

   Unmount `sync-ldap`, remove the `ldapsync` feature from registry construction, remove LDAP
   settings providers, and make application configuration unaware of LDAP-only consumers. Add route
   tests proving the endpoint is not mounted. Commit: `refactor: remove LDAP runtime wiring`.

3. **Remove the LDAP module and dependency**

   Delete `modules/identity/ldapsync` and its tests, remove `github.com/go-ldap/ldap/v3`, remove
   LDAP-only config types/defaults/validation/env mappings, and remove LDAP entries from
   `.env.example`. Commit: `chore: remove LDAP integration`.

4. **Remove LDAP development infrastructure**

   Remove the glauth service, seed data, LDAP-only compose volumes/configuration, and LDAP-specific
   test setup. Keep Postgres, Mailpit, and services needed by in-scope tests. Commit:
   `chore: remove LDAP development services`.

5. **Remove LDAP schema remnants safely**

   Add a new Postgres migration to drop LDAP-only columns and indexes from identity tables; do not
   edit an already-applied migration. Verify no user, group, or in-scope query still references
   those columns. Commit: `db: remove LDAP schema remnants`.

6. **Remove Application Images runtime paths**

   Unmount Application Images routes, remove `modules/appimage` only when no shared caller remains,
   and preserve storage code required by other in-scope features. Delete its tests, fixtures, and
   Yaak requests. Commit: `chore: remove application images integration`.

7. **Clean references and verify the removal**

   Mark both feature families `excluded` in the endpoint reference and deviations docs. Run a
   repository search to confirm no active code, config, compose file, or test refers to LDAP or
   Application Images. Run focused tests, `task test`, `task lint`, and `task check`. Commit:
   `docs: finalize excluded-feature cleanup`.

## Removal acceptance criteria

- No active route mounts LDAP or Application Images.
- No active Go package imports the LDAP client or `modules/identity/ldapsync`.
- LDAP-only environment keys, config fields, compose services, fixtures, and tests are gone.
- LDAP-only columns and indexes are removed through a new Postgres migration, not by rewriting an
  applied migration.
- Application Images code is removed only when no in-scope caller needs it.
- `go list -deps ./...`, focused tests, `task test`, `task lint`, and `task check` pass.
