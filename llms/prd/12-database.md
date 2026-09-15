---
status: draft
updated: 2026-09-15
owner: tango-auth-porting
---

# PostgreSQL Database Requirements

## Database strategy

Tango supports PostgreSQL only. The database uses one `public` schema with logical ownership by
application module. Separate PostgreSQL schemas, RLS, partitioning, and database-per-module are
out of scope until a measured requirement appears.

The database is new product surface, not a compatibility target. The final schema must not preserve
obsolete names or shapes solely for older tango code.

## Module ownership

| Module | Tables and responsibilities |
| --- | --- |
| Identity | users, groups, credentials, sessions, auth/reset tokens, MFA, WebAuthn, device login |
| Federation | OIDC clients, redirect URIs, authorization codes, tokens, keys, SCIM providers |
| Admin | app settings, API keys, APIs, permissions, custom claims, audit logs |
| Webhook | endpoints, subscriptions, outbox events, deliveries, attempts |

Only the owning module may read or write its tables. Cross-module code uses UUID foreign keys and
consumer-side Go interfaces rather than importing another module's stores.

## Identifier and timestamp rules

- Use UUID/UUIDv7 for database identifiers.
- Use TypeID only at URL and cross-module API boundaries.
- Keep protocol identifiers such as OIDC `client_id`, OAuth codes, and external provider IDs in
  dedicated text columns when required by the protocol; do not use them as internal primary keys.
- Use `timestamptz` for timestamps and UTC in application code.
- Use explicit `created_at`, `updated_at`, `expires_at`, `revoked_at`, `used_at`, or `disabled_at`
  fields only where their lifecycle meaning applies.
- Do not use string IDs, delimited ID lists, or timestamps embedded in JSONB for core entities.

## Secret and credential storage

| Data | Storage |
| --- | --- |
| Password | Password hash |
| Reset/session/auth/recovery token | Purpose-specific hash |
| TOTP seed | `enc:<ciphertext>` |
| Webhook signing secret | `enc:<ciphertext>` |
| Provider token/private key | `enc:<ciphertext>` when recovery is required |
| Client secret | Hash when comparison is sufficient; encrypt only when recovery is required |
| Public key/identifier | Plain public representation |

Known encrypted columns must enforce the `enc:` marker with a database check where practical. The
application remains responsible for AES-GCM authentication and actual decryption.

There is no legacy format. Ciphertext without `enc:` is invalid. The system must not add a legacy
reader, fallback decoder, dual-write path, compatibility column, migration backfill, or compatibility
view.

## Domain-specific requirements

### Final schema cleanup

- Keep one final app configuration table, `app_config`; remove the duplicate `app_settings` shape.
- Use UUID session identifiers plus a separately hashed session token; do not store MFA-pending state
  as a flag on the full session row.
- Keep public key material separate from encrypted private key material.
- Replace combined webhook endpoint/log tables with explicit endpoint, delivery, and attempt tables.
- Remove LDAP and Application Images columns and tables from the final schema.

### Identity and MFA

- Keep credentials and sessions separate from `users`.
- Keep pending MFA authentication separate from full sessions.
- Store each recovery code as a hash row with a single-use timestamp.
- Add uniqueness and expiry constraints for user identifiers and authentication state.

### Federation

- Store OIDC redirect URIs as child rows.
- Separate public and private key material.
- Model authorization codes and refresh tokens as expiring/revocable rows.

### Webhooks

- Separate endpoint, event subscription, delivery, and attempt rows.
- Store immutable canonical delivery bytes so retries produce the same signature and body.
- Store only redacted response metadata in delivery logs.

### Audit and outbox

- Audit records are append-only and never contain secrets or ciphertext.
- Domain mutation and audit/outbox insert occur in one transaction.
- Queue notification happens after commit; delivery failure does not roll back the domain mutation.

## Constraints and migration rules

- Initial migrations include final `NOT NULL`, foreign key, unique, check, and index definitions.
- Migrations are additive or explicitly destructive only when the plan says the old feature is
  removed. Never edit an applied migration.
- Do not introduce nullable transitional columns, old/new duplicate fields, compatibility views, or
  adapters for deleted features.
- Cleanup jobs must use indexed expiry/status predicates and bounded batches.
- JSONB is limited to flexible metadata and event payloads; it must not replace relational security
  tables.

## Acceptance criteria

- A fresh Postgres database contains only the final in-scope schema.
- No LDAP, Application Images, or obsolete compatibility tables/columns remain.
- Every table has a documented module owner.
- Secret and token storage follows the classification table.
- Domain plus audit/outbox transaction tests pass.
- Exact webhook body retry tests pass.
- No legacy code or backward-compatibility behavior exists anywhere in the implementation.
