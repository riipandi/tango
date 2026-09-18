---
status: done
updated: 2026-09-17
owner: tango-remediation
---

# Phase 1 — Final Schema and Compatibility Removal

Prerequisite: Phase 0 done and the inventory approved.

## Task 1.1 — Choose the final client secret shape

Settle on one final shape for client secrets. The final shape must support secret metadata,
hash-only comparison, expiry, active state, create-once response, rotation, and deletion without
requiring a legacy column.

Update the schema types, store queries, create/verify/rotate/delete flows, and tests so only the
final shape is used. Remove `LegacySecretID`, the synthetic legacy entry, the fallback
comparison, and any comments claiming legacy compatibility.

Add tests for create, multi-secret, expiry, inactive secret, rotation, deletion, and client
authentication after the legacy column is no longer read.

Commit: `refactor: finalize oidc client secret storage`

## Task 1.2 — Drop the client-secret and image legacy columns

Create a new migration; never edit an applied migration. Remove the columns that are not part of
the final schema, at minimum:

- `oidc_clients.secret` once Task 1.1 has moved every usage;
- `oidc_clients.image_type`;
- `oidc_clients.dark_image_type`.

Adjust `SELECT`, `INSERT`, `UPDATE`, scanners, the metadata view, the API access view, and tests.
Client logos stay supported through `logo_path` if that is an in-scope feature.

The migration must be safe on a fresh database and on a database that already holds the previous
shape. Add a schema contract test asserting the obsolete columns are absent.

Commit: `db: remove obsolete client columns`

## Task 1.3 — Drop the unused refresh-token table

Make sure every runtime OIDC flow uses the chosen final storage. If `oidc_refresh_tokens` has no
active caller, drop the table and its indexes through a new migration, then update the migrator
count/assertions, schema contract, backup tests, and the database reference if needed.

If an active caller turns up, document the caller and prove why the table is part of the final
schema before changing it.

Commit: `db: remove unused oidc refresh token table`

## Task 1.4 — Enforce the final encrypted-value storage

Audit every recoverable secret: TOTP, webhook, SCIM, JWKS/private material, and sensitive
settings. Make sure every write goes through `pkg/crypto.Cipher.Encrypt`, every read through
`Decrypt`, and every known encrypted column has the matching marker check.

Add real-Postgres tests for malformed prefix, missing prefix, wrong key, tampering, and
redaction. Hash-only values stay hashes and must never move to encryption.

Commit: `test: enforce final encrypted value storage`

## Phase acceptance criteria

- No `legacy`, fallback reader, dual write, or compatibility adapter remains for client secrets.
- Fresh schema and migration upgrade produce the same final schema.
- No obsolete column or table without a final caller remains.
- Every recoverable secret uses the `enc:` format.
- Migration version/count assertions are updated.

## Evidence

All four tasks committed and validated (2026-09-17):

- Task 1.1 — commit `cd085ef`. Final shape: credentials JSONB only; `CreateClient` seeds one
  active entry, `UpdateClient` with `SecretHash` replaces the whole list (rotation), `AddClientSecret`
  appends, `DeleteClientSecret` removes. `secretMatches` reads the credentials list only;
  `has_secret` reports an active, unexpired entry. Tests: `modules/federation/oidc/client_secret_test.go`
  (lifecycle + no-legacy-fallback over real Postgres).
- Task 1.2 — commit `b52886c`. Migration `00009_drop_obsolete_oidc_client_columns.sql` drops
  `secret`, `image_type`, `dark_image_type`. `has_logo` derives from `logo_path`; `has_dark_logo`
  removed from the meta view, the apiaccess `ClientRef`, and the SDK zod schemas/fixtures.
  Schema contract test asserts the columns are absent (`database/schema_fresh_test.go`).
- Task 1.3 — commit `e17a7e0`. Migration `00010_drop_unused_oidc_refresh_tokens.sql` drops the
  table and `idx_oidc_refresh_tokens_expires_at`; no runtime caller existed (runtime refresh
  tokens are `oauth2_sessions` rows). Migrator count/assertions updated.
- Task 1.4 — commit `e7fbfd7`. Sensitive settings (`smtp_password`) are sealed with `enc:` on
  write and decrypted on read via the module cipher (wired in `internal/registry/registry.go`);
  an undecryptable value fails closed (no plaintext fallback). The SCIM provider token decrypts
  strictly (the plaintext fallback reader is gone). Marker checks now cover `user_mfa_totp.secret_enc`
  and `jwks.private_key` in addition to webhook/SCIM (`database/schema_contract_test.go`).
  Migration `00011_enforce_app_config_sensitive_enc.sql` adds the conditional CHECK and purges
  un-encryptable plaintext overrides. Tests: `appconfig/store_test.go` (enc: at rest, fail-closed
  on tampering), `scimsync/store_test.go` (enc: at rest, strict decrypt), real-Postgres wrong
  key/tampering covered in both.

Validation run at close: `go vet ./...` pass, `gofmt -l` clean, full `go test -tags release ./...`
pass (exit 0), debug-tag `./cmd/... ./database/...` pass, vitest 76/76 pass, `tsc -b --noEmit`
clean. Migrations count is 11; assertions live in `database/migrator_test.go` and
`cmd/launcher/db_migrate*_test.go`.
