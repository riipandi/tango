# Phase 0 — Baseline Evidence (Task 0.1)

Captured at commit `cacaa6355c1147a2d2b3712595684947c72f941a` (branch `overhaul-stack`),
2026-09-17. `git status --short`: clean.

## Environment status

| Component | Status |
| --- | --- |
| Docker daemon | running |
| Postgres (`tango-pgsql-1`, :5432) | up 3h (healthy) |
| Mailpit (`tango-mailpit-1`, :1025/:8025) | up 3h (healthy) |
| Upstream parity instance (`tango-pocketid-1`, :1411) | up ~1h (healthy) |
| Tango server (`:3080`) | not running at capture time |
| Redis (`tango-redis-1`, :6379), nginx (`:3443/:8443/:9180`), silo (:9100) | up (compose extras) |
| Yaak workspace | present at `~/Library/Application Support/app.yaak.desktop` (`db.sqlite` with folders/requests) |

All commands below ran with explicit timeouts. No source or runtime behavior was changed.

## Endpoint reference counts (`llms/endpoint-reference.md`)

| Status | Rows |
| --- | --- |
| done | 129 |
| partial | 1 (`PUT /api/users/me` — profile fields only) |
| planned | 0 |
| excluded | 14 |

## `pkg/crypto.Cipher` Encrypt/Decrypt consumers

- `modules/webhook/service.go` (endpoint secret write + read, deliveries signing)
- `modules/identity/totp/service.go` (TOTP seed enroll + verify)
- `modules/federation/jwks/service.go` (private key PEM)
- `modules/federation/scimsync/store.go` (bearer token write + read)
- Tests: `pkg/crypto/encryption_test.go`, `modules/webhook/secret_contract_test.go`, `modules/identity/totp/totp_test.go`

## Route mounts (runtime, `internal/transport/http.go` + module `APIRoutes`)

- Root (unwrapped): `/static/*`, `/healthz`, `/.well-known/version`, API root `/api`, `/api/healthz`, `/api/version/{current,latest}`
- `modules/identity/session`: `/api/auth/sign-in|sign-out`, `/api/auth/session`
- `modules/identity/recovery`: `/api/auth/forgot-password`, `/api/auth/reset-password`
- `modules/identity/account`: `/api/account{,/password,/sessions,/sessions/{id}}`
- `modules/identity/totp`: `/api/mfa/totp/*` (enroll/verify/confirm/manage)
- `modules/identity/webauthn`: `/api/webauthn/*` (register/login begin+finish, credentials CRUD)
- `modules/identity/signup`: `/api/signup{,/setup}`
- `modules/identity/devicelogin`: `/api/device-login/requests{,/{id}/exchange}`
- `modules/identity/onetimeaccess`: `/api/one-time-access-email`, `/api/one-time-access-token/{token}`
- `modules/identity/emailverification`: email verification create/verify surface
- `modules/identity/user` (admin-guarded): `/api/users` CRUD + profile picture + API-key auth path
- `modules/identity/usergroup`: `/api/user-groups` CRUD + membership + allowed clients
- `modules/admin/apikey`: `/api/api-keys` CRUD + renew
- `modules/admin/apiaccess`: `/api/apis` CRUD, `/api/api-access/{clientId}/apis{,/assignable-apis}`
- `modules/admin/customclaim`: `/api/custom-claims/...` (user + user-group claim surfaces)
- `modules/admin/auditlog`: audit log list/detail surface
- `modules/admin/appconfig`: `/api/application-configuration` (public + admin variants)
- `modules/federation/discovery`: `/.well-known/jwks.json`, `/.well-known/openid-configuration`, `/.well-known/oauth-authorization-server`
- `modules/federation/oidc`: `/api/oidc/authorize`, `/api/token`, `/api/userinfo`, introspect, end-session, PAR, device (authorize/verify/info), `/api/oidc/clients` admin CRUD + secrets/logo/meta/preview/refresh, interaction endpoints, client logo GET
- `modules/federation/scimsync`: `/api/scim/service-provider` CRUD + sync, SCIM provider GET on client
- `modules/webhook`: webhook endpoint + delivery surface

## Tables owned by `database/migrations/` (00001–00008)

`deleted_records`; `users`, `user_passwords`, `user_groups`, `user_groups_users`, `sessions`,
`auth_tokens`, `signup_tokens`, `signup_tokens_user_groups`, `device_login_requests`,
`audit_logs`; `webauthn_credentials`, `webauthn_sessions`, `user_mfa_totp`,
`user_mfa_recovery_codes`, `user_mfa_pending`; `jwks`, `oidc_clients`, `custom_claims`,
`oidc_authorization_codes`, `user_authorized_oidc_clients`, `oidc_clients_allowed_user_groups`,
`user_groups_allowed_oidc_clients`, `oidc_refresh_tokens`, `oidc_device_codes`,
`oauth2_sessions`, `oauth2_jtis`, `interaction_sessions`, `scim_service_providers`; `apis`,
`api_permissions`, `oidc_client_api_grants`, `oidc_client_api_grant_permissions`, `app_config`,
`api_keys`; `webhook_endpoints`, `webhook_deliveries`, `webhook_delivery_attempts`;
`rate_limits`; `queue_tasks`, `queue_tasks_completed`.

Column-level inventory is defined by the migration files themselves (verbatim DDL). Current
migration count: 8; migrator assertions live in `database/migrator_test.go` and
`cmd/launcher/db_migrate*_test.go`.

## Gate results at baseline

| Check | Result |
| --- | --- |
| `gofmt -l .` | clean (no output) |
| `go vet ./...` | pass |
| `go test -tags release ./...` (full Go suite) | pass, exit 0 |
| Frontend vitest (`pnpm exec vitest run`) | pass — 9 files, 76/76 tests |
| `task test` full gate | assembled from the suites above (release-tag Go + debug-tag suites + frontend) |

Frontend note: `pnpm -C web` is not a valid entrypoint — the pnpm workspace root is the repo
root, so the suite runs via `pnpm exec vitest run`.

## Notes

- No LDAP artifacts exist (excluded feature stays excluded).
- `compose.yaml` upstream parity instance holds real schema state; left untouched.
