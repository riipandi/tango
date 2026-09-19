# Endpoint Reference (Pocket ID upstream)

Source: <https://pocket-id.org/swagger.yaml> — grouped by spec tag. Use as the Yaak
request checklist: one request per row, named `<METHOD> <path>` for REST and
`<METHOD> /rpc/<package>.<Service>/<Method>` for ConnectRPC. Status vocabulary:
**done** (implemented with test evidence in the Evidence column), **partial**
(implemented with a noted deviation), **planned** (unimplemented — owning phase
named), **excluded** (out of scope per `llms/tango-deviations.md` — never parity work).

Tango-only extensions (not in the upstream spec) and all structural deviations (envelope,
pagination, snake_case) are documented in `llms/tango-deviations.md` — read it before porting
upstream handlers.

## Transport split

First-party application surfaces are ConnectRPC below `/rpc`: the SPA, the admin console, and
internal tools call the generated clients from `api/connect/*.proto`. The `Endpoint` column keeps
the original REST path for traceability; it is no longer mounted. Protocol and infrastructure
surfaces stay HTTP below `/api` (or their root path) and are marked `REST`.

Protected RPCs authenticate with `Authorization: Bearer <internal-access-token>`; the admin
application API also accepts `X-API-KEY` for machine clients. Self-service and credential-lifecycle
procedures — the account surface, `UserService` self procedures, email verification, one-time
access administration, signup-token administration, MFA, and device approval — never accept a
machine credential, so a leaked key cannot rotate its owner's password or edit its owner's profile.
Cookie presence never authorizes an RPC.

Every procedure is called with `POST`; `GET` is reserved for procedures that declare
`idempotency_level = NO_SIDE_EFFECTS`, and no procedure in `api/connect/` does, so a `GET` on any
procedure answers `405` with `Allow: POST` (`internal/transport.TestRPCRejectsWrongMethods`).
`llms/connectrpc-plan/endpoint-reference.md`
is the authoritative transport decision record and service/method matrix; the retained REST set is
pinned by `internal/registry.TestRetainedRESTInventory` and the machine-credential boundary by
`internal/registry.TestRPCMachineCredentialBoundary`.

## Authentication (tango-only)

Password authentication is a tango-only surface: upstream Pocket ID signs users in with passkeys
only. Contracts below define the full password lifecycle.

| Method | Procedure / Endpoint | Summary / Yaak Title | Status | Evidence |
| ------ | -------------------- | -------------------- | ------ | -------- |
| POST | `/rpc/tango.identity.v1.AuthService/SignIn` | Sign in with password | done — indistinguishable failures for unknown identity vs wrong secret; disabled accounts fail closed; sets the token cookies | `modules/identity/session.TestRPCSignInIssuesCookies`, `modules/identity/session.TestRPCSignInPendingFlow` |
| POST | `/rpc/tango.identity.v1.AuthService/SignOut` | Sign out | done — revokes the token family and clears cookies; the REST twin below is the worker's cookie-channel fallback | `modules/identity/session.TestRPCSignOutAndSession` |
| POST | `/rpc/tango.identity.v1.AuthService/GetSession` | Inspect current session | done | `modules/identity/session.TestRPCSignOutAndSession` |
| POST | `/rpc/tango.identity.v1.AuthService/ForgotPassword` | Request a password reset (RPC, unimplemented) | done — declared for contract completeness; answers `unimplemented`; recovery is served by the retained REST routes | `modules/identity/recovery.TestForgotIsAlwaysGeneric` |
| POST | `/rpc/tango.identity.v1.AuthService/ResetPassword` | Reset a password (RPC, unimplemented) | done — declared for contract completeness; answers `unimplemented`; recovery is served by the retained REST routes | `modules/identity/recovery.TestResetLifecycle` |
| POST | `/rpc/tango.identity.v1.AccountService/ChangePassword` | Change own password | done — current secret required; other sessions revoked; refuses a machine credential | `modules/identity/account.TestAccountRPCSelfService` |
| POST | `/rpc/tango.identity.v1.AccountService/ListSessions` | List own sessions | done — self-service; refuses a machine credential | `modules/identity/account.TestAccountRPCSelfService` |
| POST | `/rpc/tango.identity.v1.AccountService/RevokeSession` | Revoke one own session | done — self-service; refuses a machine credential | `modules/identity/account.TestAccountRPCSelfService` |
| POST | `/api/auth/token` | Cookie bridge for the auth worker | REST — access/refresh cookies in, access token + rotation out; never bearer | `modules/identity/session.TestTokenBridgeBootstrapAndRotation`, `modules/identity/session.TestTokenBridgeRejectsAnonymous` |
| POST | `/api/auth/sign-out` | Sign out (cookie channel) | REST — worker fallback when the bearer path is unusable | `modules/identity/session.TestRPCSignOutAndSession` |
| POST | `/api/auth/forgot-password` | Request a password reset | REST — anonymous; always 204; queues recovery email; the RPC twin answers `unimplemented` | `modules/identity/recovery.TestForgotIsAlwaysGeneric` |
| POST | `/api/auth/reset-password` | Reset with a reset token | REST — hashed single-use token; revokes sessions; rotates cookies; the RPC twin answers `unimplemented` | `modules/identity/recovery.TestResetLifecycle` |

Shared rules: the sign-in procedure and both recovery endpoints ride the tight auth rate budget;
recovery responses never reveal whether the address exists; reset tokens are SHA-256 hashed with
purpose-prefixed keys and expire in 15 minutes; a completed reset revokes every sign-in session of
the account and issues a fresh session for the requester; audit events cover sign-in, sign-out,
password changes, and reset requests/completions without logging secrets.

## MFA TOTP (tango-only)

Upstream Pocket ID has no TOTP; this surface is tango-only and follows the database contract in
`llms/porting-plan/database.md` (`user_mfa_totp`, `user_mfa_recovery_codes`,
`user_mfa_pending`).

| Method | Procedure | Summary / Yaak Title | Status | Evidence |
| ------ | --------- | -------------------- | ------ | -------- |
| POST | `/rpc/tango.identity.v1.MfaService/EnrollTotp` | Start TOTP enrollment | done — self; returns the raw secret + otpauth URI exactly once; re-enroll replaces an unconfirmed row | `modules/identity/totp.TestMfaRPCLifecycle` |
| POST | `/rpc/tango.identity.v1.MfaService/ConfirmTotp` | Confirm and enable TOTP | done — verifies one code; sets `confirmed_at`; returns recovery codes exactly once | `modules/identity/totp.TestMfaRPCLifecycle` |
| POST | `/rpc/tango.identity.v1.MfaService/GetTotpStatus` | TOTP status | done — confirmed flag + remaining recovery-code count | `modules/identity/totp.TestMfaRPCLifecycle` |
| POST | `/rpc/tango.identity.v1.MfaService/VerifyPending` | Complete a pending sign-in | done — pending-auth cookie; accepts a TOTP code or a recovery code; issues the full session | `modules/identity/totp.TestMfaRPCVerifyPending` |
| POST | `/rpc/tango.identity.v1.MfaService/RotateRecoveryCodes` | Rotate recovery codes | done — requires a valid TOTP code; returns the new codes exactly once | `modules/identity/totp.TestMfaRPCLifecycle` |
| POST| `/rpc/tango.identity.v1.MfaService/DisableTotp` | Disable TOTP | done — requires the current password; drops all MFA state | `modules/identity/totp.TestMfaRPCLifecycle` |

Fixed parameters: issuer = the configured app name, 6 digits, 30-second period, SHA-1,
±1 step bounded skew. Sign-in composition: a confirmed TOTP enrollment turns a successful
password sign-in into a pending authentication (5-minute TTL, one row per user, cookie-bound)
instead of a full session; the full session is issued only by `VerifyPending`. Pending state is
never a session flag, expires server-side, is replaced on the next sign-in, and is cleared on
sign-out. TOTP verification is constant-time with step replay protection (`last_used_step`);
recovery codes are hashed, single-use, shown exactly once, and rotated atomically. Disablement
requires the current password and clears every MFA row.

## Webhooks (tango-only)

Upstream Pocket ID has no webhooks; this surface is tango-only and follows the database contract
in `llms/porting-plan/database.md` (`webhook_endpoints`, `webhook_deliveries`,
`webhook_delivery_attempts`).

| Method | Procedure | Summary / Yaak Title | Status | Evidence |
| ------ | --------- | -------------------- | ------ | -------- |
| POST | `/rpc/tango.webhook.v1.WebhookService/List` | List webhook endpoints | done — admin guard; `enabled` and `event` filters; secrets never present | `modules/webhook.TestRPCWebhookLifecycle` |
| POST | `/rpc/tango.webhook.v1.WebhookService/Create` | Create a webhook endpoint | done — returns the signing secret exactly once | `modules/webhook.TestRPCWebhookLifecycle` |
| POST | `/rpc/tango.webhook.v1.WebhookService/Get` | Get a webhook endpoint | done — no secret field | `modules/webhook.TestRPCWebhookLifecycle` |
| POST | `/rpc/tango.webhook.v1.WebhookService/Update` | Update a webhook endpoint | done — partial update; absent fields keep values | `modules/webhook.TestRPCWebhookLifecycle` |
| POST | `/rpc/tango.webhook.v1.WebhookService/Delete` | Delete a webhook endpoint | done — deliveries survive with `webhook_id` nulled | `modules/webhook.TestRPCWebhookLifecycle` |
| POST | `/rpc/tango.webhook.v1.WebhookService/RotateSecret` | Rotate the signing secret | done — returns the new plaintext exactly once; new deliveries sign with it | `modules/webhook.TestRPCRotateSecret` |
| POST | `/rpc/tango.webhook.v1.WebhookService/Test` | Send a test delivery | done — queues a `webhook.test` delivery | `modules/webhook.TestRPCTestDelivery` |
| POST | `/rpc/tango.webhook.v1.WebhookService/ListDeliveries` | List deliveries of one endpoint | done — newest first, paginated; latest attempt rides along | `modules/webhook.TestRPCWebhookLifecycle` |
| POST | `/rpc/tango.webhook.v1.WebhookService/ListAllDeliveries` | List all deliveries | done — `event` filter; redacted response metadata only | `modules/webhook.TestRPCWebhookLifecycle` |

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

---

## API Keys

| Method | Procedure | Summary / Yaak Title | Status | Evidence |
| ------ | --------- | -------------------- | ------ | -------- |
| POST | `/rpc/tango.admin.v1.ApiKeyService/List` | List API keys | done — self-scoped; accepts `X-API-KEY` | `modules/admin/apikey.TestRPCKeyLifecycle` |
| POST | `/rpc/tango.admin.v1.ApiKeyService/Create` | Create API key | done — self-scoped; raw value shown once; session-only | `modules/admin/apikey.TestRPCKeyLifecycle` |
| POST | `/rpc/tango.admin.v1.ApiKeyService/Renew` | Renew API key | done — session-only, API keys cannot renew themselves | `modules/admin/apikey.TestRPCKeyErrors` |
| POST | `/rpc/tango.admin.v1.ApiKeyService/Delete` | Revoke API key | done — self-scoped; accepts `X-API-KEY` | `modules/admin/apikey.TestRPCKeyLifecycle` |

## APIs

| Method | Procedure | Summary / Yaak Title | Status | Evidence |
| ------ | --------- | -------------------- | ------ | -------- |
| POST | `/rpc/tango.admin.v1.ApiService/ListApis` | List APIs | done | `modules/admin/apiaccess.TestRPCAPILifecycle` |
| POST | `/rpc/tango.admin.v1.ApiService/CreateApi` | Create API | done | `modules/admin/apiaccess.TestRPCAPILifecycle` |
| POST | `/rpc/tango.admin.v1.ApiService/GetApi` | Get API by ID | done | `modules/admin/apiaccess.TestRPCAPILifecycle` |
| POST | `/rpc/tango.admin.v1.ApiService/UpdateApi` | Update API | done | `modules/admin/apiaccess.TestRPCAPILifecycle` |
| POST | `/rpc/tango.admin.v1.ApiService/DeleteApi` | Delete API | done | `modules/admin/apiaccess.TestRPCAPILifecycle` |
| POST | `/rpc/tango.admin.v1.ApiService/SetPermissions` | Update API permissions | done | `modules/admin/apiaccess.TestRPCAPILifecycle` |
| POST | `/rpc/tango.admin.v1.ApiService/SetCimdAccess` | Update metadata document client access | done | `modules/admin/apiaccess.TestRPCAPILifecycle` |
| POST | `/rpc/tango.admin.v1.ApiService/ListAssignableClients` | List clients that can still be granted access | done | `modules/admin/apiaccess.TestRPCGrantLifecycle` |
| POST | `/rpc/tango.admin.v1.ApiService/ListClients` | List clients with access to an API | done | `modules/admin/apiaccess.TestRPCGrantLifecycle` |
| POST | `/rpc/tango.admin.v1.ApiService/GrantClient` | Grant a client access to an API | done | `modules/admin/apiaccess.TestRPCGrantLifecycle` |
| POST | `/rpc/tango.admin.v1.ApiService/RevokeClient` | Revoke a client's access to an API | done | `modules/admin/apiaccess.TestRPCGrantLifecycle` |
| POST | `/rpc/tango.admin.v1.ApiService/ListApisForClient` | List APIs a client may access | done | `modules/admin/apiaccess.TestRPCGrantLifecycle` |
| POST | `/rpc/tango.admin.v1.ApiService/ListAssignableApisForClient` | List APIs a client can still be granted | done | `modules/admin/apiaccess.TestRPCGrantLifecycle` |

## Application Configuration

| Method | Procedure / Endpoint | Summary / Yaak Title | Status | Evidence |
| ------ | -------------------- | -------------------- | ------ | -------- |
| POST | `/rpc/tango.admin.v1.ApplicationConfigurationService/Get` | Public bootstrap configuration | done — anonymous view the SPA reads before sign-in | `modules/admin/appconfig.TestRPCConfigBootstrapIsAnonymous` |
| POST | `/rpc/tango.admin.v1.ApplicationConfigurationService/GetAll` | List all application configurations | done — admin | `modules/admin/appconfig.TestRPCConfigLifecycle` |
| POST | `/rpc/tango.admin.v1.ApplicationConfigurationService/Update` | Update application configurations | done — partial update | `modules/admin/appconfig.TestRPCConfigLifecycle` |
| POST | `/rpc/tango.admin.v1.ApplicationConfigurationService/TestEmail` | Send test email | done — admin; defaults to the signed-in administrator | `modules/admin/appconfig.TestRPCConfigLifecycle` |
| GET | `/api/application-configuration` | List public application configurations | REST — unauthenticated bootstrap read | `modules/admin/appconfig.TestConfigCRUD` |
| POST | `/api/application-configuration/sync-ldap` | Synchronize LDAP | excluded | — |

## Application Images

| Method | Endpoint | Summary / Yaak Title | Status | Evidence |
| ------ | -------- | -------------------- | ------ | -------- |
| DELETE, GET, PUT | `/api/application-images/*` | Bundled application images | excluded — served from `public/images` through `/static/*` | `internal/transport.TestStaticAssetsHandler` |
| GET | `/api/storage/sqlite-warning` | SQLite storage warning | excluded — Postgres is the only supported database | — |

## Audit Logs

| Method | Procedure | Summary / Yaak Title | Status | Evidence |
| ------ | --------- | -------------------- | ------ | -------- |
| POST | `/rpc/tango.admin.v1.AuditLogService/List` | List audit logs | done — self listing (bearer) | `modules/admin/auditlog.TestRPCListScopesAndAdmin`, `modules/admin/auditlog.TestRPCAnonymousList` |
| POST | `/rpc/tango.admin.v1.AuditLogService/ListAll` | List all audit logs | done — admin listing; device summary parsed from the user agent | `modules/admin/auditlog.TestRPCListScopesAndAdmin` |
| POST | `/rpc/tango.admin.v1.AuditLogService/FilterOptions` | List filter facets | done — admin only; `kind` ∈ `client-names`, `users` | `modules/admin/auditlog.TestRPCListValidation` |

## Custom Claims

| Method | Procedure | Summary / Yaak Title | Status | Evidence |
| ------ | --------- | -------------------- | ------ | -------- |
| POST | `/rpc/tango.identity.v1.CustomClaimService/Suggest` | Get custom claim suggestions | done — keys ordered by usage count | `modules/admin/customclaim.TestRPCClaimLifecycle` |
| POST | `/rpc/tango.identity.v1.CustomClaimService/ListUserClaims` | List a user's custom claims | done | `modules/admin/customclaim.TestRPCClaimLifecycle` |
| POST | `/rpc/tango.identity.v1.CustomClaimService/CreateUserClaim` | Create a user custom claim | done | `modules/admin/customclaim.TestRPCClaimLifecycle` |
| POST | `/rpc/tango.identity.v1.CustomClaimService/UpdateUserClaim` | Update a user custom claim | done | `modules/admin/customclaim.TestRPCClaimLifecycle` |
| POST | `/rpc/tango.identity.v1.CustomClaimService/DeleteUserClaim` | Delete a user custom claim | done | `modules/admin/customclaim.TestRPCClaimLifecycle` |
| POST | `/rpc/tango.identity.v1.CustomClaimService/ListGroupClaims` | List a user group's custom claims | done | `modules/admin/customclaim.TestRPCClaimLifecycle` |
| POST | `/rpc/tango.identity.v1.CustomClaimService/CreateGroupClaim` | Create a group custom claim | done | `modules/admin/customclaim.TestRPCClaimLifecycle` |
| POST | `/rpc/tango.identity.v1.CustomClaimService/UpdateGroupClaim` | Update a group custom claim | done | `modules/admin/customclaim.TestRPCClaimLifecycle` |
| POST | `/rpc/tango.identity.v1.CustomClaimService/DeleteGroupClaim` | Delete a group custom claim | done | `modules/admin/customclaim.TestRPCClaimLifecycle` |

## Device Login

The device side is an integration flow and stays HTTP; the approval UI is first-party and moved to
ConnectRPC.

| Method | Procedure / Endpoint | Summary / Yaak Title | Status | Evidence |
| ------ | -------------------- | -------------------- | ------ | -------- |
| POST | `/api/device-login/requests` | Create device login request | REST — pairing cookie rides the response | `modules/identity/devicelogin.TestCreateApproveExchange` |
| POST | `/api/device-login/requests/{id}/exchange` | Exchange device login request | REST — long-poll; the device holds the request id | `modules/identity/devicelogin.TestCreateApproveExchange` |
| POST | `/rpc/tango.identity.v1.DeviceApprovalService/GetPendingRequest` | Inspect device login request | done — carries the user code | `modules/identity/devicelogin.TestRPCApprovalFlow`, `modules/identity/devicelogin.TestRPCApprovalValidation` |
| POST | `/rpc/tango.identity.v1.DeviceApprovalService/DecideRequest` | Decide device login request | done — carries the user code + approve flag | `modules/identity/devicelogin.TestRPCApprovalFlow` |

## Health

| Method | Endpoint | Summary / Yaak Title | Status | Evidence |
| ------ | -------- | -------------------- | ------ | -------- |
| GET | `/healthz` | Responds to healthchecks | REST — liveness, dependencies untouched | `internal/transport.TestNewHTTPServerRoutes` |
| GET | `/api/healthz` | Readiness document | REST — per-dependency results | `internal/transport.TestNewHTTPServerRoutes` |
| POST | `/rpc/tango.system.v1.HealthService/Check` | Connect transport smoke | done — public smoke target | `internal/transport.TestRPCRouteTable` |

## OIDC

Client administration and consents are first-party and moved to ConnectRPC. The public logo read,
the interaction pages, and every protocol endpoint stay HTTP.

| Method | Procedure / Endpoint | Summary / Yaak Title | Status | Evidence |
| ------ | -------------------- | -------------------- | ------ | -------- |
| POST | `/rpc/tango.federation.v1.OidcClientService/ListClients` | List OIDC clients | done | `modules/federation/oidc.TestRPCClientLifecycle` |
| POST | `/rpc/tango.federation.v1.OidcClientService/CreateClient` | Create OIDC client | done — show-once secret | `modules/federation/oidc.TestRPCClientLifecycle` |
| POST | `/rpc/tango.federation.v1.OidcClientService/GetClient` | Get OIDC client | done | `modules/federation/oidc.TestRPCClientLifecycle` |
| POST | `/rpc/tango.federation.v1.OidcClientService/UpdateClient` | Update OIDC client | done | `modules/federation/oidc.TestRPCClientLifecycle` |
| POST | `/rpc/tango.federation.v1.OidcClientService/DeleteClient` | Delete OIDC client | done | `modules/federation/oidc.TestRPCClientLifecycle` |
| POST | `/rpc/tango.federation.v1.OidcClientService/UpdateAllowedUserGroups` | Update allowed user groups | done | `modules/federation/oidc.TestRPCClientLifecycle` |
| POST | `/rpc/tango.federation.v1.OidcClientService/GetClientMeta` | Get client metadata | done | `modules/federation/oidc.TestRPCClientLifecycle` |
| POST | `/rpc/tango.federation.v1.OidcClientService/PreviewClient` | Preview OIDC client data for user | done — claim maps, no real JWTs; no focused RPC test yet | — |
| POST | `/rpc/tango.federation.v1.OidcClientService/RefreshClient` | Refresh client metadata document | done — CIMD-lite | `modules/federation/oidc.TestRPCIMDClientLifecycle` |
| POST | `/rpc/tango.federation.v1.OidcClientService/UploadLogo` | Update client logo | done — `bytes` payload, content type sniffed; no focused RPC test yet | — |
| POST | `/rpc/tango.federation.v1.OidcClientService/DeleteLogo` | Delete client logo | done; no focused RPC test yet | — |
| POST | `/rpc/tango.federation.v1.OidcClientService/ListSecrets` | List client secrets | done — multi-secret; values never returned | `modules/federation/oidc.TestClientSecretLifecycle` |
| POST | `/rpc/tango.federation.v1.OidcClientService/CreateSecret` | Create client secret | done — show-once | `modules/federation/oidc.TestClientSecretLifecycle` |
| POST | `/rpc/tango.federation.v1.OidcClientService/DeleteSecret` | Delete client secret | done | `modules/federation/oidc.TestClientSecretLifecycle` |
| POST | `/rpc/tango.federation.v1.OidcClientService/GetScimProvider` | Get SCIM service provider for a client | done; no focused RPC test yet | `modules/federation/scimsync.TestProviderGetByClient` (store-level) |
| POST | `/rpc/tango.federation.v1.OidcConsentService/ListMyAuthorizedClients` | List authorized clients for current user | done — revocation cascades to active tokens | `modules/federation/oidc.TestRPCConsentScopes` |
| POST | `/rpc/tango.federation.v1.OidcConsentService/RevokeMyAuthorizedClient` | Revoke authorization for an OIDC client | done | `modules/federation/oidc.TestRPCConsentScopes` |
| POST | `/rpc/tango.federation.v1.OidcConsentService/ListMyClients` | List accessible OIDC clients for current user | done | `modules/federation/oidc.TestRPCConsentScopes` |
| POST | `/rpc/tango.federation.v1.OidcConsentService/ListUserAuthorizedClients` | List authorized clients for a user | done — admin | `modules/federation/oidc.TestRPCConsentScopes` |
| POST | `/rpc/tango.federation.v1.OidcConsentService/ListAllAuthorizedClients` | List every authorized client | done — admin-wide | `modules/federation/oidc.TestRPCConsentScopes` |
| GET | `/api/oidc/clients/{id}/logo` | Get client logo | REST — bare image for the sign-in page | `internal/registry.TestRetainedRESTInventory` |
| GET | `/api/oidc/interaction/{id}` | Read the authorization interaction | REST — browser protocol flow | `modules/federation/oidc.TestAuthorizeRedirectsAnonymousToInteraction` |
| POST | `/api/oidc/interaction/{id}/approve` | Approve the authorization interaction | REST — browser session and redirect behavior | `modules/federation/oidc.TestEndToEndAuthorizeTokenUserinfo` |
| GET, POST | `/authorize` | Authorization endpoint | REST — redirect and OAuth error contract | `modules/federation/oidc.TestEndToEndAuthorizeTokenUserinfo` |
| POST | `/api/oidc/token` | Token endpoint | REST — form encoding, client authentication, RFC errors | `modules/federation/oidc.TestRefreshRotationAndReuseRevocation` |
| POST | `/api/oidc/introspect` | Introspect OIDC tokens | REST — client-scoped RFC 7662 | `modules/federation/oidc.TestIntrospection` |
| POST | `/api/oidc/par` | Push authorization request | REST — RFC 9126; one-time request_uri | `modules/federation/oidc.TestPARPushAndOneTimeAuthorizeResume` |
| POST | `/api/oidc/device/authorize` | Device authorization grant | REST — RFC 8628; hashed codes | `modules/federation/oidc.TestDeviceFlowIssuesTokensAfterApproval` |
| GET | `/api/oidc/device/info` | Device code info for the consent page | REST | `modules/federation/oidc.TestDeviceFlowIssuesTokensAfterApproval` |
| POST | `/api/oidc/device/verify` | Approve or deny a device code | REST — browser session; single approval | `modules/federation/oidc.TestDeviceFlowDenialDeniesThePoll` |
| GET | `/api/oidc/userinfo` | Get user information | REST — bearer token, RFC-style errors | `modules/federation/oidc.TestEndToEndAuthorizeTokenUserinfo` |
| GET, POST | `/api/oidc/end-session` | RP-initiated logout | REST — redirect behavior | `modules/federation/oidc.TestEndSessionRevokesFamilyAndRedirects` |

The protocol endpoints are verified by the same suite: `/authorize` →
`modules/federation/oidc.TestEndToEndAuthorizeTokenUserinfo` and friends. It
accepts an RFC 9126 `request_uri` in place of inline parameters (one-time; a replayed push is
rejected); `/api/oidc/end-session` → `modules/federation/oidc.TestEndSessionRevokesFamilyAndRedirects`:
the `id_token_hint` must verify (issuer, audience, subject, jti), the `client_id` must match the
hint's audience, and the user must have granted the client — every failure redirects to the
instance logout page without explaining why. Ending the session deactivates the grant's whole
token family (the ID token carries the access token's `jti`), replays are idempotent, and an
unregistered `post_logout_redirect_uri` is never followed;
`/.well-known/*` and JWKS → `modules/federation/discovery` and `modules/federation/jwks`. The
discovery document advertises the device, PAR, and introspection endpoints plus upstream's
metadata fields (`response_modes_supported`, `prompt_values_supported`, `grant_types_supported`
with the device grant, `authorization_response_iss_parameter_supported`,
`client_id_metadata_document_supported: false`, `require_pushed_authorization_requests: false`).
Device-flow codes are stored hashed; the poll answers `authorization_pending`, `slow_down`,
`expired_token`, and `access_denied` per RFC 8628 §3.5.

## SCIM

| Method | Procedure | Summary / Yaak Title | Status | Evidence |
| ------ | --------- | -------------------- | ------ | -------- |
| POST | `/rpc/tango.federation.v1.ScimProviderService/Upsert` | Create SCIM service provider | done | `modules/federation/scimsync.TestRPCProviderLifecycle` |
| POST | `/rpc/tango.federation.v1.ScimProviderService/Update` | Update SCIM service provider | done | `modules/federation/scimsync.TestRPCProviderLifecycle` |
| POST | `/rpc/tango.federation.v1.ScimProviderService/Delete` | Delete SCIM service provider | done | `modules/federation/scimsync.TestRPCProviderLifecycle` |
| POST | `/rpc/tango.federation.v1.ScimProviderService/Sync` | Sync SCIM service provider | done — queues outbound sync | `modules/federation/scimsync.TestRPCProviderLifecycle` |

## User Groups

| Method | Procedure | Summary / Yaak Title | Status | Evidence |
| ------ | --------- | -------------------- | ------ | -------- |
| POST | `/rpc/tango.identity.v1.UserGroupService/ListGroups` | List user groups | done | `modules/identity/usergroup.TestGroupRPCAdminCRUD` |
| POST | `/rpc/tango.identity.v1.UserGroupService/CreateGroup` | Create user group | done | `modules/identity/usergroup.TestGroupRPCAdminCRUD` |
| POST | `/rpc/tango.identity.v1.UserGroupService/GetGroup` | Get user group by ID | done | `modules/identity/usergroup.TestGroupRPCAdminCRUD` |
| POST | `/rpc/tango.identity.v1.UserGroupService/UpdateGroup` | Update user group | done | `modules/identity/usergroup.TestGroupRPCAdminCRUD` |
| POST | `/rpc/tango.identity.v1.UserGroupService/DeleteGroup` | Delete user group | done | `modules/identity/usergroup.TestGroupRPCAdminCRUD` |
| POST | `/rpc/tango.identity.v1.UserGroupService/ListGroupUsers` | List users in a group | done | `modules/identity/usergroup.TestGroupRPCAdminCRUD` |
| POST | `/rpc/tango.identity.v1.UserGroupService/ReplaceGroupUsers` | Update users in a group | done — replaces the set atomically | `modules/identity/usergroup.TestGroupRPCAdminCRUD` |
| POST | `/rpc/tango.identity.v1.UserGroupService/ReplaceAllowedOidcClients` | Update allowed OIDC clients | done | `modules/identity/usergroup.TestGroupRPCAdminCRUD` |

## Users

| Method | Procedure / Endpoint | Summary / Yaak Title | Status | Evidence |
| ------ | -------------------- | -------------------- | ------ | -------- |
| POST | `/rpc/tango.identity.v1.SignupService/Signup` | Sign up | done — requires a valid signup token | `modules/identity/signup.TestSignupRequiresValidToken` |
| POST | `/rpc/tango.identity.v1.SignupService/GetSetupAvailability` | Check initial admin setup availability | done — 204 while no user exists, then 404 | `modules/identity/signup.TestSetupAvailabilityAndInitialAdmin` |
| POST | `/rpc/tango.identity.v1.SignupService/SetupInitialAdmin` | Sign up initial admin user | done — 409 once any user exists | `modules/identity/signup.TestSetupAvailabilityAndInitialAdmin` |
| POST | `/rpc/tango.identity.v1.SignupService/ListSignupTokens` | List signup tokens | done | `modules/identity/signup.TestSignupTokenAdminCRUD` |
| POST | `/rpc/tango.identity.v1.SignupService/CreateSignupToken` | Create signup token | done — raw token shown once | `modules/identity/signup.TestSignupTokenAdminCRUD` |
| POST | `/rpc/tango.identity.v1.SignupService/DeleteSignupToken` | Delete signup token | done | `modules/identity/signup.TestSignupTokenAdminCRUD` |
| POST | `/rpc/tango.identity.v1.UserService/ListUsers` | List users | done — admin | `modules/identity/user.TestUserRPCAdminLifecycle` |
| POST | `/rpc/tango.identity.v1.UserService/CreateUser` | Create user | done — admin; show-once password | `modules/identity/user.TestUserRPCAdminLifecycle` |
| POST | `/rpc/tango.identity.v1.UserService/GetUser` | Get user by ID | done — admin; TypeID only | `modules/identity/user.TestUserRPCAdminLifecycle` |
| POST | `/rpc/tango.identity.v1.UserService/UpdateUser` | Update user | done — admin | `modules/identity/user.TestUserRPCAdminLifecycle` |
| POST | `/rpc/tango.identity.v1.UserService/DeleteUser` | Delete user | done — admin | `modules/identity/user.TestUserRPCAdminLifecycle` |
| POST | `/rpc/tango.identity.v1.UserService/UpdateMe` | Update current user | done — self; profile fields only; refuses a machine credential | `modules/identity/user.TestUserRPCSelfBranches` |
| POST | `/rpc/tango.identity.v1.UserService/UpdateMyProfilePicture` | Update current user's profile picture | done — self; raw bytes; refuses a machine credential | `modules/identity/user.TestUserRPCPictureBranches` |
| POST | `/rpc/tango.identity.v1.UserService/DeleteMyProfilePicture` | Reset current user's profile picture | done — self; refuses a machine credential | `modules/identity/user.TestUserRPCPictureBranches` |
| POST | `/rpc/tango.identity.v1.UserService/UpdateProfilePicture` | Update user profile picture | done — admin; raw bytes | `modules/identity/user.TestUserRPCPictureBranches` |
| POST | `/rpc/tango.identity.v1.UserService/DeleteProfilePicture` | Reset user profile picture | done — admin | `modules/identity/user.TestUserRPCPictureBranches` |
| POST | `/rpc/tango.identity.v1.UserService/ListUserGroups` | Get user groups | done — admin | `modules/identity/user.TestUserRPCAdminLifecycle` |
| POST | `/rpc/tango.identity.v1.UserService/ReplaceUserGroups` | Update user groups | done — replaces the set atomically | `modules/identity/user.TestUserRPCAdminLifecycle` |
| POST | `/rpc/tango.identity.v1.UserService/ListWebAuthnCredentials` | List user passkeys | done — admin; key material never leaves the store | `modules/identity/user.TestUserRPCAdminLifecycle` |
| POST | `/rpc/tango.identity.v1.UserService/UpdateWebAuthnCredential` | Rename user passkey | done — admin | `modules/identity/user.TestUserRPCAdminLifecycle` |
| POST | `/rpc/tango.identity.v1.UserService/DeleteWebAuthnCredential` | Delete user passkey | done — admin | `modules/identity/user.TestUserRPCAdminLifecycle` |
| POST | `/rpc/tango.identity.v1.OneTimeAccessService/RequestEmail` | Request one-time access email | done — answers success unconditionally | `modules/identity/onetimeaccess.TestOneTimeAccessRPCBranches` |
| POST | `/rpc/tango.identity.v1.OneTimeAccessService/AdminSendEmail` | Request one-time access email (admin) | done — admin | `modules/identity/onetimeaccess.TestOneTimeAccessRPCBranches` |
| POST | `/rpc/tango.identity.v1.OneTimeAccessService/AdminIssueToken` | Create one-time access token for user (admin) | done — raw token shown once | `modules/identity/onetimeaccess.TestOneTimeAccessRPCBranches` |
| POST | `/api/one-time-access-token/{token}` | Exchange one-time access token | REST — email link; single use, sets the session cookie | `modules/identity/onetimeaccess.TestOneTimeAccessRPCBranches` |
| POST | `/rpc/tango.identity.v1.EmailVerificationService/SendEmail` | Send email verification | done — self; token travels by email only | `modules/identity/emailverification.TestSendEmailRPC` |
| POST | `/api/users/me/verify-email` | Verify email | REST — single-use token scoped to the session user | `modules/identity/emailverification.TestVerifyConsumesScopedToken` |
| GET | `/api/users/{id}/profile-picture.png` | Get user profile picture | REST — bare bytes, default fallback | `modules/identity/user.TestProfilePictureDefaultFallback` |

## WebAuthn

Passkey ceremonies are a browser contract and stay HTTP.

| Method | Endpoint | Summary / Yaak Title | Status | Evidence |
| ------ | -------- | -------------------- | ------ | -------- |
| POST | `/api/webauthn/register/begin` | Begin passkey registration | done — bare `publicKey` options plus an explicit ceremony id | `modules/identity/webauthn.TestRegisterBeginReturnsOptions` |
| POST | `/api/webauthn/register/finish` | Finish passkey registration | done | `modules/identity/webauthn.TestRegisterBeginReturnsOptions` |
| POST | `/api/webauthn/login/begin` | Begin discoverable passkey login | done — bare `publicKey` options plus an explicit ceremony id | `modules/identity/webauthn.TestLoginBeginAnonymousAndFinishValidation` |
| POST | `/api/webauthn/login/finish` | Finish discoverable passkey login | done — fail closed on unknown ceremony sessions | `modules/identity/webauthn.TestLoginBeginAnonymousAndFinishValidation` |

## Version

| Method | Procedure | Summary / Yaak Title | Status | Evidence |
| ------ | --------- | -------------------- | ------ | -------- |
| POST | `/rpc/tango.system.v1.VersionService/Current` | Get current deployed version | done — bearer required | `internal/transport.TestRPCVersionAuthBranches`, `internal/transport.TestRPCVersionCurrentWithoutInterceptor` |
| POST | `/rpc/tango.system.v1.VersionService/Latest` | Get latest available version | done — anonymous; falls back to the deployed build when the feed never answered | `internal/transport.TestRPCVersionAuthBranches`, `internal/transport.TestRPCVersionCurrentWithoutInterceptor` |

## Well Known

| Method | Endpoint | Summary / Yaak Title | Status | Evidence |
| ------ | -------- | -------------------- | ------ | -------- |
| GET | `/.well-known/jwks.json` | Get JSON Web Key Set (JWKS) | REST | `modules/federation/discovery.TestOpenIDConfigurationHandler` |
| GET | `/.well-known/oauth-authorization-server` | Get OAuth 2.0 authorization server metadata | REST | `modules/federation/discovery.TestOpenIDConfigurationHandler` |
| GET | `/.well-known/openid-configuration` | Get OpenID Connect discovery configuration | REST | `modules/federation/discovery.TestOpenIDConfigurationHandler` |
