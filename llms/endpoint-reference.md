# Endpoint Reference (Pocket ID upstream)

Source: <https://pocket-id.org/swagger.yaml> — grouped by spec tag. Use as the Yaak
request checklist: one request per row, named `<METHOD> <path>`. Status vocabulary:
**done** (implemented with test evidence in the Evidence column), **partial**
(implemented with a noted deviation), **planned** (unimplemented — owning phase
named), **excluded** (out of scope per `llms/tango-deviations.md` — never parity work).

Tango-only extensions (not in the upstream spec) and all structural deviations (envelope,
pagination, snake_case) are documented in `llms/tango-deviations.md` — read it before porting
upstream handlers.

## Authentication (tango-only)

Password authentication is a tango-only surface: upstream Pocket ID signs users in with passkeys
only. Contracts below define the full password lifecycle.

| Method | Endpoint                        | Summary / Yaak Title        | Status | Evidence |
| ------ | ------------------------------- | --------------------------- | ------ | -------- |
| POST   | `/api/auth/sign-in`             | Sign in with password       | done — indistinguishable failures for unknown identity vs wrong secret; disabled accounts fail closed | `modules/identity/session.TestSignInSessionSignOutRoundTrip`, `modules/identity/session.TestSignInRejectsBadCredentials` |
| POST   | `/api/auth/sign-out`            | Sign out                    | done — revokes the presented session only | `modules/identity/session.TestSignInSessionSignOutRoundTrip` |
| GET    | `/api/auth/session`             | Inspect current session     | done   | `modules/identity/session.TestSignInSessionSignOutRoundTrip` |
| PUT    | `/api/account/password`         | Change own password         | done — current secret required; other sessions revoked | `modules/identity/account.TestChangePasswordRevokesOtherSessions` |
| GET    | `/api/account/sessions`         | List own sessions           | done   | `modules/identity/account.TestSessionListAndRevoke` |
| DELETE | `/api/account/sessions/{id}`    | Revoke one own session      | done   | `modules/identity/account.TestSessionListAndRevoke` |
| POST   | `/api/auth/forgot-password`     | Request a password reset    | done — anonymous; always 204; queues recovery email | `modules/identity/recovery.TestForgotIsAlwaysGeneric` |
| POST   | `/api/auth/reset-password`      | Reset with a reset token    | done — hashed single-use token; revokes sessions; rotates cookies | `modules/identity/recovery.TestResetLifecycle` |

Shared rules: both endpoints ride the tight auth rate budget; recovery responses never reveal
whether the address exists; reset tokens are SHA-256 hashed with purpose-prefixed keys and
expire in 15 minutes; a completed reset revokes every sign-in session of the account and issues
a fresh session for the requester; audit events cover sign-in, sign-out, password changes, and
reset requests/completions without logging secrets.

## MFA TOTP (tango-only)

Upstream Pocket ID has no TOTP; this surface is tango-only and follows the database contract in
`llms/porting-plan/database.md` (`user_mfa_totp`, `user_mfa_recovery_codes`,
`user_mfa_pending`).

| Method | Endpoint                        | Summary / Yaak Title         | Status | Evidence |
| ------ | ------------------------------- | ---------------------------- | ------ | -------- |
| POST   | `/api/mfa/totp/enroll`          | Start TOTP enrollment        | done — self; returns the raw secret + otpauth URI exactly once; re-enroll replaces an unconfirmed row | `modules/identity/totp.TestTOTPLifecycle` |
| POST   | `/api/mfa/totp/confirm`         | Confirm and enable TOTP      | done — verifies one code; sets `confirmed_at`; returns recovery codes exactly once | `modules/identity/totp.TestTOTPLifecycle` |
| GET    | `/api/mfa/totp/status`          | TOTP status                  | done — confirmed flag + remaining recovery-code count | `modules/identity/totp.TestTOTPLifecycle` |
| POST   | `/api/mfa/totp/verify`          | Complete a pending sign-in   | done — pending-auth cookie; accepts a TOTP code or a recovery code; issues the full session | `modules/identity/totp.TestTOTPLifecycle` |
| POST   | `/api/mfa/totp/recovery-codes`  | Rotate recovery codes        | done — requires a valid TOTP code; returns the new codes exactly once | `modules/identity/totp.TestTOTPLifecycle` |
| DELETE | `/api/mfa/totp`                 | Disable TOTP                 | done — requires the current password; drops all MFA state | `modules/identity/totp.TestTOTPLifecycle` |

Fixed parameters: issuer = the configured app name, 6 digits, 30-second period, SHA-1,
±1 step bounded skew. Sign-in composition: a confirmed TOTP enrollment turns a successful
password sign-in into a pending authentication (5-minute TTL, one row per user, cookie-bound)
instead of a full session; the full session is issued only by `verify`. Pending state is never
a session flag, expires server-side, is replaced on the next sign-in, and is cleared on
sign-out. TOTP verification is constant-time with step replay protection (`last_used_step`);
recovery codes are hashed, single-use, shown exactly once, and rotated atomically. Disablement
requires the current password and clears every MFA row.

## API Keys

| Method | Endpoint                   | Summary / Yaak Title | Status | Evidence |
| ------ | -------------------------- | -------------------- | ------ | -------- |
| GET    | `/api/api-keys`            | List API keys        | done   | `modules/admin/apikey.TestKeyPagination` || POST   | `/api/api-keys`            | Create API key       | done — session auth only, API keys cannot create | `modules/admin/apikey.TestKeyRoutesAreSessionGuarded`, `modules/admin/apikey.TestKeyLifecycle` |
| DELETE | `/api/api-keys/{id}`       | Revoke API key       | done   | `modules/admin/apikey.TestKeyLifecycle` |
| POST   | `/api/api-keys/{id}/renew` | Renew API key        | done — session auth only, API keys cannot renew | `modules/admin/apikey.TestKeyRoutesAreSessionGuarded`, `modules/admin/apikey.TestKeyLifecycle` |

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
| GET    | `/api/audit-logs/all`                  | List all audit logs  | done — admin listing; device summary parsed from the user agent | `modules/admin/auditlog.TestListEndpointEnvelopeAndPagination` |
| GET    | `/api/audit-logs/filters/client-names` | List client names    | done — admin only                  | `modules/admin/auditlog.TestListFiltersByUserAndEvent` |
| GET    | `/api/audit-logs/filters/users`        | List users with IDs  | done — admin only                  | `modules/admin/auditlog.TestListFiltersByUserAndEvent` |

## Custom Claims

| Method | Endpoint                                      | Summary / Yaak Title                  | Status | Evidence |
| ------ | --------------------------------------------- | ------------------------------------- | ------ | -------- |
| GET    | `/api/custom-claims/suggestions`              | Get custom claim suggestions          | done — keys ordered by usage count | `modules/admin/customclaim.TestClaimsForUserAndGroup` |
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

## Webhooks (tango-only)

Upstream Pocket ID has no webhooks; this surface is tango-only and follows the database contract
in `llms/porting-plan/database.md` (`webhook_endpoints`, `webhook_deliveries`,
`webhook_delivery_attempts`).

| Method | Endpoint                          | Summary / Yaak Title        | Status | Evidence |
| ------ | --------------------------------- | --------------------------- | ------ | -------- |
| GET    | `/api/webhooks`                   | List webhook endpoints      | done — admin guard; `enabled` and `event` filters; secrets never present | `modules/webhook.TestCreateListGetUpdateDeleteLifecycle` |
| POST   | `/api/webhooks`                   | Create a webhook endpoint   | done — 201; returns the signing secret exactly once | `modules/webhook.TestCreateListGetUpdateDeleteLifecycle` |
| GET    | `/api/webhooks/{id}`              | Get a webhook endpoint      | done — no secret field | `modules/webhook.TestCreateListGetUpdateDeleteLifecycle` |
| PUT    | `/api/webhooks/{id}`              | Update a webhook endpoint   | done — partial update; nil fields keep values | `modules/webhook.TestCreateListGetUpdateDeleteLifecycle` |
| DELETE | `/api/webhooks/{id}`              | Delete a webhook endpoint   | done — deliveries survive with `webhook_id` nulled | `modules/webhook.TestDeleteKeepsDeliveries` |
| POST   | `/api/webhooks/{id}/rotate-secret`| Rotate the signing secret   | done — returns the new plaintext exactly once; new deliveries sign with it | `modules/webhook.TestRotateSecretInvalidatesTheOldSignature` |
| POST   | `/api/webhooks/{id}/test`         | Send a test delivery        | done — 202 + delivery id; bypasses the subscription filter | `modules/webhook.TestTestEndpointAcceptsAndQueues` |
| GET    | `/api/webhooks/{id}/deliveries`   | List deliveries of one endpoint | done — newest first, paginated; latest attempt rides along | `modules/webhook.TestDeliveriesEndpointsListScopedAndGlobal` |
| GET    | `/api/webhook-deliveries`         | List all deliveries         | done — `event` filter; redacted response metadata only | `modules/webhook.TestDeliveriesEndpointsListScopedAndGlobal` |

Delivery contract: HMAC-SHA256 over `t=<unix>,v1=<hex>` where the digest covers the signed
timestamp concatenated with the exact canonical body bytes. Headers on every delivery:
`X-Signature` (timestamp + `v1` digest, ±5-minute verification skew), `X-Webhook-Event` (event
name), `X-Webhook-Id` (endpoint id), `Content-Type: application/json`. The canonical body is the
deterministic JSON encoding of the payload, capped at 1 MiB, stored once as immutable bytes and
reused byte-for-byte by every retry — the signature therefore stays valid across retries. Custom
registration headers cannot override the signature set. Subscriptions use event names or the
`*` wildcard; an empty list receives every event. Retries run on the queue (5 attempts, 30 s
backoff, 30 s receiver deadline); non-2xx and transport failures are recorded per attempt and
pruned after a week. Rotation affects new deliveries only and never returns the stored
ciphertext.

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