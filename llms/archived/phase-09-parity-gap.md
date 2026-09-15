---
status: planned
updated: 2026-09-14
---

# Phase 9 — Upstream Parity Gaps

Close the remaining 16 upstream endpoints (Pocket ID v2.14.0, 113 total; 97 already live).
Porting rule: **never adopt upstream code or shapes verbatim** — every endpoint is re-expressed
in tango conventions. Read `llms/tango-deviations.md` before writing any handler.

Sub-phases are independent; 9A is trivial and lands first, 9C needs 9B's appconfig only for the
`public_url`-derived cache keys (fall back to env until then).

## 9A — OIDC client context surface (phase-5 leftovers)

Three small endpoints, no new tables except the allowlist junction. Owners: `modules/federation/oidc`,
`modules/identity/usergroup`. **Status: done (2026-09-14)** — migration `00028`, meta + preview in
`modules/federation/oidc/handler_meta.go`, allowlist in the usergroup store; live-tested via Yaak.

| Method | Path                                       | Notes                                                     |
| ------ | ------------------------------------------ | --------------------------------------------------------- |
| GET    | `/api/oidc/clients/{id}/meta`              | Admin view of the client's metadata document              |
| GET    | `/api/oidc/clients/{id}/preview/{userId}`  | Claims/redirect preview for one user + scopes             |
| PUT    | `/api/user-groups/{id}/allowed-oidc-clients` | List-replace a group's client allowlist                 |

- [x] `meta` — serve the metadata document derived from the stored client row (response shape
      mirrors the discovery/client-registration document: snake_case, envelope-wrapped). CIMD
      re-fetch wiring stays out until 9D.
- [x] `preview` — reuse the ID-token claim pipeline (custom claims for user + groups, scope
      filtering) in a new `preview.go`; upstream reference `internal/oidc/preview.go`
      (ClientPreviewBuilder) is logic-only guidance. Query param: `scopes` (comma-separated).
- [x] `allowed-oidc-clients` — usergroup module: body `{"oidc_client_ids": []}` (snake_case;
      upstream camelCase `oidcClientIds` stays a documented deviation), validate TypeIDs, mutate
      through the existing allowlist store, audit event `user_group.allowed_clients_updated`.
- [x] Tests per endpoint (`pkg/testutils.StartPostgres`); Yaak requests already exist — send
      against the running server, then flip the three rows in `endpoint-reference.md`.

## 9B — Application configuration module

Endpoints: `GET /api/application-configuration` (public subset), `GET
/api/application-configuration/all` (admin), `PUT /api/application-configuration` (admin).
Owner: `modules/appconfig` (also owns `test-email`). **Status: done (2026-09-14)** — migration
`00029` (`app_config` key/value), key catalog in `config.go`, store + partial PUT live-tested.

- [x] Migration (goose, verbatim DDL): `app_config` table — `key TEXT PRIMARY KEY`, `value TEXT
      NOT NULL`, `updated_at`. Key/value rows; no embedded schema, no auto-create.
- [x] Keys modeled as one explicit catalog (key, type, public flag, env-backed default) — no
      reflect/tag magic. Key set trimmed to tango-relevant settings; SMTP and LDAP stay env-only
      (documented deviation); no sensitive keys, so /all needs no redaction.
- [x] Public vs admin split via the `Public` flag; partial PUT (omitted keys keep their value —
      deviation from the upstream all-required binding); unknown keys ignored.
- [ ] Stretch: migrate the phase-8 env-backed `LDAPSettings` to appconfig-backed with env
      fallback (deferred — env fallback works and LDAP reconfig is rare).
- [x] Tests (`TestConfigCRUD`, testcontainers) + Yaak bodies filled and live-tested.

## 9C — Profile pictures & client logos (8 endpoints)

Build on `internal/storage` + the `modules/appimage` upload pattern (5 MiB cap, MIME allowlist,
`Cache-Control` + `skipCache`, delete tombstones). No upstream gorm/gin code.
**Status: done (2026-09-14)** — migration `00030`, shared blob backend wired through the
registry; live-tested via Yaak. Deviation: no transcoding (upstream squares to PNG) — uploads
are stored as-is behind the allowlist; dark-variant client logos skipped.

| Method | Path                                        | Notes                                  |
| ------ | ------------------------------------------- | -------------------------------------- |
| PUT    | `/api/users/me/profile-picture`             | Self upload (session auth)             |
| DELETE | `/api/users/me/profile-picture`             | Reset to default                       |
| PUT    | `/api/users/{id}/profile-picture`           | Admin upload                           |
| DELETE | `/api/users/{id}/profile-picture`           | Admin reset                            |
| GET    | `/api/users/{id}/profile-picture.png`       | Bare image bytes (no envelope)         |
| GET    | `/api/oidc/clients/{id}/logo`               | Bare image bytes                       |
| POST   | `/api/oidc/clients/{id}/logo`               | Admin upload                           |
| DELETE | `/api/oidc/clients/{id}/logo`               | Admin delete                           |

- [x] Migration: nullable `profile_picture_path` on `users`, `logo_path` on `oidc_clients`
      (singleton columns — simpler than upstream's file-store rows; appimage keeps `file_stores`).
- [x] Serve `GET .../profile-picture.png` and client logo as **bare bytes** (jwks-style
      no-envelope deviation); errors still use the responder envelope.
- [x] Dark-variant client logo skipped (single logo; recorded as the documented deviation).
- [x] Audit events, tests (upload/replace/reset/404-default), Yaak requests, live test, status flips.

## 9D — CIMD refresh + explicit non-goals (done)

- [x] `POST /api/oidc/clients/{id}/refresh` — re-fetch the metadata document for CIMD clients,
      validate (URL allowlist default-deny, https-only, 1 MiB cap), update the row without
      clobbering concurrent admin edits (upstream regression test:
      `UpdateClient_CIMDDoesNotOverwriteConcurrentMetadataRefresh` — behavior ported, not the
      test). **CIMD-lite scope decision**: the planned precondition "CIMD client-id decoding
      already present" was wrong — tango never had `~base64url` client-id decoding or
      authorize-time materialization. Instead of porting the full fosite CIMDResolver stack,
      tango ships admin-registered metadata-URL clients: `POST /api/oidc/clients` accepts a
      `metadata_url`; the document materializes `client_name`/`redirect_uris`/
      `logout_callback_uris`/`grant_types` into the row (client_type `cimd`, public, PKCE),
      and admin updates skip those document-owned fields. Deviation recorded in
      `tango-deviations.md`.

**Phase 9 final parity**: 112/113 upstream endpoints implemented; 1 non-goal
(`sqlite-warning`). Optional stretch landed: `smtp_*`/`ldap_*` settings moved into
`app_config` with env-backed defaults (mailer resolves per send; LDAP sync reads the same
surface).

## Gate (applies to every sub-phase)

1. `task test` green (all three suites), `task lint`, `task typecheck` untouched.
2. Yaak request created/updated via MCP and sent against the running server before ticking.
3. `endpoint-reference.md` + `README.md` gap register updated in the same change set.
4. New config keys documented in `.env.example` (sync test enforces it).
5. No upstream shape leaks: envelope everywhere except declared bare-document endpoints.
