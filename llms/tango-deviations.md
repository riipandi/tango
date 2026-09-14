---
status: current
updated: 2026-09-14
---

# Tango Deviations & Tango-only Features

Read this before porting any upstream Pocket ID code. Upstream is the functional reference;
tango deliberately re-expresses it. "Upstream" = Pocket ID v2.14.0 (gin + gorm + SQLite/MySQL/
Postgres). Tango = chi + pgx + sqlbuilder + Postgres-only.

## Structural deviations (apply to every endpoint)

| Area             | Upstream                                          | Tango                                                                |
| ---------------- | ------------------------------------------------- | -------------------------------------------------------------------- |
| Response envelope | Bare JSON body or `{error}` object               | `{status, message?, data, error?, metadata{status_code, request_id, …pagination}, links}` via `pkg/responder` |
| Pagination       | `search` + `pagination[page]` query params        | `page`/`limit` (default 25, max 100, `-1` = all) + `sort_by`/`sort_order` (asc/desc) |
| JSON field names | camelCase                                         | snake_case                                                            |
| IDs              | UUID / string IDs                                 | TypeID per module (`go.jetify.com/typeid`); token/code rows use SHA-256 keys + DB `uuidv7()` |
| Schema           | gorm automigrate / embedded                       | `database/migrations/` (goose) owns schema; never embed or auto-create |
| Validation       | controller-level ad-hoc checks                    | `pkg/validate` (ozzo v4, `Validate()` methods) → 422 via `pkg/responder` |
| Tests            | sqlite/in-memory harness                          | `pkg/testutils.StartPostgres` (testcontainers); unique names, no absolute-count assertions |
| Stack            | gin, gorm                                         | chi router, pgx + `huandu/go-sqlbuilder`                              |

## Bare-document endpoints (no envelope — by protocol, like upstream)

`/.well-known/jwks.json`, `/.well-known/openid-configuration`,
`/.well-known/oauth-authorization-server`, `/api/oidc/token` + `/api/oidc/userinfo`
(OAuth/OIDC wire formats), `/api/oidc/introspect` (RFC 7662), `GET /healthz` (upstream 204
variant), planned: profile-picture/client-logo bytes (phase 9C).

## Tango-only endpoints (no upstream counterpart — all done)

| Surface            | Endpoints                                                                 |
| ------------------ | ------------------------------------------------------------------------- |
| Auth/session       | `POST /api/auth/sign-in`, `POST /api/auth/sign-out`, `GET /api/auth/session` (cookie session model; upstream uses JWT + webauthn session store) |
| Account            | `GET/PATCH /api/account`, `PUT /api/account/password`, `GET /api/account/sessions`, `DELETE /api/account/sessions/{id}` |
| Webhooks           | `GET/POST /api/webhooks`, `GET/PUT/DELETE /api/webhooks/{id}`, `POST /api/webhooks/{id}/rotate-secret`, `POST /api/webhooks/{id}/test`, `GET /api/webhooks/{id}/logs`, `GET /api/webhook-logs` |
| Custom claims      | Single-claim CRUD: `POST /api/custom-claims/user/{userId}`, `PUT/DELETE …/{claimId}`, same for `/user-group/` (upstream only has list-replace PUT) |
| OIDC interaction   | `GET /api/oidc/interaction/{id}`, `POST /api/oidc/interaction/{id}/approve` |
| Version (extra)    | `GET /.well-known/version`                                                 |
| Health (extra)     | `GET /api/healthz` (JSON variant; upstream root `/healthz` is bare 204)    |
| Test email         | `POST /api/application-configuration/test-email` accepts an explicit-recipient body (upstream emails the admin) |
| Machine auth       | `X-API-KEY` guard on the users CRUD surface: `GET/POST /api/users`, `GET/PUT/DELETE /api/users/{id}` (session-auth admin routes accept either guard) |
| Userinfo method    | `POST /api/oidc/userinfo` mounted alongside GET (upstream: GET only)       |

## Behavioral deviations in shared endpoints

- `PUT /api/users/me` — profile fields only; email changes stay admin-gated.
- `POST /api/one-time-access-email` — 204 without minting until appconfig policy exists
  (phase 9B); upstream mints + emails immediately.
- `POST /api/signup/setup` — first-admin bootstrap only; 409 once an admin exists.
- Device login — own `device_login_requests` table; upstream uses the francis actor framework.
- `POST /api/oidc/introspect` — client-scoped: tokens of other clients introspect as
  `active: false`; form body with client auth (Basic or form credentials).
- End-session / refresh — refresh-token rotation per use; reuse kills the whole token family.
- LDAP/SMTP/app config — admin-editable in `app_config` with env-backed defaults (stretch of
  phase 9B): `smtp_*`/`ldap_*` keys fold env → catalog → DB overrides; `smtp_password` and
  `ldap_bind_password` redact in the admin view. The mailer resolves relay settings per send;
  LDAP sync reads its settings through the same surface. Upstream parity, different mechanics.
- WebAuthn finish endpoints take `session_id` as a query param (upstream: not in swagger).
- CIMD — **CIMD-lite** (phase 9D): admin-registered metadata-URL clients instead of upstream's
  dynamic URL-as-client-id flow. No `~base64url` client-id decode middleware, no
  authorize-time materialization (fosite `CIMDResolver` stack not ported). A client created
  with `metadata_url` materializes `client_name`/`redirect_uris`/`logout_callback_uris`/
  `grant_types` from the document (https-only, 1 MiB cap, allowlist via `cimd_url_allowlist`
  appconfig key, default deny); `POST /api/oidc/clients/{id}/refresh` re-fetches; admin
  updates never write document-owned fields.

## Deliberate non-goals

- `GET /api/storage/sqlite-warning` — Postgres-only, never ported.
- SQLite/MySQL storage backends — Postgres only.
- Geolite IP enrichment — optional upstream tier, not ported (audit log IP column exists).
- Upstream invitations flow (signup tokens cover the need; revisit if requested).

Status source of truth: `llms/endpoint-reference.md` + the gap register in `README.md`.
Next work: `llms/phase-09-parity-gap.md`.
