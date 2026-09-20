# API Endpoint

Summary of the public API surface, grouped by area.

## Reference

This document is self-contained: the tables below are the authoritative description of every route
the server mounts, and a route that is not listed is not served.

Two conventions apply throughout:

- **Upstream parity** — a section notes where the surface diverges from Pocket ID. Sections marked
  *tango-only* have no upstream equivalent; the rest mirror the upstream contract so a Pocket ID
  client can be pointed at this server.
- **Transport** — an operation is served either as ConnectRPC below `/rpc` or as HTTP/REST. The
  protocol column states which, and the two never overlap for the same operation.

## Transport and authentication

- **ConnectRPC** below `/rpc` — the SPA, the admin console, and internal tools call the generated
  clients from `api/connect/*.proto`. Every procedure is called with `POST` and
  `Content-Type: application/json` plus `Connect-Protocol-Version: 1`; `GET` is reserved for
  procedures that declare `idempotency_level = NO_SIDE_EFFECTS`, and no procedure does. Errors use
  the Connect code/message document.
- **HTTP/REST** below `/api`, `/authorize`, or a documented root path — protocol and infrastructure
  surfaces only: OAuth/OIDC, WebAuthn ceremonies, device-login request and exchange, email links,
  the auth worker's cookie bridge, health, and discovery.
- **Credentials** — `Authorization: Bearer <access-token>` for protected RPCs. Machine clients may
  also send `X-API-KEY` on the admin application API only; every self-service and
  credential-lifecycle surface refuses a machine credential, and API key create and renew stay
  session-only so a leaked key cannot extend itself or rotate its owner's password. Cookies are
  token storage and never authorize an RPC. Browser ceremony surfaces (device approval, MFA, the
  auth lifecycle) never accept a machine credential.

## Lifetimes

Every authentication lifetime is configured through the environment, in
**seconds**, and validated at startup: a non-positive value, or a short session
longer than the remembered one, fails startup instead of degrading at runtime.

| Variable | Bounds |
| --- | --- |
| `AUTH_ACCESS_TOKEN_EXPIRY` | internal RPC bearer JWT; refresh stays cookie-only |
| `AUTH_SESSION_LIFETIME` | session issued with `remember: true` |
| `AUTH_SESSION_SHORT_LIFETIME` | session issued with `remember: false` or absent |
| `OIDC_ACCESS_TOKEN_EXPIRY` | provider default; a client's own duration overrides it |
| `OIDC_REFRESH_TOKEN_EXPIRY` | provider default; a client's own duration overrides it |
| `OIDC_AUTHORIZATION_CODE_EXPIRY` | one-time authorization code |
| `OIDC_INTERACTION_EXPIRY` | sign-in / consent interaction bridge |
| `OIDC_DEVICE_CODE_EXPIRY` | device authorization window (RFC 8628) |
| `OIDC_PAR_EXPIRY` | pushed authorization `request_uri` (RFC 9126) |

## Authentication

Password authentication is a tango-only surface; upstream Pocket ID signs users in with passkeys
only. Recovery stays on HTTP: the `ForgotPassword` and `ResetPassword` RPCs answer `unimplemented`.
Reading and updating the caller's own profile live on `UserService` (`GetSession` for the read),
matching upstream's `GET`/`PUT /api/users/me`; the account surface carries only the tango-only
password and session procedures.

`SignIn` takes `{"identity": "<username or email>", "password": "<plaintext>", "remember": <bool>}`.
`remember` selects the session duration: `true` issues the long lifetime
(`AUTH_SESSION_LIFETIME`), `false` or absent the short one
(`AUTH_SESSION_SHORT_LIFETIME`). Both are seconds and must be configured so the
short lifetime does not exceed the long one. The choice is stored on the session, so a sliding
refresh or token rotation never promotes a short session to the long lifetime. The
response echoes the mode as `remember`.

| Method   | Procedure / Endpoint                                         | Protocol     | Summary                             |
| -------- | ------------------------------------------------------------ | ------------ | ----------------------------------- |
| POST     | `/rpc/tango.identity.v1.AuthService/SignIn`                  | ConnectRPC   | Sign in with password               |
| POST     | `/rpc/tango.identity.v1.AuthService/SignOut`                 | ConnectRPC   | Sign out                            |
| POST     | `/rpc/tango.identity.v1.AuthService/GetSession`              | ConnectRPC   | Inspect current session, including the caller's user |
| POST     | `/rpc/tango.identity.v1.AuthService/ForgotPassword`          | ConnectRPC   | Request a password reset (unimplemented; use the REST route) |
| POST     | `/rpc/tango.identity.v1.AuthService/ResetPassword`           | ConnectRPC   | Reset a password (unimplemented; use the REST route) |
| POST     | `/rpc/tango.identity.v1.AccountService/ChangePassword`       | ConnectRPC   | Change own password                 |
| POST     | `/rpc/tango.identity.v1.AccountService/ListSessions`         | ConnectRPC   | List own sessions                   |
| POST     | `/rpc/tango.identity.v1.AccountService/RevokeSession`        | ConnectRPC   | Revoke one own session              |
| POST     | `/api/auth/token`                                            | HTTP/REST    | Cookie bridge for the auth worker   |
| POST     | `/api/auth/sign-out`                                         | HTTP/REST    | Sign out (cookie channel)           |
| POST     | `/api/auth/forgot-password`                                  | HTTP/REST    | Request a password reset            |
| POST     | `/api/auth/reset-password`                                   | HTTP/REST    | Reset with a reset token            |

## MFA TOTP

Tango-only; upstream Pocket ID has no TOTP. A confirmed enrollment turns a successful password
sign-in into a pending authentication that only `VerifyPending` completes.

| Method   | Procedure / Endpoint                                         | Protocol     | Summary                      |
| -------- | ------------------------------------------------------------ | ------------ | ---------------------------- |
| POST     | `/rpc/tango.identity.v1.MfaService/EnrollTotp`               | ConnectRPC   | Start TOTP enrollment        |
| POST     | `/rpc/tango.identity.v1.MfaService/ConfirmTotp`              | ConnectRPC   | Confirm and enable TOTP      |
| POST     | `/rpc/tango.identity.v1.MfaService/GetTotpStatus`            | ConnectRPC   | TOTP status                  |
| POST     | `/rpc/tango.identity.v1.MfaService/VerifyPending`            | ConnectRPC   | Complete a pending sign-in   |
| POST     | `/rpc/tango.identity.v1.MfaService/RotateRecoveryCodes`      | ConnectRPC   | Rotate recovery codes        |
| POST     | `/rpc/tango.identity.v1.MfaService/DisableTotp`              | ConnectRPC   | Disable TOTP                 |

## OAuth

Relying-party protocol surfaces. Client administration moved to ConnectRPC (see OIDC).

| Method      | Procedure / Endpoint                      | Protocol     | Summary                                  |
| ----------- | ----------------------------------------- | ------------ | ---------------------------------------- |
| GET, POST   | `/authorize`                              | HTTP/REST    | Authorization endpoint                   |
| POST        | `/api/oidc/token`                         | HTTP/REST    | Token endpoint                           |
| POST        | `/api/oidc/introspect`                    | HTTP/REST    | Introspect OIDC tokens (RFC 7662)        |
| POST        | `/api/oidc/par`                           | HTTP/REST    | Push authorization request (RFC 9126)    |
| POST        | `/api/oidc/device/authorize`              | HTTP/REST    | Device authorization grant (RFC 8628)    |
| GET         | `/api/oidc/device/info`                   | HTTP/REST    | Device code info for the consent page    |
| POST        | `/api/oidc/device/verify`                 | HTTP/REST    | Approve or deny a device code            |
| GET, POST   | `/api/oidc/userinfo`                      | HTTP/REST    | Get user information                     |
| GET, POST   | `/api/oidc/end-session`                   | HTTP/REST    | RP-initiated logout                      |
| GET         | `/api/oidc/interaction/{id}`              | HTTP/REST    | Read the authorization interaction       |
| POST        | `/api/oidc/interaction/{id}/approve`      | HTTP/REST    | Approve the authorization interaction    |
| GET         | `/api/oidc/clients/{clientId}/logo`       | HTTP/REST    | Get client logo                          |

## WebAuthn

Passkey ceremonies are a browser contract and stay on HTTP.

| Method   | Procedure / Endpoint              | Protocol    | Summary                             |
| -------- | --------------------------------- | ----------- | ----------------------------------- |
| POST     | `/api/webauthn/register/begin`    | HTTP/REST   | Begin passkey registration          |
| POST     | `/api/webauthn/register/finish`   | HTTP/REST   | Finish passkey registration         |
| POST     | `/api/webauthn/login/begin`       | HTTP/REST   | Begin discoverable passkey login    |
| POST     | `/api/webauthn/login/finish`      | HTTP/REST   | Finish discoverable passkey login   |

## API Key

Manage API keys for authentication. The surface is self-scoped: a key may list and revoke its own
rows, while create and renew require a session.

| Method   | Procedure / Endpoint                              | Protocol     | Summary          |
| -------- | ------------------------------------------------- | ------------ | ---------------- |
| POST     | `/rpc/tango.admin.v1.ApiKeyService/List`          | ConnectRPC   | List API keys    |
| POST     | `/rpc/tango.admin.v1.ApiKeyService/Create`        | ConnectRPC   | Create API key   |
| POST     | `/rpc/tango.admin.v1.ApiKeyService/Renew`         | ConnectRPC   | Renew API key    |
| POST     | `/rpc/tango.admin.v1.ApiKeyService/Delete`        | ConnectRPC   | Revoke API key   |

## Application Configuration

Configure application settings.

| Method   | Procedure / Endpoint                                                         | Protocol     | Summary                                  |
| -------- | ---------------------------------------------------------------------------- | ------------ | ---------------------------------------- |
| POST     | `/rpc/tango.admin.v1.ApplicationConfigurationService/Get`                    | ConnectRPC   | Public bootstrap configuration           |
| POST     | `/rpc/tango.admin.v1.ApplicationConfigurationService/GetAll`                 | ConnectRPC   | List all application configurations      |
| POST     | `/rpc/tango.admin.v1.ApplicationConfigurationService/Update`                 | ConnectRPC   | Update application configurations        |
| POST     | `/rpc/tango.admin.v1.ApplicationConfigurationService/TestEmail`              | ConnectRPC   | Send test email                          |
| GET      | `/api/application-configuration`                                             | HTTP/REST    | List public application configurations   |

## Audit Logs

Access and manage audit logs.

| Method   | Procedure / Endpoint                                    | Protocol     | Summary               |
| -------- | ------------------------------------------------------- | ------------ | --------------------- |
| POST     | `/rpc/tango.admin.v1.AuditLogService/List`              | ConnectRPC   | List audit logs       |
| POST     | `/rpc/tango.admin.v1.AuditLogService/ListAll`           | ConnectRPC   | List all audit logs   |
| POST     | `/rpc/tango.admin.v1.AuditLogService/FilterOptions`     | ConnectRPC   | List filter facets    |

## Custom Claim

Manage custom claims for users and groups.

| Method   | Procedure / Endpoint                                                      | Protocol     | Summary                             |
| -------- | ------------------------------------------------------------------------- | ------------ | ----------------------------------- |
| POST     | `/rpc/tango.identity.v1.CustomClaimService/Suggest`                       | ConnectRPC   | Get custom claim suggestions        |
| POST     | `/rpc/tango.identity.v1.CustomClaimService/ListUserClaims`                | ConnectRPC   | List a user's custom claims         |
| POST     | `/rpc/tango.identity.v1.CustomClaimService/CreateUserClaim`               | ConnectRPC   | Create a user custom claim          |
| POST     | `/rpc/tango.identity.v1.CustomClaimService/UpdateUserClaim`               | ConnectRPC   | Update a user custom claim          |
| POST     | `/rpc/tango.identity.v1.CustomClaimService/DeleteUserClaim`               | ConnectRPC   | Delete a user custom claim          |
| POST     | `/rpc/tango.identity.v1.CustomClaimService/ListGroupClaims`               | ConnectRPC   | List a user group's custom claims   |
| POST     | `/rpc/tango.identity.v1.CustomClaimService/CreateGroupClaim`              | ConnectRPC   | Create a group custom claim         |
| POST     | `/rpc/tango.identity.v1.CustomClaimService/UpdateGroupClaim`              | ConnectRPC   | Update a group custom claim         |
| POST     | `/rpc/tango.identity.v1.CustomClaimService/DeleteGroupClaim`              | ConnectRPC   | Delete a group custom claim         |

## OIDC

OIDC client administration and consent records.

| Method   | Procedure / Endpoint                                                                | Protocol     | Summary                                         |
| -------- | ----------------------------------------------------------------------------------- | ------------ | ----------------------------------------------- |
| POST     | `/rpc/tango.federation.v1.OidcClientService/ListClients`                            | ConnectRPC   | List OIDC clients                               |
| POST     | `/rpc/tango.federation.v1.OidcClientService/CreateClient`                           | ConnectRPC   | Create OIDC client                              |
| POST     | `/rpc/tango.federation.v1.OidcClientService/GetClient`                              | ConnectRPC   | Get OIDC client                                 |
| POST     | `/rpc/tango.federation.v1.OidcClientService/UpdateClient`                           | ConnectRPC   | Update OIDC client                              |
| POST     | `/rpc/tango.federation.v1.OidcClientService/DeleteClient`                           | ConnectRPC   | Delete OIDC client                              |
| POST     | `/rpc/tango.federation.v1.OidcClientService/UpdateAllowedUserGroups`                | ConnectRPC   | Update allowed user groups                      |
| POST     | `/rpc/tango.federation.v1.OidcClientService/GetClientMeta`                          | ConnectRPC   | Get client metadata                             |
| POST     | `/rpc/tango.federation.v1.OidcClientService/PreviewClient`                          | ConnectRPC   | Preview OIDC client data for user               |
| POST     | `/rpc/tango.federation.v1.OidcClientService/RefreshClient`                          | ConnectRPC   | Refresh client metadata document                |
| POST     | `/rpc/tango.federation.v1.OidcClientService/UploadLogo`                             | ConnectRPC   | Update client logo                              |
| POST     | `/rpc/tango.federation.v1.OidcClientService/DeleteLogo`                             | ConnectRPC   | Delete client logo                              |
| POST     | `/rpc/tango.federation.v1.OidcClientService/ListSecrets`                            | ConnectRPC   | List client secrets                             |
| POST     | `/rpc/tango.federation.v1.OidcClientService/CreateSecret`                           | ConnectRPC   | Create client secret                            |
| POST     | `/rpc/tango.federation.v1.OidcClientService/DeleteSecret`                           | ConnectRPC   | Delete client secret                            |
| POST     | `/rpc/tango.federation.v1.OidcClientService/GetScimProvider`                        | ConnectRPC   | Get SCIM service provider for a client          |
| POST     | `/rpc/tango.federation.v1.OidcConsentService/ListMyAuthorizedClients`               | ConnectRPC   | List authorized clients for current user        |
| POST     | `/rpc/tango.federation.v1.OidcConsentService/RevokeMyAuthorizedClient`              | ConnectRPC   | Revoke authorization for an OIDC client         |
| POST     | `/rpc/tango.federation.v1.OidcConsentService/ListMyClients`                         | ConnectRPC   | List accessible OIDC clients for current user   |
| POST     | `/rpc/tango.federation.v1.OidcConsentService/ListUserAuthorizedClients`             | ConnectRPC   | List authorized clients for a user              |
| POST     | `/rpc/tango.federation.v1.OidcConsentService/ListAllAuthorizedClients`              | ConnectRPC   | List every authorized client                    |

## Users

User management, sign-up, one-time access, email verification, and profile pictures.

| Method   | Procedure / Endpoint                                                         | Protocol     | Summary                                         |
| -------- | ---------------------------------------------------------------------------- | ------------ | ----------------------------------------------- |
| POST     | `/rpc/tango.identity.v1.SignupService/Signup`                                | ConnectRPC   | Sign up                                         |
| POST     | `/rpc/tango.identity.v1.SignupService/GetSetupAvailability`                  | ConnectRPC   | Check initial admin setup availability          |
| POST     | `/rpc/tango.identity.v1.SignupService/SetupInitialAdmin`                     | ConnectRPC   | Sign up initial admin user                      |
| POST     | `/rpc/tango.identity.v1.SignupService/ListSignupTokens`                      | ConnectRPC   | List signup tokens                              |
| POST     | `/rpc/tango.identity.v1.SignupService/CreateSignupToken`                     | ConnectRPC   | Create signup token                             |
| POST     | `/rpc/tango.identity.v1.SignupService/DeleteSignupToken`                     | ConnectRPC   | Delete signup token                             |
| POST     | `/rpc/tango.identity.v1.UserService/ListUsers`                               | ConnectRPC   | List users                                      |
| POST     | `/rpc/tango.identity.v1.UserService/CreateUser`                              | ConnectRPC   | Create user                                     |
| POST     | `/rpc/tango.identity.v1.UserService/GetUser`                                 | ConnectRPC   | Get user by ID                                  |
| POST     | `/rpc/tango.identity.v1.UserService/UpdateUser`                              | ConnectRPC   | Update user                                     |
| POST     | `/rpc/tango.identity.v1.UserService/DeleteUser`                              | ConnectRPC   | Delete user                                     |
| POST     | `/rpc/tango.identity.v1.UserService/UpdateMe`                                | ConnectRPC   | Update current user                             |
| POST     | `/rpc/tango.identity.v1.UserService/UpdateMyProfilePicture`                  | ConnectRPC   | Update current user's profile picture           |
| POST     | `/rpc/tango.identity.v1.UserService/DeleteMyProfilePicture`                  | ConnectRPC   | Reset current user's profile picture            |
| POST     | `/rpc/tango.identity.v1.UserService/UpdateProfilePicture`                    | ConnectRPC   | Update user profile picture                     |
| POST     | `/rpc/tango.identity.v1.UserService/DeleteProfilePicture`                    | ConnectRPC   | Reset user profile picture                      |
| POST     | `/rpc/tango.identity.v1.UserService/ListUserGroups`                          | ConnectRPC   | Get user groups                                 |
| POST     | `/rpc/tango.identity.v1.UserService/ReplaceUserGroups`                       | ConnectRPC   | Update user groups                              |
| POST     | `/rpc/tango.identity.v1.UserService/ListWebAuthnCredentials`                 | ConnectRPC   | List user passkeys                              |
| POST     | `/rpc/tango.identity.v1.UserService/UpdateWebAuthnCredential`                | ConnectRPC   | Rename user passkey                             |
| POST     | `/rpc/tango.identity.v1.UserService/DeleteWebAuthnCredential`                | ConnectRPC   | Delete user passkey                             |
| POST     | `/rpc/tango.identity.v1.OneTimeAccessService/RequestEmail`                   | ConnectRPC   | Request one-time access email                   |
| POST     | `/rpc/tango.identity.v1.OneTimeAccessService/AdminSendEmail`                 | ConnectRPC   | Request one-time access email (admin)           |
| POST     | `/rpc/tango.identity.v1.OneTimeAccessService/AdminIssueToken`                | ConnectRPC   | Create one-time access token for user (admin)   |
| POST     | `/api/one-time-access-token/{token}`                                         | HTTP/REST    | Exchange one-time access token                  |
| POST     | `/rpc/tango.identity.v1.EmailVerificationService/SendEmail`                  | ConnectRPC   | Send email verification                         |
| POST     | `/api/users/me/verify-email`                                                 | HTTP/REST    | Verify email                                    |
| GET      | `/api/users/{id}/profile-picture.png`                                        | HTTP/REST    | Get user profile picture                        |

## User Groups

User group management operations.

| Method   | Procedure / Endpoint                                                       | Protocol     | Summary                       |
| -------- | -------------------------------------------------------------------------- | ------------ | ----------------------------- |
| POST     | `/rpc/tango.identity.v1.UserGroupService/ListGroups`                       | ConnectRPC   | List user groups              |
| POST     | `/rpc/tango.identity.v1.UserGroupService/CreateGroup`                      | ConnectRPC   | Create user group             |
| POST     | `/rpc/tango.identity.v1.UserGroupService/GetGroup`                         | ConnectRPC   | Get user group by ID          |
| POST     | `/rpc/tango.identity.v1.UserGroupService/UpdateGroup`                      | ConnectRPC   | Update user group             |
| POST     | `/rpc/tango.identity.v1.UserGroupService/DeleteGroup`                      | ConnectRPC   | Delete user group             |
| POST     | `/rpc/tango.identity.v1.UserGroupService/ListGroupUsers`                   | ConnectRPC   | List users in a group         |
| POST     | `/rpc/tango.identity.v1.UserGroupService/ReplaceGroupUsers`                | ConnectRPC   | Update users in a group       |
| POST     | `/rpc/tango.identity.v1.UserGroupService/ReplaceAllowedOidcClients`        | ConnectRPC   | Update allowed OIDC clients   |

## Well Known

Discovery endpoints for OpenID Connect.

| Method   | Service / Endpoint                          | Protocol    | Summary                                       |
| -------- | ------------------------------------------- | ----------- | --------------------------------------------- |
| GET      | `/.well-known/openid-configuration`         | HTTP/REST   | Get OpenID Connect discovery configuration    |
| GET      | `/.well-known/oauth-authorization-server`   | HTTP/REST   | Get OAuth 2.0 authorization server metadata   |
| GET      | `/.well-known/jwks.json`                    | HTTP/REST   | Get JSON Web Key Set (JWKS)                   |

## APIs

Resource APIs, permissions, and client grants.

| Method   | Procedure / Endpoint                                                       | Protocol     | Summary                                         |
| -------- | -------------------------------------------------------------------------- | ------------ | ----------------------------------------------- |
| POST     | `/rpc/tango.admin.v1.ApiService/ListApis`                                  | ConnectRPC   | List APIs                                       |
| POST     | `/rpc/tango.admin.v1.ApiService/CreateApi`                                 | ConnectRPC   | Create API                                      |
| POST     | `/rpc/tango.admin.v1.ApiService/GetApi`                                    | ConnectRPC   | Get API by ID                                   |
| POST     | `/rpc/tango.admin.v1.ApiService/UpdateApi`                                 | ConnectRPC   | Update API                                      |
| POST     | `/rpc/tango.admin.v1.ApiService/DeleteApi`                                 | ConnectRPC   | Delete API                                      |
| POST     | `/rpc/tango.admin.v1.ApiService/SetPermissions`                            | ConnectRPC   | Update API permissions                          |
| POST     | `/rpc/tango.admin.v1.ApiService/SetCimdAccess`                             | ConnectRPC   | Update metadata document client access          |
| POST     | `/rpc/tango.admin.v1.ApiService/ListAssignableClients`                     | ConnectRPC   | List clients that can still be granted access   |
| POST     | `/rpc/tango.admin.v1.ApiService/ListClients`                               | ConnectRPC   | List clients with access to an API              |
| POST     | `/rpc/tango.admin.v1.ApiService/GrantClient`                               | ConnectRPC   | Grant a client access to an API                 |
| POST     | `/rpc/tango.admin.v1.ApiService/RevokeClient`                              | ConnectRPC   | Revoke a client's access to an API              |
| POST     | `/rpc/tango.admin.v1.ApiService/ListApisForClient`                         | ConnectRPC   | List APIs a client may access                   |
| POST     | `/rpc/tango.admin.v1.ApiService/ListAssignableApisForClient`               | ConnectRPC   | List APIs a client can still be granted         |

## Device Login

The device side is an integration flow and stays on HTTP; the approval UI is first-party and moved
to ConnectRPC.

| Method   | Procedure / Endpoint                                                         | Protocol     | Summary                         |
| -------- | ---------------------------------------------------------------------------- | ------------ | ------------------------------- |
| POST     | `/api/device-login/requests`                                                 | HTTP/REST    | Create device login request     |
| POST     | `/api/device-login/requests/{id}/exchange`                                   | HTTP/REST    | Exchange device login request   |
| POST     | `/rpc/tango.identity.v1.DeviceApprovalService/GetPendingRequest`             | ConnectRPC   | Inspect device login request    |
| POST     | `/rpc/tango.identity.v1.DeviceApprovalService/DecideRequest`                 | ConnectRPC   | Decide device login request     |

## SCIM

SCIM service-provider endpoints: CRUD plus the sync push.

| Method   | Procedure / Endpoint                                               | Protocol     | Summary                         |
| -------- | ------------------------------------------------------------------ | ------------ | ------------------------------- |
| POST     | `/rpc/tango.federation.v1.ScimProviderService/Upsert`              | ConnectRPC   | Create SCIM service provider    |
| POST     | `/rpc/tango.federation.v1.ScimProviderService/Update`              | ConnectRPC   | Update SCIM service provider    |
| POST     | `/rpc/tango.federation.v1.ScimProviderService/Delete`              | ConnectRPC   | Delete SCIM service provider    |
| POST     | `/rpc/tango.federation.v1.ScimProviderService/Sync`                | ConnectRPC   | Sync SCIM service provider      |

## Webhooks

Tango-only outbound webhooks. Every delivery is signed with HMAC-SHA256 and retried on the queue.

| Method   | Procedure / Endpoint                                                | Protocol     | Summary                           |
| -------- | ------------------------------------------------------------------- | ------------ | --------------------------------- |
| POST     | `/rpc/tango.webhook.v1.WebhookService/List`                         | ConnectRPC   | List webhook endpoints            |
| POST     | `/rpc/tango.webhook.v1.WebhookService/Create`                       | ConnectRPC   | Create a webhook endpoint         |
| POST     | `/rpc/tango.webhook.v1.WebhookService/Get`                          | ConnectRPC   | Get a webhook endpoint            |
| POST     | `/rpc/tango.webhook.v1.WebhookService/Update`                       | ConnectRPC   | Update a webhook endpoint         |
| POST     | `/rpc/tango.webhook.v1.WebhookService/Delete`                       | ConnectRPC   | Delete a webhook endpoint         |
| POST     | `/rpc/tango.webhook.v1.WebhookService/RotateSecret`                 | ConnectRPC   | Rotate the signing secret         |
| POST     | `/rpc/tango.webhook.v1.WebhookService/Test`                         | ConnectRPC   | Send a test delivery              |
| POST     | `/rpc/tango.webhook.v1.WebhookService/ListDeliveries`               | ConnectRPC   | List deliveries of one endpoint   |
| POST     | `/rpc/tango.webhook.v1.WebhookService/ListAllDeliveries`            | ConnectRPC   | List all deliveries               |

## Version

Version metadata endpoints.

| Method   | Procedure / Endpoint                                     | Protocol     | Summary                        |
| -------- | -------------------------------------------------------- | ------------ | ------------------------------ |
| POST     | `/rpc/tango.system.v1.VersionService/Current`            | ConnectRPC   | Get current deployed version   |
| POST     | `/rpc/tango.system.v1.VersionService/Latest`             | ConnectRPC   | Get latest available version   |

## Health Check

Healthcheck endpoints plus the transport smoke target.

| Method   | Procedure / Endpoint                               | Protocol     | Summary                    |
| -------- | -------------------------------------------------- | ------------ | -------------------------- |
| GET      | `/healthz`                                         | HTTP/REST    | Liveness; touches no dependency |
| GET      | `/api/healthz`                                     | HTTP/REST    | Readiness document         |
| POST     | `/rpc/tango.system.v1.HealthService/Check`         | ConnectRPC   | Connect transport smoke    |

## Infrastructure

Non-API routes the server mounts. They serve the deployment and the SPA, not the application
contract.

| Method   | Endpoint                | Protocol   | Summary                                    |
| -------- | ----------------------- | ---------- | ------------------------------------------ |
| GET      | `/`                     | HTTP/REST  | SPA document; every unmatched path falls back to it |
| GET      | `/api/`                 | HTTP/REST  | API root document (name, version, platform) |
| GET      | `/.well-known/version`  | HTTP/REST  | Bare version document for tooling          |
| GET      | `/static/*`             | HTTP/REST  | Embedded static assets                     |
