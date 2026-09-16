# Endpoint Reference (Pocket ID upstream)

Source: <https://pocket-id.org/swagger.yaml> — grouped by spec tag. Use as the Yaak
request checklist: one request per row, named `<METHOD> <path>`. Status vocabulary:
**done** (implemented with test evidence in the Evidence column), **partial**
(implemented with a noted deviation), **planned** (unimplemented — owning phase
named), **excluded** (out of scope per `llms/tango-deviations.md` — never parity work).

Tango-only extensions (not in the upstream spec) and all structural deviations (envelope,
pagination, snake_case) are documented in `llms/tango-deviations.md` — read it before porting
upstream handlers.

## API Keys

| Method | Endpoint                   | Summary / Yaak Title | Status | Evidence |
| ------ | -------------------------- | -------------------- | ------ | -------- |
| GET    | `/api/api-keys`            | List API keys        | done   | `modules/admin/apikey.TestKeyPagination` |
| POST   | `/api/api-keys`            | Create API key       | done   | `modules/admin/apikey.TestKeyLifecycle` |
| DELETE | `/api/api-keys/{id}`       | Revoke API key       | done   | `modules/admin/apikey.TestKeyLifecycle` |
| POST   | `/api/api-keys/{id}/renew` | Renew API key        | done   | `modules/admin/apikey.TestKeyLifecycle` |

## APIs

| Method | Endpoint                                     | Summary / Yaak Title                                    | Status | Evidence |
| ------ | -------------------------------------------- | ------------------------------------------------------- | ------ | -------- |
| GET    | `/api/api-access/{clientId}/apis`            | List APIs a client may access                           | done   | `modules/admin/apiaccess.TestGrantLifecycle` |
| GET    | `/api/api-access/{clientId}/assignable-apis` | List APIs a client can still be granted access to       | done   | `modules/admin/apiaccess.TestGrantLifecycle` |
| GET    | `/api/apis`                                  | List APIs                                               | done   | `modules/admin/apiaccess.TestListPagination` |
| POST   | `/api/apis`                                  | Create API                                              | done   | `modules/admin/apiaccess.TestAPICRUD` |
| DELETE | `/api/apis/{id}`                             | Delete API                                              | done   | `modules/admin/apiaccess.TestAPICRUD` |
| GET    | `/api/apis/{id}`                             | Get API by ID                                           | done   | `modules/admin/apiaccess.TestAPICRUD` |
| PUT    | `/api/apis/{id}`                             | Update API                                              | done   | `modules/admin/apiaccess.TestAPICRUD` |
| GET    | `/api/apis/{id}/assignable-clients`          | List clients that can still be granted access to an API | done   | `modules/admin/apiaccess.TestGrantLifecycle` |
| PUT    | `/api/apis/{id}/cimd-access`                 | Update metadata document client access                  | done   | `modules/admin/apiaccess.TestSetCIMDAccess` |
| GET    | `/api/apis/{id}/clients`                     | List clients with access to an API                      | done   | `modules/admin/apiaccess.TestGrantLifecycle` |
| DELETE | `/api/apis/{id}/clients/{clientId}`          | Revoke a client's access to an API                      | done   | `modules/admin/apiaccess.TestGrantLifecycle` |
| PUT    | `/api/apis/{id}/clients/{clientId}`          | Update a client's access to an API                      | done   | `modules/admin/apiaccess.TestGrantLifecycle` |
| PUT    | `/api/apis/{id}/permissions`                 | Update API permissions                                  | done   | `modules/admin/apiaccess.TestSetPermissionsListReplace` |

## Application Configuration

| Method | Endpoint                                    | Summary / Yaak Title                   | Status             | Evidence |
| ------ | ------------------------------------------- | -------------------------------------- | ------------------ | -------- |
| GET    | `/api/application-configuration`            | List public application configurations | done               | `modules/admin/appconfig.TestConfigCRUD` |
| PUT    | `/api/application-configuration`            | Update application configurations      | done — partial PUT | `modules/admin/appconfig.TestConfigCRUD` |
| GET    | `/api/application-configuration/all`        | List all application configurations    | done               | `modules/admin/appconfig.TestEnvDefaultsAndSensitiveRedaction` |
| POST   | `/api/application-configuration/sync-ldap`  | Synchronize LDAP                       | excluded           | — |
| POST   | `/api/application-configuration/test-email` | Send test email                        | done               | `modules/admin/appconfig.TestTestEmailQueuesToTheSignedInAdmin` |

## Application Images

| Method | Endpoint                                          | Summary / Yaak Title                 | Status   | Evidence |
| ------ | ------------------------------------------------- | ------------------------------------ | -------- | -------- |
| DELETE | `/api/application-images/background`              | Delete background image              | excluded | — |
| GET    | `/api/application-images/background`              | Get background image                 | excluded | — |
| PUT    | `/api/application-images/background`              | Update background image              | excluded | — |
| DELETE | `/api/application-images/default-profile-picture` | Delete default profile picture image | excluded | — |
| GET    | `/api/application-images/default-profile-picture` | Get default profile picture image    | excluded | — |
| PUT    | `/api/application-images/default-profile-picture` | Update default profile picture image | excluded | — |
| GET    | `/api/application-images/email`                   | Get email logo image                 | excluded | — |
| PUT    | `/api/application-images/email`                   | Update email logo                    | excluded | — |
| GET    | `/api/application-images/favicon`                 | Get favicon                          | excluded | — |
| PUT    | `/api/application-images/favicon`                 | Update favicon                       | excluded | — |
| DELETE | `/api/application-images/logo`                    | Delete logo image                    | excluded | — |
| GET    | `/api/application-images/logo`                    | Get logo image                       | excluded | — |
| PUT    | `/api/application-images/logo`                    | Update logo                          | excluded | — |

## Audit Logs

| Method | Endpoint                               | Summary / Yaak Title | Status                             | Evidence |
| ------ | -------------------------------------- | -------------------- | ---------------------------------- | -------- |
| GET    | `/api/audit-logs`                      | List audit logs      | done — self listing (session auth) | `modules/admin/auditlog.TestSelfListingScopesToCurrentUser` |
| GET    | `/api/audit-logs/all`                  | List all audit logs  | done — admin listing               | `modules/admin/auditlog.TestListEndpointEnvelopeAndPagination` |
| GET    | `/api/audit-logs/filters/client-names` | List client names    | done — admin only                  | `modules/admin/auditlog.TestListFiltersByUserAndEvent` |
| GET    | `/api/audit-logs/filters/users`        | List users with IDs  | done — admin only                  | `modules/admin/auditlog.TestListFiltersByUserAndEvent` |

## Custom Claims

| Method | Endpoint                                      | Summary / Yaak Title                  | Status | Evidence |
| ------ | --------------------------------------------- | ------------------------------------- | ------ | -------- |
| GET    | `/api/custom-claims/suggestions`              | Get custom claim suggestions          | done   | `modules/admin/customclaim.TestClaimEndpointsAdminGated` |
| PUT    | `/api/custom-claims/user-group/{userGroupId}` | Update custom claims for a user group | done   | `modules/admin/customclaim.TestClaimEndpointsAdminGated` |
| PUT    | `/api/custom-claims/user/{userId}`            | Update custom claims for a user       | done   | `modules/admin/customclaim.TestClaimEndpointsAdminGated` |

## Device Login

| Method | Endpoint                                   | Summary / Yaak Title          | Status | Evidence |
| ------ | ------------------------------------------ | ----------------------------- | ------ | -------- |
| POST   | `/api/device-login/requests`               | Create device login request   | done   | `modules/identity/devicelogin.TestCreateApproveExchange` |
| POST   | `/api/device-login/requests/{id}/exchange` | Exchange device login request | done   | `modules/identity/devicelogin.TestCreateApproveExchange` |
| POST   | `/api/device-login/verification`           | Inspect device login request  | done   | `modules/identity/devicelogin.TestCreateApproveExchange` |
| POST   | `/api/device-login/verification/decision`  | Decide device login request   | done   | `modules/identity/devicelogin.TestCreateApproveExchange` |

## Health

| Method | Endpoint   | Summary / Yaak Title     | Status | Evidence |
| ------ | ---------- | ------------------------ | ------ | -------- |
| GET    | `/healthz` | Responds to healthchecks | done   | `internal/transport.TestNewHTTPServerRoutes` |
## OIDC

| Method | Endpoint                                           | Summary / Yaak Title                          | Status                                      | Evidence |
| ------ | -------------------------------------------------- | --------------------------------------------- | ------------------------------------------- | -------- |
| GET    | `/api/oidc/clients`                                | List OIDC clients                             | done                                        | `modules/federation/oidc.TestClientCRUDLifecycle` |
| POST   | `/api/oidc/clients`                                | Create OIDC client                            | done                                        | `modules/federation/oidc.TestClientCRUDLifecycle` |
| DELETE | `/api/oidc/clients/{id}`                           | Delete OIDC client                            | done                                        | `modules/federation/oidc.TestClientCRUDLifecycle` |
| GET    | `/api/oidc/clients/{id}`                           | Get OIDC client                               | done                                        | `modules/federation/oidc.TestClientCRUDLifecycle` |
| PUT    | `/api/oidc/clients/{id}`                           | Update OIDC client                            | done                                        | `modules/federation/oidc.TestClientCRUDLifecycle` |
| PUT    | `/api/oidc/clients/{id}/allowed-user-groups`       | Update allowed user groups                    | done                                        | `modules/federation/oidc.TestUpdateAllowedGroups` |
| DELETE | `/api/oidc/clients/{id}/logo`                      | Delete client logo                            | done                                        | `modules/federation/oidc.TestClientLogoLifecycle` |
| GET    | `/api/oidc/clients/{id}/logo`                      | Get client logo                               | done                                        | `modules/federation/oidc.TestClientLogoLifecycle` |
| POST   | `/api/oidc/clients/{id}/logo`                      | Update client logo                            | done                                        | `modules/federation/oidc.TestClientLogoLifecycle` |
| GET    | `/api/oidc/clients/{id}/meta`                      | Get client metadata                           | done                                        | `modules/federation/oidc.TestClientMetaAndPreview` |
| GET    | `/api/oidc/clients/{id}/preview/{userId}`          | Preview OIDC client data for user             | done — claim maps, no real JWTs             | `modules/federation/oidc.TestClientMetaAndPreview` |
| POST   | `/api/oidc/clients/{id}/refresh`                   | Refresh client metadata document              | done — CIMD-lite                            | `modules/federation/oidc.TestCIMDClientLifecycle` |
| GET    | `/api/oidc/clients/{id}/scim-service-provider`     | Get SCIM service provider                     | done                                        | `modules/federation/scimsync.TestHandlerProviderLifecycle` |
| GET    | `/api/oidc/clients/{id}/secrets`                   | List client secrets                           | done — multi-secret, values shown once      | `modules/federation/oidc.TestClientSecretsLifecycle` |
| POST   | `/api/oidc/clients/{id}/secrets`                   | Create client secret                          | done — multi-secret, values shown once      | `modules/federation/oidc.TestClientSecretsLifecycle` |
| DELETE | `/api/oidc/clients/{id}/secrets/{secretId}`        | Delete client secret                          | done — multi-secret, values shown once      | `modules/federation/oidc.TestClientSecretsLifecycle` |
| POST   | `/api/oidc/introspect`                             | Introspect OIDC tokens                        | done — client-scoped RFC 7662               | `modules/federation/oidc.TestIntrospection` |
| GET    | `/api/oidc/userinfo`                               | Get user information                          | done                                        | `modules/federation/oidc.TestEndToEndAuthorizeTokenUserinfo` |
| GET    | `/api/oidc/users/me/authorized-clients`            | List authorized clients for current user      | done — revocation cascades to active tokens | `modules/federation/oidc.TestUsersMeClientSurfaces` |
| DELETE | `/api/oidc/users/me/authorized-clients/{clientId}` | Revoke authorization for an OIDC client       | done — revocation cascades to active tokens | `modules/federation/oidc.TestUsersMeClientSurfaces` |
| GET    | `/api/oidc/users/me/clients`                       | List accessible OIDC clients for current user | done                                        | `modules/federation/oidc.TestUsersMeClientSurfaces` |
| GET    | `/api/oidc/users/{id}/authorized-clients`          | List authorized clients for a user            | done — revocation cascades to active tokens | `modules/federation/oidc.TestUsersMeClientSurfaces` |
| PUT    | `/api/user-groups/{id}/allowed-oidc-clients`       | Update allowed OIDC clients                   | done — snake_case oidc_client_ids           | `modules/identity/usergroup.TestSetAllowedClients` |

The protocol endpoints (root router, bare OAuth documents) are verified by the same suite:
`/authorize` → `modules/federation/oidc.TestEndToEndAuthorizeTokenUserinfo` and friends;
`/api/oidc/end-session` (RP-initiated logout) → `modules/federation/oidc.TestEndSessionRevokesFamilyAndRedirects`:
the `id_token_hint` must verify (issuer, audience, subject, jti), the `client_id` must match the
hint's audience, and the user must have granted the client — every failure redirects to the
instance logout page without explaining why. Ending the session deactivates the grant's whole
token family (the ID token carries the access token's `jti`), replays are idempotent, and an
unregistered `post_logout_redirect_uri` is never followed;
`/.well-known/*` and JWKS → `modules/federation/discovery` and `modules/federation/jwks`.

## SCIM

| Method | Endpoint                               | Summary / Yaak Title         | Status | Evidence |
| ------ | -------------------------------------- | ---------------------------- | ------ | -------- |
| POST   | `/api/scim/service-provider`           | Create SCIM service provider | done   | `modules/federation/scimsync.TestHandlerProviderLifecycle` |
| DELETE | `/api/scim/service-provider/{id}`      | Delete SCIM service provider | done   | `modules/federation/scimsync.TestHandlerProviderLifecycle` |
| PUT    | `/api/scim/service-provider/{id}`      | Update SCIM service provider | done   | `modules/federation/scimsync.TestHandlerProviderLifecycle` |
| POST   | `/api/scim/service-provider/{id}/sync` | Sync SCIM service provider   | done   | `modules/federation/scimsync.TestHandlerProviderLifecycle` |

## User Groups

| Method | Endpoint                      | Summary / Yaak Title    | Status | Evidence |
| ------ | ----------------------------- | ----------------------- | ------ | -------- |
| GET    | `/api/user-groups`            | List user groups        | done   | `modules/identity/usergroup.TestGroupEndpointsAdminGated` |
| POST   | `/api/user-groups`            | Create user group       | done   | `modules/identity/usergroup.TestGroupEndpointsAdminGated` |
| DELETE | `/api/user-groups/{id}`       | Delete user group       | done   | `modules/identity/usergroup.TestGroupEndpointsAdminGated` |
| GET    | `/api/user-groups/{id}`       | Get user group by ID    | done   | `modules/identity/usergroup.TestGroupEndpointsAdminGated` |
| PUT    | `/api/user-groups/{id}`       | Update user group       | done   | `modules/identity/usergroup.TestGroupEndpointsAdminGated` |
| PUT    | `/api/user-groups/{id}/users` | Update users in a group | done   | `modules/identity/usergroup.TestGroupEndpointsAdminGated` |

## Users

| Method | Endpoint                                              | Summary / Yaak Title                          | Status                                                 | Evidence |
| ------ | ----------------------------------------------------- | --------------------------------------------- | ------------------------------------------------------ | -------- |
| POST   | `/api/one-time-access-email`                          | Request one-time access email                 | done — answers 204 unconditionally                     | `modules/identity/onetimeaccess.TestEmailRequestNeverEnumerates` |
| POST   | `/api/one-time-access-token/{token}`                  | Exchange one-time access token                | done — single use, sets the session cookie             | `modules/identity/onetimeaccess.TestAdminMintAndExchange` |
| POST   | `/api/signup`                                         | Sign up                                       | done — requires a valid signup token                   | `modules/identity/signup.TestSignupRequiresValidToken` |
| GET    | `/api/signup-tokens`                                  | List signup tokens                            | done                                                   | `modules/identity/signup.TestSignupTokenAdminCRUD` |
| POST   | `/api/signup-tokens`                                  | Create signup token                           | done — raw token shown once                            | `modules/identity/signup.TestSignupTokenAdminCRUD` |
| DELETE | `/api/signup-tokens/{id}`                             | Delete signup token                           | done                                                   | `modules/identity/signup.TestSignupTokenAdminCRUD` |
| GET    | `/api/signup/setup`                                   | Check initial admin setup availability        | done — 204 while no user exists, then 404              | `modules/identity/signup.TestSetupAvailableLifecycle` |
| POST   | `/api/signup/setup`                                   | Sign up initial admin user                    | done — 409 once any user exists                        | `modules/identity/signup.TestSetupAvailableLifecycle` |
| GET    | `/api/users`                                          | List users                                    | done                                                   | `modules/identity/user.TestListUsers` |
| POST   | `/api/users`                                          | Create user                                   | done                                                   | `modules/identity/user.TestCreateUser` |
| GET    | `/api/users/me`                                       | Get current user                              | done                                                   | `modules/identity/account.TestProfileRoundTrip` |
| PUT    | `/api/users/me`                                       | Update current user                           | partial — profile fields only; email stays admin-gated | `modules/identity/account.TestProfileRoundTrip` |
| DELETE | `/api/users/me/profile-picture`                       | Reset current user's profile picture          | done                                                   | `modules/identity/user.TestProfilePictureSurface` |
| PUT    | `/api/users/me/profile-picture`                       | Update current user's profile picture         | done                                                   | `modules/identity/user.TestProfilePictureSurface` |
| POST   | `/api/users/me/send-email-verification`               | Send email verification                       | done — 204, token travels by email only                | `modules/identity/emailverification.TestSendDoesNotLeakToken` |
| POST   | `/api/users/me/verify-email`                          | Verify email                                  | done — single-use token scoped to the session user     | `modules/identity/emailverification.TestVerifyConsumesScopedToken` |
| DELETE | `/api/users/{id}`                                     | Delete user                                   | done                                                   | `modules/identity/user.TestDeleteUser` |
| GET    | `/api/users/{id}`                                     | Get user by ID                                | done                                                   | `modules/identity/user.TestGetUser` |
| PUT    | `/api/users/{id}`                                     | Update user                                   | done                                                   | `modules/identity/user.TestGetUser` |
| GET    | `/api/users/{id}/groups`                              | Get user groups                               | done                                                   | `modules/identity/usergroup.TestGroupEndpointsAdminGated` |
| POST   | `/api/users/{id}/one-time-access-email`               | Request one-time access email (admin)         | done                                                   | `modules/identity/onetimeaccess.TestAdminEmailMint` |
| POST   | `/api/users/{id}/one-time-access-token`               | Create one-time access token for user (admin) | done — raw token shown once                            | `modules/identity/onetimeaccess.TestAdminMintAndExchange` |
| DELETE | `/api/users/{id}/profile-picture`                     | Reset user profile picture                    | done                                                   | `modules/identity/user.TestProfilePictureSurface` |
| PUT    | `/api/users/{id}/profile-picture`                     | Update user profile picture                   | done                                                   | `modules/identity/user.TestProfilePictureSurface` |
| GET    | `/api/users/{id}/profile-picture.png`                 | Get user profile picture                      | done — bare bytes, default fallback                    | `modules/identity/user.TestProfilePictureDefaultFallback` |
| PUT    | `/api/users/{id}/user-groups`                         | Update user groups                            | done — replaces the set atomically                     | `modules/identity/usergroup.TestReplaceUserGroupsForUser` |
| GET    | `/api/users/{id}/webauthn-credentials`                | List user passkeys                            | done — key material never leaves the store             | `modules/identity/webauthn.TestCredentialAdminCRUD` |
| PUT    | `/api/users/{id}/webauthn-credentials/{credentialId}` | Rename user passkey                           | done                                                   | `modules/identity/webauthn.TestCredentialAdminCRUD` |
| DELETE | `/api/users/{id}/webauthn-credentials/{credentialId}` | Delete user passkey                           | done                                                   | `modules/identity/webauthn.TestCredentialAdminCRUD` |

## WebAuthn

| Method | Endpoint                        | Summary / Yaak Title              | Status | Evidence |
| ------ | ------------------------------- | --------------------------------- | ------ | -------- |
| POST   | `/api/webauthn/register/begin`  | Begin passkey registration        | done — bare `publicKey` options plus an explicit ceremony id | `modules/identity/webauthn.TestRegisterBeginReturnsOptions` |
| POST   | `/api/webauthn/register/finish` | Finish passkey registration       | done   | `modules/identity/webauthn.TestRegisterBeginReturnsOptions` |
| POST   | `/api/webauthn/login/begin`     | Begin discoverable passkey login  | done — bare `publicKey` options plus an explicit ceremony id | `modules/identity/webauthn.TestLoginBeginAnonymousAndFinishValidation` |
| POST   | `/api/webauthn/login/finish`    | Finish discoverable passkey login | done — fail closed on unknown ceremony sessions | `modules/identity/webauthn.TestLoginBeginAnonymousAndFinishValidation` |

## Version

| Method | Endpoint               | Summary / Yaak Title                      | Status | Evidence |
| ------ | ---------------------- | ----------------------------------------- | ------ | -------- |
| GET    | `/api/version/current` | Get current deployed version of Pocket ID | done — session required | `internal/transport.TestVersionContracts` |
| GET    | `/api/version/latest`  | Get latest available version of Pocket ID | done — falls back to the deployed build when the feed never answered | `internal/transport.TestVersionContracts` |

## Well Known

| Method | Endpoint                                  | Summary / Yaak Title                        | Status | Evidence |
| ------ | ----------------------------------------- | ------------------------------------------- | ------ | -------- |
| GET    | `/.well-known/jwks.json`                  | Get JSON Web Key Set (JWKS)                 | done   | `modules/federation/discovery.TestOpenIDConfigurationHandler` |
| GET    | `/.well-known/oauth-authorization-server` | Get OAuth 2.0 authorization server metadata | done   | `modules/federation/discovery.TestOpenIDConfigurationHandler` |
| GET    | `/.well-known/openid-configuration`       | Get OpenID Connect discovery configuration  | done   | `modules/federation/discovery.TestOpenIDConfigurationHandler` |