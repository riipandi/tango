# Endpoint Reference (Pocket ID upstream)

Source: <https://pocket-id.org/swagger.yaml> — grouped by spec tag. Use as the Yaak
request checklist: one request per row, named `<METHOD> <path>`. Status per the Gap
Register in `README.md`: **done** (implemented + live-tested), **partial** (implemented
with a noted deviation), **planned** (unimplemented — owning phase named).

Tango-only extensions (not in the upstream spec) and all structural deviations (envelope,
pagination, snake_case) are documented in `llms/tango-deviations.md` — read it before porting
upstream handlers. Remaining gaps are planned in `llms/phase-09-parity-gap.md`.

## API Keys

| Method | Endpoint                   | Summary / Yaak Title | Status |
| ------ | -------------------------- | -------------------- | ------ |
| GET    | `/api/api-keys`            | List API keys        | done   |
| POST   | `/api/api-keys`            | Create API key       | done   |
| DELETE | `/api/api-keys/{id}`       | Revoke API key       | done   |
| POST   | `/api/api-keys/{id}/renew` | Renew API key        | done   |

## APIs

| Method | Endpoint                                     | Summary / Yaak Title                                    | Status |
| ------ | -------------------------------------------- | ------------------------------------------------------- | ------ |
| GET    | `/api/api-access/{clientId}/apis`            | List APIs a client may access                           | done   |
| GET    | `/api/api-access/{clientId}/assignable-apis` | List APIs a client can still be granted access to       | done   |
| GET    | `/api/apis`                                  | List APIs                                               | done   |
| POST   | `/api/apis`                                  | Create API                                              | done   |
| DELETE | `/api/apis/{id}`                             | Delete API                                              | done   |
| GET    | `/api/apis/{id}`                             | Get API by ID                                           | done   |
| PUT    | `/api/apis/{id}`                             | Update API                                              | done   |
| GET    | `/api/apis/{id}/assignable-clients`          | List clients that can still be granted access to an API | done   |
| PUT    | `/api/apis/{id}/cimd-access`                 | Update metadata document client access                  | done   |
| GET    | `/api/apis/{id}/clients`                     | List clients with access to an API                      | done   |
| DELETE | `/api/apis/{id}/clients/{clientId}`          | Revoke a client's access to an API                      | done   |
| PUT    | `/api/apis/{id}/clients/{clientId}`          | Update a client's access to an API                      | done   |
| PUT    | `/api/apis/{id}/permissions`                 | Update API permissions                                  | done   |

## Application Configuration

| Method | Endpoint                                    | Summary / Yaak Title                   | Status            |
| ------ | ------------------------------------------- | -------------------------------------- | ----------------- |
| GET    | `/api/application-configuration`            | List public application configurations | done                |
| PUT    | `/api/application-configuration`            | Update application configurations      | done — partial PUT  |
| GET    | `/api/application-configuration/all`        | List all application configurations    | done                |
| POST   | `/api/application-configuration/sync-ldap`  | Synchronize LDAP                       | done — phase 8    |
| POST   | `/api/application-configuration/test-email` | Send test email                        | done — phase 7 mail queue |

## Application Images

| Method | Endpoint                                          | Summary / Yaak Title                 | Status            |
| ------ | ------------------------------------------------- | ------------------------------------ | ----------------- |
| DELETE | `/api/application-images/background`              | Delete background image              | done    |
| GET    | `/api/application-images/background`              | Get background image                 | done    |
| PUT    | `/api/application-images/background`              | Update background image              | done    |
| DELETE | `/api/application-images/default-profile-picture` | Delete default profile picture image | done    |
| GET    | `/api/application-images/default-profile-picture` | Get default profile picture image    | done    |
| PUT    | `/api/application-images/default-profile-picture` | Update default profile picture image | done    |
| GET    | `/api/application-images/email`                   | Get email logo image                 | done    |
| PUT    | `/api/application-images/email`                   | Update email logo                    | done    |
| GET    | `/api/application-images/favicon`                 | Get favicon                          | done    |
| PUT    | `/api/application-images/favicon`                 | Update favicon                       | done    |
| DELETE | `/api/application-images/logo`                    | Delete logo image                    | done    |
| GET    | `/api/application-images/logo`                    | Get logo image                       | done    |
| PUT    | `/api/application-images/logo`                    | Update logo                          | done    |

## Audit Logs

| Method | Endpoint                               | Summary / Yaak Title | Status                             |
| ------ | -------------------------------------- | -------------------- | ---------------------------------- |
| GET    | `/api/audit-logs`                      | List audit logs      | done — self listing (session auth) |
| GET    | `/api/audit-logs/all`                  | List all audit logs  | done — admin listing               |
| GET    | `/api/audit-logs/filters/client-names` | List client names    | done — admin only                  |
| GET    | `/api/audit-logs/filters/users`        | List users with IDs  | done — admin only                  |

## Custom Claims

| Method | Endpoint                                      | Summary / Yaak Title                  | Status |
| ------ | --------------------------------------------- | ------------------------------------- | ------ |
| GET    | `/api/custom-claims/suggestions`              | Get custom claim suggestions          | done   |
| PUT    | `/api/custom-claims/user-group/{userGroupId}` | Update custom claims for a user group | done   |
| PUT    | `/api/custom-claims/user/{userId}`            | Update custom claims for a user       | done   |

## Device Login

| Method | Endpoint                                   | Summary / Yaak Title          | Status |
| ------ | ------------------------------------------ | ----------------------------- | ------ |
| POST   | `/api/device-login/requests`               | Create device login request   | done   |
| POST   | `/api/device-login/requests/{id}/exchange` | Exchange device login request | done   |
| POST   | `/api/device-login/verification`           | Inspect device login request  | done   |
| POST   | `/api/device-login/verification/decision`  | Decide device login request   | done   |

## Health

| Method | Endpoint   | Summary / Yaak Title     | Status |
| ------ | ---------- | ------------------------ | ------ |
| GET    | `/healthz` | Responds to healthchecks | done   |

## OIDC

| Method | Endpoint                                           | Summary / Yaak Title                          | Status                                      |
| ------ | -------------------------------------------------- | --------------------------------------------- | ------------------------------------------- |
| GET    | `/api/oidc/clients`                                | List OIDC clients                             | done                                        |
| POST   | `/api/oidc/clients`                                | Create OIDC client                            | done                                        |
| DELETE | `/api/oidc/clients/{id}`                           | Delete OIDC client                            | done                                        |
| GET    | `/api/oidc/clients/{id}`                           | Get OIDC client                               | done                                        |
| PUT    | `/api/oidc/clients/{id}`                           | Update OIDC client                            | done                                        |
| PUT    | `/api/oidc/clients/{id}/allowed-user-groups`       | Update allowed user groups                    | done                                        |
| DELETE | `/api/oidc/clients/{id}/logo`                      | Delete client logo                            | done                                       |
| GET    | `/api/oidc/clients/{id}/logo`                      | Get client logo                               | done                                       |
| POST   | `/api/oidc/clients/{id}/logo`                      | Update client logo                            | done                                       |
| GET    | `/api/oidc/clients/{id}/meta`                      | Get client metadata                           | done                                       |
| GET    | `/api/oidc/clients/{id}/preview/{userId}`          | Preview OIDC client data for user             | done — claim maps, no real JWTs            |
| POST   | `/api/oidc/clients/{id}/refresh`                   | Refresh client metadata document              | done — phase 9D, CIMD-lite                  |
| GET    | `/api/oidc/clients/{id}/scim-service-provider`     | Get SCIM service provider                     | done — phase 8                              |
| GET    | `/api/oidc/clients/{id}/secrets`                   | List client secrets                           | done — multi-secret, values shown once      |
| POST   | `/api/oidc/clients/{id}/secrets`                   | Create client secret                          | done — multi-secret, values shown once      |
| DELETE | `/api/oidc/clients/{id}/secrets/{secretId}`        | Delete client secret                          | done — multi-secret, values shown once      |
| POST   | `/api/oidc/introspect`                             | Introspect OIDC tokens                        | done — client-scoped RFC 7662               |
| GET    | `/api/oidc/userinfo`                               | Get user information                          | done                                        |
| GET    | `/api/oidc/users/me/authorized-clients`            | List authorized clients for current user      | done — revocation cascades to active tokens |
| DELETE | `/api/oidc/users/me/authorized-clients/{clientId}` | Revoke authorization for an OIDC client       | done — revocation cascades to active tokens |
| GET    | `/api/oidc/users/me/clients`                       | List accessible OIDC clients for current user | done                                        |
| GET    | `/api/oidc/users/{id}/authorized-clients`          | List authorized clients for a user            | done — revocation cascades to active tokens |
| PUT    | `/api/user-groups/{id}/allowed-oidc-clients`       | Update allowed OIDC clients                   | done — snake_case oidc_client_ids          |

## SCIM

| Method | Endpoint                               | Summary / Yaak Title         | Status            |
| ------ | -------------------------------------- | ---------------------------- | ----------------- |
| POST   | `/api/scim/service-provider`           | Create SCIM service provider | done — phase 8    |
| DELETE | `/api/scim/service-provider/{id}`      | Delete SCIM service provider | done — phase 8    |
| PUT    | `/api/scim/service-provider/{id}`      | Update SCIM service provider | done — phase 8    |
| POST   | `/api/scim/service-provider/{id}/sync` | Sync SCIM service provider   | done — phase 8    |

## Storage

| Method | Endpoint                      | Summary / Yaak Title                                   | Status                                                     |
| ------ | ----------------------------- | ------------------------------------------------------ | ---------------------------------------------------------- |
| GET    | `/api/storage/sqlite-warning` | Get whether the SQLite storage warning should be shown | won't port — Postgres-only (upstream-specific)             |

## User Groups

| Method | Endpoint                      | Summary / Yaak Title    | Status |
| ------ | ----------------------------- | ----------------------- | ------ |
| GET    | `/api/user-groups`            | List user groups        | done   |
| POST   | `/api/user-groups`            | Create user group       | done   |
| DELETE | `/api/user-groups/{id}`       | Delete user group       | done   |
| GET    | `/api/user-groups/{id}`       | Get user group by ID    | done   |
| PUT    | `/api/user-groups/{id}`       | Update user group       | done   |
| PUT    | `/api/user-groups/{id}/users` | Update users in a group | done   |

## Users

| Method | Endpoint                                              | Summary / Yaak Title                          | Status                                                 |
| ------ | ----------------------------------------------------- | --------------------------------------------- | ------------------------------------------------------ |
| POST   | `/api/one-time-access-email`                          | Request one-time access email                 | done                                                   |
| POST   | `/api/one-time-access-token/{token}`                  | Exchange one-time access token                | done                                                   |
| POST   | `/api/signup`                                         | Sign up                                       | done                                                   |
| GET    | `/api/signup-tokens`                                  | List signup tokens                            | done                                                   |
| POST   | `/api/signup-tokens`                                  | Create signup token                           | done                                                   |
| DELETE | `/api/signup-tokens/{id}`                             | Delete signup token                           | done                                                   |
| POST   | `/api/signup/setup`                                   | Sign up initial admin user                    | done                                                   |
| GET    | `/api/users`                                          | List users                                    | done                                                   |
| POST   | `/api/users`                                          | Create user                                   | done                                                   |
| GET    | `/api/users/me`                                       | Get current user                              | done                                                   |
| PUT    | `/api/users/me`                                       | Update current user                           | partial — profile fields only; email stays admin-gated |
| DELETE | `/api/users/me/profile-picture`                       | Reset current user's profile picture          | done                                       |
| PUT    | `/api/users/me/profile-picture`                       | Update current user's profile picture         | done                                       |
| POST   | `/api/users/me/send-email-verification`               | Send email verification                       | done                                                   |
| POST   | `/api/users/me/verify-email`                          | Verify email                                  | done                                                   |
| DELETE | `/api/users/{id}`                                     | Delete user                                   | done                                                   |
| GET    | `/api/users/{id}`                                     | Get user by ID                                | done                                                   |
| PUT    | `/api/users/{id}`                                     | Update user                                   | done                                                   |
| GET    | `/api/users/{id}/groups`                              | Get user groups                               | done                                                   |
| POST   | `/api/users/{id}/one-time-access-email`               | Request one-time access email (admin)         | done                                                   |
| POST   | `/api/users/{id}/one-time-access-token`               | Create one-time access token for user (admin) | done                                                   |
| DELETE | `/api/users/{id}/profile-picture`                     | Reset user profile picture                    | done                                       |
| PUT    | `/api/users/{id}/profile-picture`                     | Update user profile picture                   | done                                       |
| GET    | `/api/users/{id}/profile-picture.png`                 | Get user profile picture                      | done — bare bytes, default fallback        |
| PUT    | `/api/users/{id}/user-groups`                         | Update user groups                            | done                                                   |
| GET    | `/api/users/{id}/webauthn-credentials`                | List user passkeys                            | done                                                   |
| PUT    | `/api/users/{id}/webauthn-credentials/{credentialId}` | Rename user passkey                           | done                                                   |
| DELETE | `/api/users/{id}/webauthn-credentials/{credentialId}` | Delete user passkey                           | done                                                   |

## WebAuthn

| Method | Endpoint                        | Summary / Yaak Title              | Status |
| ------ | ------------------------------- | --------------------------------- | ------ |
| POST   | `/api/webauthn/register/begin`  | Begin passkey registration        | done   |
| POST   | `/api/webauthn/register/finish` | Finish passkey registration       | done   |
| POST   | `/api/webauthn/login/begin`     | Begin discoverable passkey login  | done   |
| POST   | `/api/webauthn/login/finish`    | Finish discoverable passkey login | done   |

## Version

| Method | Endpoint               | Summary / Yaak Title                      | Status                                                  |
| ------ | ---------------------- | ----------------------------------------- | ------------------------------------------------------- |
| GET    | `/api/version/current` | Get current deployed version of Pocket ID | done                                                    |
| GET    | `/api/version/latest`  | Get latest available version of Pocket ID | done — phase 7 release-check job (falls back to the deployed build when the feed never answered) |

## Well Known

| Method | Endpoint                                  | Summary / Yaak Title                        | Status |
| ------ | ----------------------------------------- | ------------------------------------------- | ------ |
| GET    | `/.well-known/jwks.json`                  | Get JSON Web Key Set (JWKS)                 | done   |
| GET    | `/.well-known/oauth-authorization-server` | Get OAuth 2.0 authorization server metadata | done   |
| GET    | `/.well-known/openid-configuration`       | Get OpenID Connect discovery configuration  | done   |
