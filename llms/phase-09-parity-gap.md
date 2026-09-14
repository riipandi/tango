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
Owner: `modules/appconfig` (already owns `test-email`).

- [ ] Migration (goose, verbatim DDL): `app_config` table — `key TEXT PRIMARY KEY`, `value TEXT
      NOT NULL`, `updated_at`. Key/value rows; no embedded schema, no auto-create.
- [ ] Keys modeled as one typed Go struct (snake_case JSON fields, string-coerced values).
      Port the upstream key list as reference, but trim to tango-relevant keys; SMTP stays
      env-backed (deviation: tango mail config never lived in the DB).
- [ ] Public vs admin split via a key allowlist (upstream semantics), envelope responses,
      `PUT` upserts + audit event `application_configuration_updated`.
- [ ] Migrate the phase-8 env-backed `LDAPSettings` to appconfig-backed with env fallback
      (stretch — keep if the refactor balloons).
- [ ] Tests + Yaak: fill bodies for the three existing draft requests, live test, flip statuses.

## 9C — Profile pictures & client logos (8 endpoints)

Build on `internal/storage` + the `modules/appimage` upload pattern (5 MiB cap, MIME allowlist,
`Cache-Control` + `skipCache`, delete tombstones). No upstream gorm/gin code.

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

- [ ] Migration: nullable `profile_picture_path` on `users`, `logo_path` on `oidc_clients`
      (singleton columns — simpler than upstream's file-store rows; appimage keeps `file_stores`).
- [ ] Serve `GET .../profile-picture.png` and client logo as **bare bytes** (jwks-style
      no-envelope deviation); errors still use the responder envelope.
- [ ] Dark-variant client logo (`has_dark_logo`) only if the SPA needs it — otherwise skip and
      record the deviation.
- [ ] Audit events, tests (upload/replace/reset/404-default), Yaak requests, live test, status flips.

## 9D — CIMD refresh + explicit non-goals

- [ ] `POST /api/oidc/clients/{id}/refresh` — re-fetch the metadata document for CIMD clients,
      validate (URL allowlist, size cap), update the row without clobbering concurrent admin
      edits (upstream regression test: `UpdateClient_CIMDDoesNotOverwriteConcurrentMetadataRefresh`
      — port the behavior, not the test). Depends on the CIMD client-id decoding already present
      (`middleware.NewClientIDParamMiddleware` equivalent in tango transport).
- [ ] `GET /api/storage/sqlite-warning` — **won't port** (Postgres-only); keep recorded in
      `endpoint-reference.md` as a non-goal so future audits stop flagging it.

## Gate (applies to every sub-phase)

1. `task test` green (all three suites), `task lint`, `task typecheck` untouched.
2. Yaak request created/updated via MCP and sent against the running server before ticking.
3. `endpoint-reference.md` + `README.md` gap register updated in the same change set.
4. New config keys documented in `.env.example` (sync test enforces it).
5. No upstream shape leaks: envelope everywhere except declared bare-document endpoints.
