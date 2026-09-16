---
status: done
updated: 2026-09-16
---

# PostgreSQL Database Design

Goal: define a small, enforceable PostgreSQL schema for the modular monolith without introducing
legacy tables, compatibility columns, dual writes, or transitional adapters.

Prerequisites: read `llms/database-reference.sql`, all active migrations, the owning module, and
`llms/prd/12-database.md`. This plan describes the target schema, not a compatibility layer for an
older implementation.

## Target decisions

- PostgreSQL is the only supported database.
- Keep one `public` schema. Module ownership is enforced in code, migrations, foreign keys, and
  review; do not create one PostgreSQL schema per module yet.
- Database entity identifiers use UUID/UUIDv7. TypeID is used only at URL and cross-module API
  boundaries and is converted at store edges. Protocol identifiers such as OIDC `client_id`, OAuth
  codes, and external provider IDs remain dedicated text columns when required by the protocol;
  they must not be overloaded as internal primary keys.
- Every table has an explicit owner: identity, federation, admin, or webhook.
- Core data uses typed columns and relational tables. JSONB is limited to flexible metadata and
  event payloads; it is not a replacement for credentials, permissions, subscriptions, or MFA rows.
- New migrations create the final shape. Do not add `legacy_*` columns, compatibility views,
  dual-write paths, fallback readers, or migration-only adapters.
- Resolve known schema drift before feature work: use one final app-config table, separate webhook
  endpoints from delivery/attempt records, keep session identifiers as UUIDs with token hashes,
  remove `totp_pending` from sessions, remove LDAP/image columns, and keep public key material
  separate from encrypted private material.

## Ownership model

```text
identity    users, groups, credentials, sessions, auth/reset tokens, MFA, WebAuthn, device login
federation  OIDC clients, redirect URIs, authorization codes, tokens, keys, SCIM providers
admin       app settings, API keys, APIs, permissions, claims, audit logs
webhook     endpoints, subscriptions, outbox events, deliveries, delivery attempts
```

### Current table inventory

Live tables map to one owner each; obsolete tables are removal targets for the cleanup migration.

| Owner | Live tables |
| --- | --- |
| identity | users, user_passwords, user_groups, user_groups_users, sessions, auth_tokens, signup_tokens, signup_tokens_user_groups, refresh_tokens, device_login_requests, webauthn_credentials, webauthn_sessions, deleted_records (trigger archive) |
| federation | oidc_clients, oidc_clients_allowed_user_groups, oidc_client_api_grants, oidc_client_api_grant_permissions, user_authorized_oidc_clients, oidc_authorization_codes, oidc_refresh_tokens, oauth2_sessions, oauth2_jtis, interaction_sessions, jwks, scim_service_providers |
| admin | app_config, audit_logs, api_keys, apis, api_permissions, custom_claims |
| webhook | webhook_endpoints, webhook_deliveries, webhook_delivery_attempts |
| infrastructure | queue_tasks, queue_tasks_completed, rate_limits (via fn_check_rate_limit) |

Obsolete (zero live references): app_settings (duplicate legacy shape), user_phones,
invitations, mfa_keys, oauth_connections, oidc_device_codes, file_stores, refresh_tokens
(sessions use sliding expiry via `refreshed_at`, not separate refresh-token rows). Session
drift: `totp_pending`, `oauth_groups`, `oauth_name`, and `oauth_sub` columns on sessions have
no live readers. The `refresh_token` TypeID prefix is also dead.

## Final schema contracts

These contracts drive the cleanup and feature migrations; each section lists the deltas from
the current shape, not a full re-declaration of unchanged columns.

### Identity (delta)

- `users`: nothing to drop beyond the removed `ldap_id` (00032). Keep CITEXT username,
  normalized unique username/email, and metadata JSONB for flexible profile data only.
- `sessions`: convert `id` from TEXT to UUID with `uuidv7()` (TypeID is a store-edge
  conversion, not a storage format); drop `totp_pending`, `oauth_groups`, `oauth_name`,
  `oauth_sub`; keep `token_hash` UNIQUE and `provider`. Pending MFA state lives in the MFA
  tables, never as a session flag.
- `auth_tokens`: keep the purpose CHECK (`email_verification`, `one_time_access`,
  `reauthentication`), SHA-256 token hashes, expiry CHECK, and `last_sent_at`.
- `signup_tokens` + `signup_tokens_user_groups`: unchanged shape; keep the group FKs and
  usage/limit columns.
- `webauthn_credentials` / `webauthn_sessions`: unchanged shape; ceremony sessions expire and
  are cleaned up by the token cleanup job.
- `device_login_requests`: unchanged shape; expiry + status columns drive cleanup.

### MFA (new tables, task 5)

- `user_mfa_totp` (identity-owned): `id UUID PK uuidv7()`, `user_id UUID NOT NULL UNIQUE
  REFERENCES users(id) ON DELETE CASCADE`, `secret_enc TEXT NOT NULL CHECK (secret_enc LIKE
  'enc:%')`, `digits SMALLINT NOT NULL CHECK (digits IN (6, 8))`, `period SMALLINT NOT NULL
  CHECK (period BETWEEN 15 AND 120)`, `algorithm TEXT NOT NULL DEFAULT 'SHA1' CHECK (algorithm
  IN ('SHA1', 'SHA256', 'SHA512'))`, `confirmed_at TIMESTAMPTZ`, `last_used_step BIGINT` —
  one row per user (UNIQUE user_id); unconfirmed rows are replaced on re-enroll.
- `user_mfa_recovery_codes` (identity-owned): `id UUID PK uuidv7()`, `user_id UUID NOT NULL
  REFERENCES users(id) ON DELETE CASCADE`, `code_hash TEXT NOT NULL UNIQUE`, `used_at
  TIMESTAMPTZ` — one row per code, hashed, single-use; indexed on `(user_id)` and
  `(user_id, used_at)` for the remaining-code check.
- `user_mfa_pending` (identity-owned): `id UUID PK uuidv7()`, `user_id UUID NOT NULL UNIQUE
  REFERENCES users(id) ON DELETE CASCADE`, `token_hash TEXT NOT NULL UNIQUE`, `expires_at
  TIMESTAMPTZ NOT NULL CHECK (expires_at > CURRENT_TIMESTAMP)`, `created_at TIMESTAMPTZ NOT
  NULL DEFAULT CURRENT_TIMESTAMP` — the short-lived bridge between a successful password
  sign-in and full session issuance; one row per user, replaced on every password sign-in.

### Federation (delta)

- `oidc_clients`: move redirect URIs to a child table `oidc_client_redirect_uris` (`id UUID
  PK`, `client_id UUID NOT NULL REFERENCES oidc_clients(id) ON DELETE CASCADE`, `uri TEXT NOT
  NULL UNIQUE`, `created_at`); drop the delimited URI column.
- `jwks`: keep public key material in plain columns and the private PEM as `enc:<ciphertext>`;
  never encrypt public data.
- `oidc_authorization_codes`, `oidc_refresh_tokens`, and `oauth2_sessions`/`oauth2_jtis`: keep
  expiry/revocation semantics; `oauth2_sessions.kind` CHECK covers `authorize_code`,
  `access_token`, `refresh_token`.
- `scim_service_providers`: token stays `enc:<ciphertext>`.

### Admin (delta)

- `app_config`: the one final settings table (one row per key); drop `app_settings` and its
  trigger, indexes, and seed rows.
- `audit_logs`: append-only; `user_id` stays a plain UUID reference without `ON DELETE
  CASCADE` so history survives user deletion.
- `api_keys`, `apis`, `api_permissions`, `custom_claims`: unchanged shape.

### Webhook (restructure)

- `webhook_endpoints` (renamed from `webhook_events`): endpoint configuration — name, URL,
  event subscriptions, `secret_enc TEXT CHECK (secret_enc LIKE 'enc:%')`, timestamps.
- `webhook_deliveries` (replaces `webhook_logs`): one row per endpoint per event — `id`,
  `webhook_id FK`, `event`, `body BYTEA NOT NULL` (the exact canonical bytes; retries sign and
  deliver these bytes), `status`, `attempt_count`, `created_at`, `delivered_at`.
- `webhook_delivery_attempts` (new): one row per attempt — `delivery_id FK`, `attempt_number`,
  `response_status`, `error`, `duration_ms`, `created_at`; redacted response metadata only.
- The outbox write (delivery row + queue task) stays in the domain transaction; the queue is
  notified only after commit.
- Delivery `status` is a fixed set: `pending`, `succeeded`, `failed`. `attempt_count` increments
  per attempt row; `delivered_at` stamps the first success. The request snapshot (method, target,
  signature headers) is redacted metadata on the delivery, not re-serialized JSONB of the body.
- Endpoint deletion keeps deliveries: `webhook_deliveries.webhook_id` is `ON DELETE SET NULL` so
  the audit trail survives; attempts cascade with their delivery.
### Cleanup migration

One new migration drops: `app_settings`, `user_phones`, `invitations`, `mfa_keys`,
`oauth_connections`, `oidc_device_codes`, `file_stores`, `refresh_tokens`, the dead `sessions`
columns (`totp_pending`, `oauth_groups`, `oauth_name`, `oauth_sub`), and converts `sessions.id`
to UUID where the live code allows. The webhook restructure (rename + new tables + data
migration) lands in the same or a following migration — never by editing an applied migration.

Cross-module references use foreign keys to UUIDs and consumer-side interfaces in Go. A table must
not be read or written directly by a non-owner module.

## Data rules

### Identity and credentials

- Keep users, credentials, sessions, and one-time tokens in separate tables.
- Passwords, reset tokens, session tokens, API keys, and recovery codes are hashed when the server
  only needs verification.
- Use purpose constraints and expiry/consumption timestamps for one-time tokens.
- Store pending MFA authentication separately from full sessions.
- Use normalized unique indexes for login identifiers such as lowercase email or username.
- Prefer explicit status, `revoked_at`, `used_at`, and `expires_at` columns over overloaded JSONB.

### TOTP MFA

- Store TOTP state in a dedicated table owned by identity.
- Store the active seed as `enc:<ciphertext>` through `pkg/crypto`.
- Store recovery codes as individual hashes with `used_at`; do not store them as JSONB.
- Track the last accepted time step to prevent replay.
- Add foreign keys and a uniqueness rule for the intended enrollment cardinality.

### Federation

- Keep redirect URIs in a child table, not a delimited column.
- Store recoverable provider secrets and private keys with `enc:`; hash client secrets when only
  comparison is required.
- Model authorization codes and refresh tokens as expiring, revocable, purpose-specific rows.
- Keep public keys separate from private key material and do not encrypt public data unnecessarily.

### Application settings

- Keep the key/value catalog in the application layer and store one row per setting key.
- Choose one final table, `app_config`; remove the duplicate legacy-shaped `app_settings` table.
- Sensitive settings that must be recovered are stored with `enc:`; non-sensitive settings remain
  plain and hash-only values are not encrypted.
- Redaction is an API concern; consumers receive resolved values only through the appconfig port.
- Do not store an entire configuration document as one JSONB value.

### Webhooks and durable delivery

- Keep endpoint configuration, event subscriptions, deliveries, and attempts in separate tables.
- Replace the current endpoint/log table shape with explicit endpoint, delivery, and attempt tables;
  do not keep `webhook_events` or `webhook_logs` as compatibility aliases.
- Store endpoint signing secrets as `enc:` and never return stored ciphertext through the API.
- Store the exact canonical request body as `BYTEA` (or an equivalent immutable byte representation)
  so retries sign and deliver identical bytes.
- Record delivery status, attempt number, timestamps, response status, and redacted response metadata.
- Write the outbox record in the same transaction as the domain mutation. Notify the existing queue
  only after commit.

### Audit

- Keep audit rows append-only with actor, action, target, metadata, and timestamp.
- Metadata is JSONB only for non-secret structured context. Never store secrets, ciphertext, tokens,
  passwords, or full sensitive request bodies.
- Do not cascade-delete audit history with user or endpoint records unless retention explicitly says so.

## Constraints, indexes, and transactions

- Use `NOT NULL`, foreign keys, unique constraints, and stable `CHECK` constraints in the initial
  migration instead of validating everything in Go.
- Known recoverable-secret columns must have a marker constraint such as `LIKE 'enc:%'`; cryptographic
  validity remains an application test.
- Add indexes for every lookup used by authentication, expiry cleanup, queue claiming, ownership,
  and pagination. Avoid speculative indexes.
- Use one transaction for a domain mutation plus its audit/outbox record. Do not expose a partially
  committed state to the queue worker.
- Keep cleanup jobs bounded and index-backed. Do not use unbounded table scans in request handlers.
- Do not add RLS, partitioning, database triggers for business behavior, or a second queue until a
  measured requirement justifies it.

## Atomic tasks

1. Inventory active migrations and map every table to one owning module. Mark LDAP, Application
   Images, and obsolete compatibility tables for removal. Commit: `docs: define database ownership`.
2. Define the final identity schema for users, credentials, sessions, tokens, and pending auth with
   constraints and indexes. Commit: `docs: define identity database contracts`.
3. Define the final MFA and federation schema, including encrypted fields, hashes, expiry, and key
   ownership. Commit: `docs: define MFA and federation database contracts`.
4. Define app settings, audit, webhook, outbox, delivery, and attempt tables with exact payload
   storage and transaction boundaries. Commit: `docs: define admin and webhook database contracts`.
5. Add or adjust final migrations. Do not modify applied migrations and do not add compatibility
   columns, fallback views, dual writes, or legacy readers. Drop obsolete app settings, LDAP/image
   columns, session flags, and webhook table shapes in the cleanup migration. Commit:
   `feat: add final postgres schema`.
6. Add real-Postgres tests for constraints, unique identifiers, expiry/revocation, secret prefixes,
   outbox atomicity, exact webhook bytes, and cleanup indexes. Commit: `test: verify postgres schema contracts`.
7. Remove obsolete tables, columns, routes, and dev fixtures for excluded features. Confirm the
   resulting schema is clean on a fresh database. Commit: `chore: remove excluded database residue`.

## Validation and uncertainty

- Use focused Postgres tests with explicit timeouts and fail-fast behavior.
- Apply migrations to a fresh database and inspect constraints/indexes directly.
- No test may depend on a pre-cleanup schema or compatibility fixture.
- If the upstream schema or a field meaning is ambiguous, stop the dependent task, show the
  evidence, and ask the project owner. Do not preserve both interpretations in the schema.

## Acceptance criteria

- Every active table has one module owner.
- Fresh migrations create the final schema without compatibility artifacts.
- No legacy code, legacy reader, fallback format, dual write, or backward-compatibility branch exists.
- Recoverable secrets use `enc:` and verification-only values use hashes.
- Identity, MFA, federation, admin, webhook, audit, and outbox transaction tests pass.
