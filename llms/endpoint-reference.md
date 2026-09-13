# Endpoint Reference (Pocket ID upstream)

Source: <https://pocket-id.org/swagger.yaml> — grouped by spec tag. Use as the Yaak
request checklist: one request per row, named `<METHOD> <path>`. The Gap Register in
`README.md` tracks what is implemented per phase.

## API Keys

| Method | Endpoint                   | Summary / Yaak Title |
| ------ | -------------------------- | -------------------- |
| GET    | `/api/api-keys`            | List API keys        |
| POST   | `/api/api-keys`            | Create API key       |
| DELETE | `/api/api-keys/{id}`       | Revoke API key       |
| POST   | `/api/api-keys/{id}/renew` | Renew API key        |

## APIs

| Method | Endpoint                                     | Summary / Yaak Title                                    |
| ------ | -------------------------------------------- | ------------------------------------------------------- |
| GET    | `/api/api-access/{clientId}/apis`            | List APIs a client may access                           |
| GET    | `/api/api-access/{clientId}/assignable-apis` | List APIs a client can still be granted access to       |
| GET    | `/api/apis`                                  | List APIs                                               |
| POST   | `/api/apis`                                  | Create API                                              |
| DELETE | `/api/apis/{id}`                             | Delete API                                              |
| GET    | `/api/apis/{id}`                             | Get API by ID                                           |
| PUT    | `/api/apis/{id}`                             | Update API                                              |
| GET    | `/api/apis/{id}/assignable-clients`          | List clients that can still be granted access to an API |
| PUT    | `/api/apis/{id}/cimd-access`                 | Update metadata document client access                  |
| GET    | `/api/apis/{id}/clients`                     | List clients with access to an API                      |
| DELETE | `/api/apis/{id}/clients/{clientId}`          | Revoke a client's access to an API                      |
| PUT    | `/api/apis/{id}/clients/{clientId}`          | Update a client's access to an API                      |
| PUT    | `/api/apis/{id}/permissions`                 | Update API permissions                                  |

## Application Configuration

| Method | Endpoint                                    | Summary / Yaak Title                   |
| ------ | ------------------------------------------- | -------------------------------------- |
| GET    | `/api/application-configuration`            | List public application configurations |
| PUT    | `/api/application-configuration`            | Update application configurations      |
| GET    | `/api/application-configuration/all`        | List all application configurations    |
| POST   | `/api/application-configuration/sync-ldap`  | Synchronize LDAP                       |
| POST   | `/api/application-configuration/test-email` | Send test email                        |

## Application Images

| Method | Endpoint                                          | Summary / Yaak Title                 |
| ------ | ------------------------------------------------- | ------------------------------------ |
| DELETE | `/api/application-images/background`              | Delete background image              |
| GET    | `/api/application-images/background`              | Get background image                 |
| PUT    | `/api/application-images/background`              | Update background image              |
| DELETE | `/api/application-images/default-profile-picture` | Delete default profile picture image |
| GET    | `/api/application-images/default-profile-picture` | Get default profile picture image    |
| PUT    | `/api/application-images/default-profile-picture` | Update default profile picture image |
| GET    | `/api/application-images/email`                   | Get email logo image                 |
| PUT    | `/api/application-images/email`                   | Update email logo                    |
| GET    | `/api/application-images/favicon`                 | Get favicon                          |
| PUT    | `/api/application-images/favicon`                 | Update favicon                       |
| DELETE | `/api/application-images/logo`                    | Delete logo image                    |
| GET    | `/api/application-images/logo`                    | Get logo image                       |
| PUT    | `/api/application-images/logo`                    | Update logo                          |

## Audit Logs

| Method | Endpoint                               | Summary / Yaak Title |
| ------ | -------------------------------------- | -------------------- |
| GET    | `/api/audit-logs`                      | List audit logs      |
| GET    | `/api/audit-logs/all`                  | List all audit logs  |
| GET    | `/api/audit-logs/filters/client-names` | List client names    |
| GET    | `/api/audit-logs/filters/users`        | List users with IDs  |

## Custom Claims

| Method | Endpoint                                      | Summary / Yaak Title                  |
| ------ | --------------------------------------------- | ------------------------------------- |
| GET    | `/api/custom-claims/suggestions`              | Get custom claim suggestions          |
| PUT    | `/api/custom-claims/user-group/{userGroupId}` | Update custom claims for a user group |
| PUT    | `/api/custom-claims/user/{userId}`            | Update custom claims for a user       |

## Device Login

| Method | Endpoint                                   | Summary / Yaak Title          |
| ------ | ------------------------------------------ | ----------------------------- |
| POST   | `/api/device-login/requests`               | Create device login request   |
| POST   | `/api/device-login/requests/{id}/exchange` | Exchange device login request |
| POST   | `/api/device-login/verification`           | Inspect device login request  |
| POST   | `/api/device-login/verification/decision`  | Decide device login request   |

## Health

| Method | Endpoint   | Summary / Yaak Title     |
| ------ | ---------- | ------------------------ |
| GET    | `/healthz` | Responds to healthchecks |

## OIDC

| Method | Endpoint                                           | Summary / Yaak Title                          |
| ------ | -------------------------------------------------- | --------------------------------------------- |
| GET    | `/api/oidc/clients`                                | List OIDC clients                             |
| POST   | `/api/oidc/clients`                                | Create OIDC client                            |
| DELETE | `/api/oidc/clients/{id}`                           | Delete OIDC client                            |
| GET    | `/api/oidc/clients/{id}`                           | Get OIDC client                               |
| PUT    | `/api/oidc/clients/{id}`                           | Update OIDC client                            |
| PUT    | `/api/oidc/clients/{id}/allowed-user-groups`       | Update allowed user groups                    |
| DELETE | `/api/oidc/clients/{id}/logo`                      | Delete client logo                            |
| GET    | `/api/oidc/clients/{id}/logo`                      | Get client logo                               |
| POST   | `/api/oidc/clients/{id}/logo`                      | Update client logo                            |
| GET    | `/api/oidc/clients/{id}/meta`                      | Get client metadata                           |
| GET    | `/api/oidc/clients/{id}/preview/{userId}`          | Preview OIDC client data for user             |
| POST   | `/api/oidc/clients/{id}/refresh`                   | Refresh client metadata document              |
| GET    | `/api/oidc/clients/{id}/scim-service-provider`     | Get SCIM service provider                     |
| GET    | `/api/oidc/clients/{id}/secrets`                   | List client secrets                           |
| POST   | `/api/oidc/clients/{id}/secrets`                   | Create client secret                          |
| DELETE | `/api/oidc/clients/{id}/secrets/{secretId}`        | Delete client secret                          |
| POST   | `/api/oidc/introspect`                             | Introspect OIDC tokens                        |
| GET    | `/api/oidc/userinfo`                               | Get user information                          |
| GET    | `/api/oidc/users/me/authorized-clients`            | List authorized clients for current user      |
| DELETE | `/api/oidc/users/me/authorized-clients/{clientId}` | Revoke authorization for an OIDC client       |
| GET    | `/api/oidc/users/me/clients`                       | List accessible OIDC clients for current user |
| GET    | `/api/oidc/users/{id}/authorized-clients`          | List authorized clients for a user            |
| PUT    | `/api/user-groups/{id}/allowed-oidc-clients`       | Update allowed OIDC clients                   |

## SCIM

| Method | Endpoint                               | Summary / Yaak Title         |
| ------ | -------------------------------------- | ---------------------------- |
| POST   | `/api/scim/service-provider`           | Create SCIM service provider |
| DELETE | `/api/scim/service-provider/{id}`      | Delete SCIM service provider |
| PUT    | `/api/scim/service-provider/{id}`      | Update SCIM service provider |
| POST   | `/api/scim/service-provider/{id}/sync` | Sync SCIM service provider   |

## Storage

| Method | Endpoint                      | Summary / Yaak Title                                   |
| ------ | ----------------------------- | ------------------------------------------------------ |
| GET    | `/api/storage/sqlite-warning` | Get whether the SQLite storage warning should be shown |

## User Groups

| Method | Endpoint                      | Summary / Yaak Title    |
| ------ | ----------------------------- | ----------------------- |
| GET    | `/api/user-groups`            | List user groups        |
| POST   | `/api/user-groups`            | Create user group       |
| DELETE | `/api/user-groups/{id}`       | Delete user group       |
| GET    | `/api/user-groups/{id}`       | Get user group by ID    |
| PUT    | `/api/user-groups/{id}`       | Update user group       |
| PUT    | `/api/user-groups/{id}/users` | Update users in a group |

## Users

| Method | Endpoint                                              | Summary / Yaak Title                          |
| ------ | ----------------------------------------------------- | --------------------------------------------- |
| POST   | `/api/one-time-access-email`                          | Request one-time access email                 |
| POST   | `/api/one-time-access-token/{token}`                  | Exchange one-time access token                |
| POST   | `/api/signup`                                         | Sign up                                       |
| GET    | `/api/signup-tokens`                                  | List signup tokens                            |
| POST   | `/api/signup-tokens`                                  | Create signup token                           |
| DELETE | `/api/signup-tokens/{id}`                             | Delete signup token                           |
| POST   | `/api/signup/setup`                                   | Sign up initial admin user                    |
| GET    | `/api/users`                                          | List users                                    |
| POST   | `/api/users`                                          | Create user                                   |
| GET    | `/api/users/me`                                       | Get current user                              |
| PUT    | `/api/users/me`                                       | Update current user                           |
| DELETE | `/api/users/me/profile-picture`                       | Reset current user's profile picture          |
| PUT    | `/api/users/me/profile-picture`                       | Update current user's profile picture         |
| POST   | `/api/users/me/send-email-verification`               | Send email verification                       |
| POST   | `/api/users/me/verify-email`                          | Verify email                                  |
| DELETE | `/api/users/{id}`                                     | Delete user                                   |
| GET    | `/api/users/{id}`                                     | Get user by ID                                |
| PUT    | `/api/users/{id}`                                     | Update user                                   |
| GET    | `/api/users/{id}/groups`                              | Get user groups                               |
| POST   | `/api/users/{id}/one-time-access-email`               | Request one-time access email (admin)         |
| POST   | `/api/users/{id}/one-time-access-token`               | Create one-time access token for user (admin) |
| DELETE | `/api/users/{id}/profile-picture`                     | Reset user profile picture                    |
| PUT    | `/api/users/{id}/profile-picture`                     | Update user profile picture                   |
| GET    | `/api/users/{id}/profile-picture.png`                 | Get user profile picture                      |
| PUT    | `/api/users/{id}/user-groups`                         | Update user groups                            |
| GET    | `/api/users/{id}/webauthn-credentials`                | List user passkeys                            |
| DELETE | `/api/users/{id}/webauthn-credentials/{credentialId}` | Delete user passkey                           |

## Version

| Method | Endpoint               | Summary / Yaak Title                      |
| ------ | ---------------------- | ----------------------------------------- |
| GET    | `/api/version/current` | Get current deployed version of Pocket ID |
| GET    | `/api/version/latest`  | Get latest available version of Pocket ID |

## Well Known

| Method | Endpoint                                  | Summary / Yaak Title                        |
| ------ | ----------------------------------------- | ------------------------------------------- |
| GET    | `/.well-known/jwks.json`                  | Get JSON Web Key Set (JWKS)                 |
| GET    | `/.well-known/oauth-authorization-server` | Get OAuth 2.0 authorization server metadata |
| GET    | `/.well-known/openid-configuration`       | Get OpenID Connect discovery configuration  |
