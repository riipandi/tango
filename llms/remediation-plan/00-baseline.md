---
status: done
updated: 2026-09-17
owner: tango-remediation
---

# Phase 0 — Baseline and Inventory

## Goal

Produce a final, auditable record of the repository before any source or schema change.

## Task 0.1 — Capture baseline

Record the repository state at the start:

- `git status --short` and the base commit;
- the number of in-scope, excluded, and partial endpoints in the endpoint reference;
- every `Encrypt`/`Decrypt` consumer;
- every route mount;
- every table and column owned by the migrations;
- the results of `gofmt -l`, `go vet`, focused tests, frontend tests, and the full gate;
- the status of Docker, Postgres, the tango server, the upstream parity instance, and the Yaak
  workspace.

Store the results as evidence in the remediation documents or update the relevant matrix. Do not
change runtime behavior in this task.

Validation: every command uses an explicit timeout; environment failures are recorded separately
from source failures.

Commit: `docs: capture remediation baseline`

## Task 0.2 — Inventory final-shape violations

Build an inventory table with the columns `finding`, `owner`, `source`, `required final shape`,
`migration impact`, `test impact`, and `Yaak impact`. It must cover at minimum:

- `oidc_clients.secret` and the credentials/multi-secret shape;
- `LegacySecretID` and the secret fallback;
- `oidc_clients.image_type` and `dark_image_type`;
- the runtime-unused `oidc_refresh_tokens`;
- the Application Images Yaak folder/request;
- the stale `planned` request description;
- responder imports in service/module files;
- ad-hoc request validation;
- public `err.Error()` responses;
- missing live evidence.

Do not fix the findings in this task; the inventory is the input for the following phases.

Commit: `docs: inventory final-shape violations`

## Evidence — Task 0.1 (commit `e9d6f7a`)

Captured at commit `cacaa6355c1147a2d2b3712595684947c72f941a` (branch `overhaul-stack`),
2026-09-17. `git status --short`: clean. All commands ran with explicit timeouts; no source or
runtime behavior changed.

Environment status:

| Component | Status |
| --- | --- |
| Docker daemon | running |
| Postgres (`tango-pgsql-1`, :5432) | up 3h (healthy) |
| Mailpit (`tango-mailpit-1`, :1025/:8025) | up 3h (healthy) |
| Upstream parity instance (`tango-pocketid-1`, :1411) | up ~1h (healthy) |
| Tango server (`:3080`) | not running at capture time |
| Redis (`tango-redis-1`), nginx (`:3443/:8443/:9180`), silo (:9100) | up (compose extras) |
| Yaak workspace | present at `~/Library/Application Support/app.yaak.desktop` (`db.sqlite`) |

Endpoint reference counts (`llms/endpoint-reference.md`): done 129, partial 1
(`PUT /api/users/me` — profile fields only), planned 0, excluded 14.

`pkg/crypto.Cipher` Encrypt/Decrypt consumers: `modules/webhook/service.go` (endpoint secret
write + read), `modules/identity/totp/service.go` (TOTP seed enroll + verify),
`modules/federation/jwks/service.go` (private key PEM), `modules/federation/scimsync/store.go`
(bearer token write + read); tests in `pkg/crypto`, `modules/webhook`, `modules/identity/totp`.

Route mounts (runtime, `internal/transport/http.go` + module `APIRoutes`):

- Root: `/static/*`, `/healthz`, `/.well-known/version`, API root `/api`, `/api/healthz`, `/api/version/{current,latest}`
- `identity/session`: `/api/auth/sign-in|sign-out`, `/api/auth/session`
- `identity/recovery`: `/api/auth/forgot-password`, `/api/auth/reset-password`
- `identity/account`: `/api/account{,/password,/sessions,/sessions/{id}}`
- `identity/totp`: `/api/mfa/totp/*`
- `identity/webauthn`: `/api/webauthn/*`
- `identity/signup`: `/api/signup{,/setup}`
- `identity/devicelogin`: `/api/device-login/requests{,/{id}/exchange}`
- `identity/onetimeaccess`: `/api/one-time-access-email`, `/api/one-time-access-token/{token}`
- `identity/emailverification`: email verification create/verify surface
- `identity/user` (admin): `/api/users` CRUD + profile picture + API-key auth path
- `identity/usergroup`: `/api/user-groups` CRUD + membership + allowed clients
- `admin/apikey`: `/api/api-keys` CRUD + renew
- `admin/apiaccess`: `/api/apis` CRUD, `/api/api-access/{clientId}/apis{,/assignable-apis}`
- `admin/customclaim`: `/api/custom-claims/...` (user + user-group surfaces)
- `admin/auditlog`: audit log list/detail surface
- `admin/appconfig`: `/api/application-configuration` (public + admin variants)
- `federation/discovery`: `/.well-known/jwks.json`, `/.well-known/openid-configuration`, `/.well-known/oauth-authorization-server`
- `federation/oidc`: `/api/oidc/authorize`, `/api/token`, `/api/userinfo`, introspect, end-session, PAR, device (authorize/verify/info), `/api/oidc/clients` admin CRUD + secrets/logo/meta/preview/refresh, interaction endpoints, client logo GET
- `federation/scimsync`: `/api/scim/service-provider` CRUD + sync, SCIM provider GET on client
- `webhook`: webhook endpoint + delivery surface

Tables owned by `database/migrations/` (00001–00008 at capture time):
`deleted_records`; `users`, `user_passwords`, `user_groups`, `user_groups_users`, `sessions`,
`auth_tokens`, `signup_tokens`, `signup_tokens_user_groups`, `device_login_requests`,
`audit_logs`; `webauthn_credentials`, `webauthn_sessions`, `user_mfa_totp`,
`user_mfa_recovery_codes`, `user_mfa_pending`; `jwks`, `oidc_clients`, `custom_claims`,
`oidc_authorization_codes`, `user_authorized_oidc_clients`, `oidc_clients_allowed_user_groups`,
`user_groups_allowed_oidc_clients`, `oidc_refresh_tokens`, `oidc_device_codes`,
`oauth2_sessions`, `oauth2_jtis`, `interaction_sessions`, `scim_service_providers`; `apis`,
`api_permissions`, `oidc_client_api_grants`, `oidc_client_api_grant_permissions`, `app_config`,
`api_keys`; `webhook_endpoints`, `webhook_deliveries`, `webhook_delivery_attempts`;
`rate_limits`; `queue_tasks`, `queue_tasks_completed`. Column-level inventory is defined by the
migration files themselves (verbatim DDL).

Gate results at baseline: `gofmt -l .` clean; `go vet ./...` pass;
`go test -tags release ./...` pass (exit 0); frontend `pnpm exec vitest run` pass (9 files,
76/76). Note: `pnpm -C web` is not a valid entrypoint — the pnpm workspace root is the repo root.

No LDAP artifacts exist; the `compose.yaml` upstream parity instance holds real schema state and
was left untouched.

## Evidence — Task 0.2 (commit `c9c4efb`)

Sources: code search at `cacaa6355c11`, `llms/endpoint-reference.md`, and the Yaak workspace
(`app.yaak.desktop/db.sqlite`).

| # | Finding | Owner | Source | Required final shape | Migration impact | Test impact | Yaak impact |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | `oidc_clients.secret` still written/read as mirror of the first credentials entry | federation/oidc | `store_client.go` (`AddClientSecret` mirrors first entry, `DeleteClientSecret` clears column, `clientColumns` selects `c.secret`) | Single storage shape: credentials JSONB only; no mirror column | Task 1.2 drop column | Store + authz-code tests must not depend on mirror | None (secret flows unchanged on the wire) |
| 2 | `LegacySecretID` synthetic entry + legacy fallback comparison | federation/oidc | `store_client.go:74`, `scanClient` synthesizes `{ID: "legacy"}` entry; `token.go secretMatches` falls back to `client.SecretHash` | Verification reads credentials list only; no synthetic entry, no fallback | Task 1.1 removes code path; Task 1.2 drops column | New tests: create, multi-secret, expiry, inactive, rotation, deletion, auth after column gone | None |
| 3 | `oidc_clients.image_type` / `dark_image_type` columns still read/written | federation/oidc, admin/apiaccess | `store_client.go` (`clientColumns`, `scanClient`, logo update), `handler_meta.go` (`has_logo`), `admin/apiaccess/store.go:233` | `logo_path` is the only logo storage; meta views derive presence from it | Task 1.2 drop both columns + contract test | Meta/apiaccess projection tests updated | None (responses keep `has_logo` shape) |
| 4 | `oidc_refresh_tokens` table has no runtime caller | federation/oidc, database | `00004_create_federation_tables.sql`; only references: `migrator_test.go:83`, `schema_fresh_test.go:28`. Runtime refresh tokens live in `oauth2_sessions` | Table + index dropped | Task 1.3 new migration; migrator count/assertions + schema contract + backup tests updated | Migrator/schema tests updated | None |
| 5 | Application Images Yaak folder exists for an excluded feature | yaak artifacts | `app.yaak.desktop/db.sqlite` folder `fl_3VCPmqfLP6`, 0 requests | Folder removed; no Application Images requests | None | None | Task 2.1 cleanup |
| 6 | Stale "planned (phase 8)" request description | yaak artifacts | Yaak request "Refresh client metadata document" (`POST …/clients/{id}/refresh` description) | Description states the implemented contract, no phase marker | None | None | Task 2.3 cleanup |
| 7 | `pkg/responder` imported in service/area files | federation/oidc, federation/discovery | `modules/federation/oidc/{authorize,token,device,userinfo,par}.go`, `modules/federation/discovery/schema.go` (module `module.go` files that host handlers are not violations) | responder used only in handler files; area files return errors | None | None | None |
| 8 | Ad-hoc request validation bypasses `pkg/validate` | identity, federation/oidc | `webauthn/handler.go:94`, `onetimeaccess/handler.go:209`, `oidc/device.go` (manual `PostFormValue` guards), `signup/handler.go:163` | Code-first `Validate()` methods; 422 via responder only | None | Validation tests per endpoint | None |
| 9 | Public responses leak `err.Error()` internals | identity, admin, federation | 24+ `responder.Fail(..., err.Error())` sites: `signup/handler.go:228-239`, `devicelogin/handler.go:105,125`, `scimsync/handler.go:150` (`WithError`), `user/handler.go:28-30`, `usergroup/handler.go:24,244`, `apiaccess/handler.go:21-23`, `apikey/handler.go:22`, `customclaim/handler.go:24`, `totp/handler.go:280`, `webhook/handler.go:24-26`, `account/handler.go:201`, `recovery/recovery.go:274`, `oidc/handler.go:201,289` | Stable public messages; error taxonomy mapped to status + fixed text | None | Error-message assertions per surface | None |
| 10 | Missing live evidence per endpoint row | verification | `llms/endpoint-reference.md` Evidence column cites unit tests only; no per-row live Yaak evidence registry; tango server was not running at baseline | Every row carries live Yaak evidence against the running server | None | None | Phase 5 live sweep |

Sequencing dependencies: findings 1+2 gate Task 1.1 (code) → Task 1.2 (column drop); finding 4
gates Task 1.3; findings 3 and 1.2 share the migration; findings 7–9 belong to Phase 3/4 scopes;
5–6 to Phase 2; 10 to Phase 5.
