---
status: done
updated: 2026-09-19
---

# ConnectRPC Refactor Endpoint Reference

> Scope addendum (2026-09-19): this file is the phase-00 decision record. The route tables in the
> first half describe the pre-cutover world (they are what the matrix replaced); the
> `Connect service and method matrix` below is the authoritative list of live procedures, and
> `internal/registry.TestConnectServiceInventory` plus `TestRetainedRESTInventory` pin what the
> running composition root actually mounts. `llms/endpoint-reference.md` is the row-by-row
> contract with current evidence.

This is the transport decision list for the refactor. `ConnectRPC` means the first-party API is
served under `/rpc/`. `REST` means the existing HTTP route remains authoritative under `/api/` or
its documented root path. `REST +
ConnectRPC` means the same capability has separate protocol-specific and first-party surfaces;
the two contracts must not be merged.

`api/client` is the REST-only SDK. It covers entries marked `REST` and the REST half of entries
marked `REST + ConnectRPC`. It must not expose wrappers for ConnectRPC services; those clients are
generated from `api/connect/*.proto`.

Every entry must have Yaak evidence. Use Yaak MCP to create, update, and send REST requests for
`REST` entries and Connect Protocol HTTP requests for ConnectRPC entries. Do not edit
`api/specs/*.yaml` or other Yaak export files manually. If a request's Connect Protocol content
type, GET semantics, streaming, or optional gRPC compatibility details are unclear, consult the
official Connect Protocol documentation before creating the Yaak request.

This file is an inventory and transport decision record. Before implementation, expand every
wildcard entry into one row per exact method/path and add the canonical Connect service/method.
Each row must receive a Yaak request identifier and evidence status after the request is sent.

## Protocol rules

| Protocol | Use for | Primary consumers |
| --- | --- | --- |
| Connect Protocol | Typed application API over `/rpc` | Tango SPA, admin console, CLI, internal tools |
| REST/HTTP | Standard external protocol and retained HTTP API | OAuth/OIDC RPs, SCIM clients, WebAuthn browser APIs, monitoring, REST SDK consumers, webhook receivers |
| REST + ConnectRPC | One domain with distinct internal and external contracts | Device login, OIDC administration, webhook administration |

## Internal bearer-token model

Protected ConnectRPC methods require:

```http
Authorization: Bearer <internal-access-token>
```

Machine clients may instead send `X-API-KEY: <api-key>` against the admin application API. The
credential resolves into the same principal shape and is attached to the request context as a
machine principal; a request carrying both uses the API key. The machine credential reaches the
admin application API only. `ApiKeyService.Create` and `ApiKeyService.Renew` demand a session
principal so a leaked key cannot extend itself, and the self-service and credential-lifecycle
surfaces — `AccountService`, the `UserService` self procedures, `EmailVerificationService`,
`OneTimeAccessService` administration, `SignupService` token administration, MFA, device approval,
WebAuthn, the OIDC protocol, and the auth lifecycle — never accept a machine credential, so a
leaked key cannot rotate its owner's password or edit its owner's profile. A cookie-backed
principal never satisfies an RPC guard: only a bearer token or an API key resolves one.

Access and refresh tokens remain stored in secure cookies. Cookie presence alone must not authorize
an RPC. The frontend worker is responsible for obtaining/refreshing the access token and injecting
the bearer metadata, but the implementation must resolve the browser boundary first: a web worker
cannot read `HttpOnly` cookies directly.

The plan must choose and test a token bridge before implementation. The preferred security shape is
for the worker to call a narrowly scoped same-origin bootstrap/refresh operation with credentials,
keep only the short-lived access token in worker memory, and never expose the refresh token to
JavaScript. If a different design makes cookies readable to the worker, its XSS and token-exfiltration
trade-off must be explicitly documented and approved.

Chosen and delivered (phase 04): the session row is the token family — the opaque 256-bit refresh
token lives in the `tango_session` HttpOnly cookie and rotates on every refresh (its SHA-256 hash is
the `sessions.token_hash` lookup; the previous token stops resolving). The access token is a
10-minute RS256 JWT (`iss: tango:internal`, `aud: tango:rpc`, `sub` user TypeID, `sid` session
TypeID, `admin`) mirrored in the HttpOnly `tango_access` cookie scoped to `/api/auth`. The bridge is
`POST /api/auth/token` (credentials: include): a verifiable access cookie is returned as-is;
otherwise the refresh token rotates and both cookies re-issue. `AuthService/SignIn` mints both
cookies; `SignOut` revokes the family and clears all three cookies. RPC verification re-checks the
session by `sid`, so revocation stays exact inside the token TTL. Internal tokens are distinct from
OIDC relying-party tokens: OIDC tokens pin `iss` to the public issuer URL and `aud` to a client id.

The worker boundary uses `comlink` through `vite-plugin-comlink`: the worker module exports a typed
auth API and the frontend consumes it through the plugin-generated `ComlinkWorker` proxy. Keep the
worker API narrow, configure the worker origin explicitly, and release/terminate the worker during
logout and shutdown. The plugin removes the need for application code to call `Comlink.wrap()` or
`Comlink.expose()` directly; Comlink does not make `HttpOnly` cookies readable from the worker.

Internal access/refresh tokens are not OIDC relying-party tokens. OIDC token, UserInfo, introspection,
PAR, and device-flow authentication remains governed by the external protocol contract.

## Yaak evidence

For each route group, the Yaak request must verify the active wire contract:

- REST: HTTP method, URL, query, headers, cookies/API key, body encoding, status, headers, and
  response body.
- Connect Protocol: HTTP method, `/rpc` routing prefix, package/service/method procedure path,
  `Connect-Protocol-Version`, content type (`application/json` or `application/proto`), bearer or
  `X-API-KEY` metadata, timeout/compression headers, credentials, protobuf/JSON message, HTTP
  status, Connect error body, and response metadata.

The endpoint row is not complete until the request has been sent through Yaak MCP against the
running server. Request names should use `<METHOD> <path>` for REST and
`<METHOD> /rpc/<package>.<Service>/<Method>` for Connect Protocol requests.

## Authentication and account

| Method | Endpoint | Protocol | Consumer | Notes |
| --- | --- | --- | --- | --- |
| POST | `/api/auth/sign-in` | ConnectRPC | SPA/internal | Sets/rotates token cookies; REST route removed in phase 06. |
| POST | `/api/auth/sign-out` | REST + ConnectRPC | SPA/internal | ConnectRPC is primary; the REST route stays as the worker's documented cookie-channel fallback. |
| GET | `/api/auth/session` | ConnectRPC | SPA/internal | REST route removed in phase 06. |
| POST | `/api/auth/token` | REST | Auth worker | Cookie bridge: access/refresh cookies in, `access_token` + rotation out; HttpOnly channel, never bearer. |
| PUT | `/api/account/password` | ConnectRPC | SPA/internal | Sensitive operation; retain auth rate limit; REST route removed in phase 05. |
| GET | `/api/account/sessions` | ConnectRPC | SPA/internal | REST route removed in phase 05. |
| DELETE | `/api/account/sessions/{id}` | ConnectRPC | SPA/internal | REST route removed in phase 05. |
| POST | `/api/auth/forgot-password` | REST + ConnectRPC | Browser/email bootstrap | ConnectRPC for SPA; retain HTTP option for unauthenticated email flows if needed. |
| POST | `/api/auth/reset-password` | REST + ConnectRPC | Browser/email bootstrap | Do not expose reset secrets in logs or generic RPC errors. |
| POST | `/api/mfa/totp/enroll` | ConnectRPC | SPA/internal | Show-once secret in typed response; REST route removed in phase 05. |
| POST | `/api/mfa/totp/confirm` | ConnectRPC | SPA/internal | Recovery codes are show-once; REST route removed in phase 05. |
| GET | `/api/mfa/totp/status` | ConnectRPC | SPA/internal | REST route removed in phase 05. |
| POST | `/api/mfa/totp/verify` | ConnectRPC | SPA/internal | Pending-auth cookie/session behavior remains unchanged; REST route removed in phase 05. |
| POST | `/api/mfa/totp/recovery-codes` | ConnectRPC | SPA/internal | Show-once codes; REST route removed in phase 05. |
| DELETE | `/api/mfa/totp` | ConnectRPC | SPA/internal | REST route removed in phase 05. |
| POST | `/api/signup` | ConnectRPC | Signup UI / email link | REST route removed in phase 05. |
| GET | `/api/signup/setup` | ConnectRPC | Initial setup UI | Public but owned by the Tango UI. |
| POST | `/api/signup/setup` | ConnectRPC | Initial setup UI | |
| POST | `/api/one-time-access-email` | ConnectRPC | Browser/email bootstrap | REST route removed in phase 05; the token exchange stays REST. |
| POST | `/api/one-time-access-token/{token}` | REST | Email link | Token-in-path exchange is an externalized browser link; keep simple HTTP behavior. |
| POST | `/api/users/me/send-email-verification` | ConnectRPC | SPA/email flow | REST route removed in phase 05; the verify link stays HTTP. |
| POST | `/api/users/me/verify-email` | REST | Email link | Single-use link endpoint; keep HTTP. |

## Users and groups

| Methods | Endpoint | Protocol | Consumer | Notes |
| --- | --- | --- | --- | --- |
| GET, POST | `/api/users` | ConnectRPC | Admin console/internal | `UserService.List` and `Create`; REST route removed in phase 05. |
| GET, PUT, DELETE | `/api/users/{id}` | ConnectRPC | Admin console/internal | REST route removed in phase 05. |
| GET, PUT | `/api/users/me` | ConnectRPC | SPA/internal | REST route removed in phase 05. |
| PUT, DELETE | `/api/users/me/profile-picture` | ConnectRPC | SPA/internal | Raw bytes in the RPC message; REST route removed in phase 05. |
| GET | `/api/users/{id}/profile-picture.png` | REST | Browser/assets | Bare binary response; retained. |
| PUT, DELETE | `/api/users/{id}/profile-picture` | ConnectRPC | Admin console/internal | Raw bytes in the RPC message; REST route removed in phase 05. |
| GET | `/api/users/{id}/groups` | ConnectRPC | Admin console/internal | REST route removed in phase 05. |
| PUT | `/api/users/{id}/user-groups` | ConnectRPC | Admin console/internal | Atomic replacement; REST route removed in phase 05. |
| GET, POST, DELETE | `/api/user-groups` | ConnectRPC | Admin console/internal | REST routes removed in phase 05. |
| GET, PUT | `/api/user-groups/{id}` | ConnectRPC | Admin console/internal | REST routes removed in phase 05. |
| GET, PUT | `/api/user-groups/{id}/users` | ConnectRPC | Admin console/internal | REST routes removed in phase 05. |
| PUT | `/api/user-groups/{id}/allowed-oidc-clients` | ConnectRPC | Admin console/internal | REST route removed in phase 05. |
| GET, PUT, DELETE | `/api/users/{id}/webauthn-credentials/*` | ConnectRPC | Admin console/internal | Management CRUD is internal; ceremony endpoints remain REST. |
| POST | `/api/users/{id}/one-time-access-email` | ConnectRPC | Admin console/internal | Admin action; REST route removed in phase 05 (`OneTimeAccessService.AdminSendEmail`). |
| POST | `/api/users/{id}/one-time-access-token` | ConnectRPC | Admin console/internal | Raw token remains show-once; REST route removed in phase 05 (`OneTimeAccessService.AdminIssueToken`). |

## WebAuthn and device login

| Methods | Endpoint | Protocol | Consumer | Notes |
| --- | --- | --- | --- | --- |
| POST | `/api/webauthn/register/begin` | REST | Browser WebAuthn client | Preserve browser ceremony payload and bare options. |
| POST | `/api/webauthn/register/finish` | REST | Browser WebAuthn client | Preserve authenticator response encoding. |
| POST | `/api/webauthn/login/begin` | REST | Browser WebAuthn client | Anonymous discovery ceremony. |
| POST | `/api/webauthn/login/finish` | REST | Browser WebAuthn client | Sets the session cookie. |
| POST | `/api/device-login/requests` | REST | External device / CLI | Device login is an integration flow. |
| POST | `/api/device-login/requests/{id}/exchange` | REST | External device / CLI | Keep exchange HTTP-compatible. |
| POST | `/api/device-login/verification` | ConnectRPC | Tango approval UI | Internal UI lookup; REST route removed in phase 05. |
| POST | `/api/device-login/verification/decision` | ConnectRPC | Tango approval UI | Internal UI action; REST route removed in phase 05. |

## Admin APIs and configuration

| Methods | Endpoint | Protocol | Consumer | Notes |
| --- | --- | --- | --- | --- |
| GET, DELETE, POST | `/api/api-keys/*` | ConnectRPC | Admin console/internal | API keys remain a machine-auth credential, but management is internal; create/renew demand a session. |
| GET, POST, PUT, DELETE | `/api/apis/*` | ConnectRPC | Admin console/internal | REST routes removed in phase 05. |
| GET | `/api/api-access/{clientId}/apis` | ConnectRPC | Admin console/internal | `ApiService.ListApisForClient`; REST route removed in phase 05. |
| GET | `/api/api-access/{clientId}/assignable-apis` | ConnectRPC | Admin console/internal | `ApiService.ListAssignableApisForClient`; REST route removed in phase 05. |
| GET | `/api/apis/{id}/assignable-clients` | ConnectRPC | Admin console/internal | REST routes removed in phase 05. |
| GET | `/api/apis/{id}/clients` | ConnectRPC | Admin console/internal | REST routes removed in phase 05. |
| PUT, DELETE | `/api/apis/{id}/clients/{clientId}` | ConnectRPC | Admin console/internal | REST routes removed in phase 05. |
| PUT | `/api/apis/{id}/permissions` | ConnectRPC | Admin console/internal | REST routes removed in phase 05. |
| PUT | `/api/apis/{id}/cimd-access` | ConnectRPC | Admin console/internal | REST routes removed in phase 05. |
| GET, PUT | `/api/application-configuration` | ConnectRPC | Admin console/internal | Public bootstrap stays REST; admin surface cut over in phase 05. |
| GET | `/api/application-configuration/all` | ConnectRPC | Admin console/internal | REST route removed in phase 05. |
| POST | `/api/application-configuration/test-email` | ConnectRPC | Admin console/internal | REST route removed in phase 05. |
| GET | `/api/audit-logs` | ConnectRPC | SPA/internal | REST route removed in phase 05. |
| GET | `/api/audit-logs/all` | ConnectRPC | Admin console/internal | REST route removed in phase 05. |
| GET | `/api/audit-logs/filters/*` | ConnectRPC | Admin console/internal | REST routes removed in phase 05. |
| GET | `/api/custom-claims/suggestions` | ConnectRPC | Admin console/internal | REST routes removed in phase 05. |
| GET, POST, PUT, DELETE | `/api/custom-claims/user/{userId}*` | ConnectRPC | Admin console/internal | REST routes removed in phase 05. |
| GET, POST, PUT, DELETE | `/api/custom-claims/user-group/{userGroupId}*` | ConnectRPC | Admin console/internal | REST routes removed in phase 05. |

## OIDC administration and protocol

| Methods | Endpoint | Protocol | Consumer | Notes |
| --- | --- | --- | --- | --- |
| GET, POST, PUT, DELETE | `/api/oidc/clients*` | ConnectRPC | Admin console/internal | Client administration, metadata, logos, secrets, and group access; REST routes removed in phase 05 (logo read stays REST). |
| GET, DELETE | `/api/oidc/users/me/authorized-clients*` | ConnectRPC | SPA/internal | User consent/revocation management; REST routes removed in phase 05. |
| GET | `/api/oidc/users/me/clients` | ConnectRPC | SPA/internal | REST route removed in phase 05. |
| GET | `/api/oidc/users/{id}/authorized-clients` | ConnectRPC | Admin console/internal | REST route removed in phase 05. |
| GET | `/api/oidc/interaction/{id}` | REST | Tango authorization UI | OIDC interaction state is part of the browser protocol flow. |
| POST | `/api/oidc/interaction/{id}/approve` | REST | Tango authorization UI | Preserve browser session and redirect behavior. |
| GET | `/authorize` | REST | External OIDC RP/browser | Redirect and OAuth error contract. |
| POST | `/api/oidc/token` | REST | External OIDC RP | Form encoding, client authentication, and RFC errors. |
| POST | `/api/oidc/introspect` | REST | External OIDC RP/resource server | RFC 7662 contract. |
| POST | `/api/oidc/par` | REST | External OIDC RP | RFC 9126 contract. |
| POST | `/api/oidc/device/authorize` | REST | External device client | RFC 8628 contract. |
| GET | `/api/oidc/device/info` | REST | Device browser/consent page | Keep easy to consume from the device-flow UI. |
| POST | `/api/oidc/device/verify` | REST | Device browser/consent page | Approval is part of the device-flow HTTP surface. |
| GET | `/api/oidc/userinfo` | REST | External OIDC RP | Bearer token and RFC-style errors. |
| GET/POST | `/api/oidc/end-session` | REST | External OIDC RP/browser | RP-initiated logout and redirect behavior. |
| GET | `/.well-known/openid-configuration` | REST | OIDC discovery clients | Bare discovery document. |
| GET | `/.well-known/oauth-authorization-server` | REST | OAuth discovery clients | Bare discovery document. |
| GET | `/.well-known/jwks.json` | REST | OIDC/OAuth clients | Bare JWKS document. |

## SCIM and webhooks

| Methods | Endpoint | Protocol | Consumer | Notes |
| --- | --- | --- | --- | --- |
| POST, PUT, DELETE | `/api/scim/service-provider*` | ConnectRPC | Admin console/internal | Configuration of Tango's outbound SCIM provider; REST routes removed in phase 05. |
| POST | `/api/scim/service-provider/{id}/sync` | ConnectRPC | Admin console/internal | Internal command that triggers outbound HTTP; REST route removed in phase 05. |
| GET, POST, PUT, DELETE | `/api/webhooks*` | ConnectRPC | Admin console/internal | Registration and secret lifecycle; REST routes removed in phase 05. |
| POST | `/api/webhooks/{id}/test` | ConnectRPC | Admin console/internal | Queues an outbound delivery; REST route removed in phase 05. |
| GET | `/api/webhooks/{id}/deliveries` | ConnectRPC | Admin console/internal | REST route removed in phase 05. |
| GET | `/api/webhook-deliveries` | ConnectRPC | Admin console/internal | REST route removed in phase 05. |
| Outbound delivery | Configured webhook URL | REST/HTTP | External application | HMAC signature, retry, and canonical body are HTTP contracts. |
| Outbound sync | Configured SCIM provider URL | REST/SCIM | External provider | Use the SCIM protocol, not ConnectRPC. |

## Health, version, and excluded routes

| Method | Endpoint | Protocol | Consumer | Notes |
| --- | --- | --- | --- | --- |
| GET | `/healthz` | REST | Load balancer/orchestrator | Liveness probe. |
| GET | `/api/healthz` | REST | Deploy tooling | Readiness document. |
| GET | `/api/version/current` | ConnectRPC | Admin/internal | REST route removed in phase 05. |
| GET | `/api/version/latest` | ConnectRPC | Admin/internal | REST route removed in phase 05. |
| All | `/api/application-images/*` | None | None | Excluded and not mounted. |
| POST | `/api/application-configuration/sync-ldap` | None | None | Excluded; LDAP must not be reintroduced. |

## Route migration rule

When a `ConnectRPC` entry is cut over, its old `/api` route is deleted after the generated client,
server handler, first-party callers, and focused tests are complete. Entries marked `REST` remain
HTTP even if a Connect implementation would be technically possible, because their wire contract
belongs to an external standard or an infrastructure consumer.

## Live route inventory (phase 00, 2026-09-19)

Extracted from the running composition root with `chi.Walk` (`MountAPI` + `MountRoot`), 150
method+pattern pairs. Differences against the route tables above — these rows are real and must
appear in the service matrix below:

- `GET /api/account`, `PATCH /api/account` — account profile surface; the tables above only listed
  `/api/account/{password,sessions}`.
- `GET/POST/DELETE /api/signup-tokens*` — signup token management; missing from the tables above.
- `GET /api/oidc/authorized-clients` — admin-wide authorized-client listing; missing above.
- `GET /api/oidc/clients/{id}/scim-service-provider` — per-client SCIM binding lookup; missing above.
- `GET /api/oidc/clients/{clientId}/meta`, `POST /api/oidc/clients/{clientId}/refresh`,
  `GET /api/oidc/clients/{clientId}/preview/{userId}`, `GET/POST/DELETE .../secrets*` — the
  `/api/oidc/clients*` wildcard above under-specified these.
- `POST /api/webhooks/{id}/rotate-secret` — missing above (tables listed `/api/webhooks*` only).
- `GET /api/user-groups/{id}/users` — companion of `PUT` (replace); missing above.
- `GET/POST /api/custom-claims/user/{userId}` and the `user-group` variants, including per-claim
  `PUT/DELETE .../{claimId}` — tables above showed `PUT` only.
- `POST /authorize` — live alongside `GET /authorize`; tables above listed `GET` only.
- `GET /api/apis/{id}` and `GET/PUT/DELETE` register with a trailing-slash chi pattern (`/apis/{id}/`);
  the ambiguity list below resolves how these are recorded.

Transport-owned routes (`/api/`, `/api/healthz`, `/healthz`, `/.well-known/version`, `/static/*`)
mount outside `MountAPI` and are covered by the health/version tables above. The
`/api/version/current` and `/api/version/latest` routes are gone; version metadata serves
`VersionService` below `/rpc`.

## Connect service and method matrix

Canonical protobuf contract for every ConnectRPC entry. Packages express module ownership; every
first-party route maps to exactly one service method. Auth column: `bearer` = protected RPC
(`Authorization: Bearer <internal-access-token>` required, and the admin application API
procedures also accept `X-API-KEY` for machine clients), `public` = anonymous, `pending` =
pending-auth cookie ceremony as today. Self-service and credential-lifecycle surfaces never accept
a machine credential: the account surface, the `UserService` self procedures,
`EmailVerificationService`, `OneTimeAccessService` administration, `SignupService` token
administration, device approval, MFA, the auth lifecycle, and the OIDC protocol. API key
create/renew additionally stay session-only. Yaak request names follow
`<METHOD> /rpc/<package>.<Service>/<Method>`;
they are created via Yaak MCP in the implementing phase and the row is not complete until sent.

### Package `tango.system.v1` — `api/connect/system.proto`

| Service.Method | Replaces (method + path) | Auth | Notes |
| --- | --- | --- | --- |
| `VersionService.Current` | GET `/api/version/current` | bearer | REST route removed in phase 05. |
| `VersionService.Latest` | GET `/api/version/latest` | public | REST route removed in phase 05. |
| `HealthService.Check` | — (smoke RPC, no REST replacement) | public | Transport smoke target only; `/healthz` and `/api/healthz` stay REST. |

### Package `tango.identity.v1` — `api/connect/identity.proto`

| Service.Method | Replaces (method + path) | Auth | Notes |
| --- | --- | --- | --- |
| `AuthService.SignIn` | POST `/api/auth/sign-in` | public | Rate-limited (rule follows the /rpc procedure); sets/rotates token cookies. |
| `AuthService.SignOut` | POST `/api/auth/sign-out` | bearer | Revokes token family, clears cookies; the REST twin is the worker fallback. |
| `AuthService.GetSession` | GET `/api/auth/session` | bearer | |
| `AuthService.ForgotPassword` | POST `/api/auth/forgot-password` | public | REST is authoritative for recovery; the RPC answers `unimplemented`. Rate-limited. Yaak: `Request a password reset (RPC, unimplemented)` → 501. |
| `AuthService.ResetPassword` | POST `/api/auth/reset-password` | public | REST is authoritative for recovery; the RPC answers `unimplemented`. Rate-limited. Yaak: `Reset a password (RPC, unimplemented)` → 501. |
| `AccountService.GetAccount` | GET `/api/account` | bearer | Self; refuses a machine credential. |
| `AccountService.UpdateAccount` | PATCH `/api/account` | bearer | Self; refuses a machine credential. Shares `UpdateProfileRequest` and the `UpdateProfile` store call with `UserService.UpdateMe` (ambiguity A4). Yaak: `Update own account` → 200 with a bearer, 401 with a machine credential. |
| `AccountService.ChangePassword` | PUT `/api/account/password` | bearer | Self; rate-limited; revokes other sessions. |
| `AccountService.ListSessions` | GET `/api/account/sessions` | bearer | Self. |
| `AccountService.RevokeSession` | DELETE `/api/account/sessions/{id}` | bearer | Self. |
| `SignupService.Signup` | POST `/api/signup` | public | REST route removed in phase 05. |
| `SignupService.GetSetupAvailability` | GET `/api/signup/setup` | public | REST route removed in phase 05. |
| `SignupService.SetupInitialAdmin` | POST `/api/signup/setup` | public | REST route removed in phase 05. |
| `SignupService.ListSignupTokens` | GET `/api/signup-tokens` | bearer | Admin; REST route removed in phase 05. |
| `SignupService.CreateSignupToken` | POST `/api/signup-tokens` | bearer | Admin; show-once; REST route removed in phase 05. |
| `SignupService.DeleteSignupToken` | DELETE `/api/signup-tokens/{token_id}` | bearer | Admin; REST route removed in phase 05. |
| `MfaService.EnrollTotp` | POST `/api/mfa/totp/enroll` | bearer | Show-once secret; REST route removed in phase 05. |
| `MfaService.ConfirmTotp` | POST `/api/mfa/totp/confirm` | bearer | Show-once recovery codes; REST route removed in phase 05. |
| `MfaService.GetTotpStatus` | GET `/api/mfa/totp/status` | bearer | REST route removed in phase 05. |
| `MfaService.VerifyPending` | POST `/api/mfa/totp/verify` | pending | Pending-auth cookie unchanged; REST route removed in phase 05. |
| `MfaService.RotateRecoveryCodes` | POST `/api/mfa/totp/recovery-codes` | bearer | Show-once; REST route removed in phase 05. |
| `MfaService.DisableTotp` | DELETE `/api/mfa/totp` | bearer | REST route removed in phase 05. |
| `OneTimeAccessService.RequestEmail` | POST `/api/one-time-access-email` | public | REST route removed in phase 05. |
| `OneTimeAccessService.AdminSendEmail` | POST `/api/users/{user_id}/one-time-access-email` | bearer | REST route removed in phase 05. |
| `OneTimeAccessService.AdminIssueToken` | POST `/api/users/{user_id}/one-time-access-token` | bearer | Show-once token; REST route removed in phase 05. |
| `EmailVerificationService.SendEmail` | POST `/api/users/me/send-email-verification` | bearer | REST route removed in phase 05. |
| `UserService.ListUsers` | GET `/api/users` | bearer | Admin; `query` rides PageRequest. |
| `UserService.GetUser` | GET `/api/users/{user_id}` | bearer | Admin; TypeID-only. |
| `UserService.CreateUser` | POST `/api/users` | bearer | Admin; show-once password. |
| `UserService.UpdateUser` | PUT `/api/users/{user_id}` | bearer | Admin. |
| `UserService.DeleteUser` | DELETE `/api/users/{user_id}` | bearer | Admin. |
| `UserService.UpdateMe` | PUT `/api/users/me` | bearer | Self. |
| `UserService.UpdateMyProfilePicture` | PUT `/api/users/me/profile-picture` | bearer | Self; `bytes` payload. |
| `UserService.DeleteMyProfilePicture` | DELETE `/api/users/me/profile-picture` | bearer | Self. |
| `UserService.UpdateProfilePicture` | PUT `/api/users/{user_id}/profile-picture` | bearer | Admin; `bytes` payload. |
| `UserService.DeleteProfilePicture` | DELETE `/api/users/{user_id}/profile-picture` | bearer | Admin. |
| `UserService.ListUserGroups` | GET `/api/users/{user_id}/groups` | bearer | Admin. |
| `UserService.ReplaceUserGroups` | PUT `/api/users/{user_id}/user-groups` | bearer | Admin; atomic replacement. |
| `UserService.ListWebAuthnCredentials` | GET `/api/users/{user_id}/webauthn-credentials` | bearer | Admin. |
| `UserService.UpdateWebAuthnCredential` | PUT `/api/users/{user_id}/webauthn-credentials/{credential_id}` | bearer | Admin. |
| `UserService.DeleteWebAuthnCredential` | DELETE `/api/users/{user_id}/webauthn-credentials/{credential_id}` | bearer | Admin. |
| `UserGroupService.ListGroups` | GET `/api/user-groups` | bearer | Admin. |
| `UserGroupService.CreateGroup` | POST `/api/user-groups` | bearer | Admin. |
| `UserGroupService.GetGroup` | GET `/api/user-groups/{group_id}` | bearer | Admin. |
| `UserGroupService.UpdateGroup` | PUT `/api/user-groups/{group_id}` | bearer | Admin. |
| `UserGroupService.DeleteGroup` | DELETE `/api/user-groups/{group_id}` | bearer | Admin. |
| `UserGroupService.ListGroupUsers` | GET `/api/user-groups/{group_id}/users` | bearer | Admin. |
| `UserGroupService.ReplaceGroupUsers` | PUT `/api/user-groups/{group_id}/users` | bearer | Admin; atomic replacement. |
| `UserGroupService.ReplaceAllowedOidcClients` | PUT `/api/user-groups/{group_id}/allowed-oidc-clients` | bearer | Admin. |
| `DeviceApprovalService.GetPendingRequest` | POST `/api/device-login/verification` | bearer | Contract fixed in phase 05: the request carries the user_code. |
| `DeviceApprovalService.DecideRequest` | POST `/api/device-login/verification/decision` | bearer | Request carries user_code + approve; REST route removed in phase 05. |
| `CustomClaimService.Suggest` | GET `/api/custom-claims/suggestions` | bearer | REST route removed in phase 05. |
| `CustomClaimService.ListUserClaims` | GET `/api/custom-claims/user/{user_id}` | bearer | |
| `CustomClaimService.CreateUserClaim` | POST `/api/custom-claims/user/{user_id}` | bearer | |
| `CustomClaimService.UpdateUserClaim` | PUT `/api/custom-claims/user/{user_id}/{claim_id}` | bearer | |
| `CustomClaimService.DeleteUserClaim` | DELETE `/api/custom-claims/user/{user_id}/{claim_id}` | bearer | |
| `CustomClaimService.ListGroupClaims` | GET `/api/custom-claims/user-group/{group_id}` | bearer | |
| `CustomClaimService.CreateGroupClaim` | POST `/api/custom-claims/user-group/{group_id}` | bearer | |
| `CustomClaimService.UpdateGroupClaim` | PUT `/api/custom-claims/user-group/{group_id}/{claim_id}` | bearer | |
| `CustomClaimService.DeleteGroupClaim` | DELETE `/api/custom-claims/user-group/{group_id}/{claim_id}` | bearer | |

### Package `tango.admin.v1` — `api/connect/admin.proto`

> ApiKeyService is session-authenticated and scoped to the caller — the same self-scoped contract as the REST routes.

| Service.Method | Replaces (method + path) | Auth | Notes |
| --- | --- | --- | --- |
| `ApiKeyService.List` | GET `/api/api-keys` | bearer or X-API-KEY | Self-scoped. |
| `ApiKeyService.Create` | POST `/api/api-keys` | bearer | Self-scoped; show-once secret; session-only. |
| `ApiKeyService.Renew` | POST `/api/api-keys/{id}/renew` | bearer | Self-scoped; show-once secret; session-only. |
| `ApiKeyService.Delete` | DELETE `/api/api-keys/{id}` | bearer or X-API-KEY | Self-scoped. |
| `ApiService.ListApis` | GET `/api/apis` | bearer | REST route removed in phase 05. |
| `ApiService.CreateApi` | POST `/api/apis` | bearer | REST route removed in phase 05. |
| `ApiService.GetApi` | GET `/api/apis/{id}` | bearer | See ambiguity A3 (trailing slash). |
| `ApiService.UpdateApi` | PUT `/api/apis/{id}` | bearer | See ambiguity A3. |
| `ApiService.DeleteApi` | DELETE `/api/apis/{id}` | bearer | See ambiguity A3. |
| `ApiService.SetPermissions` | PUT `/api/apis/{id}/permissions` | bearer | |
| `ApiService.SetCimdAccess` | PUT `/api/apis/{id}/cimd-access` | bearer | |
| `ApiService.ListAssignableClients` | GET `/api/apis/{id}/assignable-clients` | bearer | |
| `ApiService.ListClients` | GET `/api/apis/{id}/clients` | bearer | |
| `ApiService.GrantClient` | PUT `/api/apis/{id}/clients/{client_id}` | bearer | |
| `ApiService.RevokeClient` | DELETE `/api/apis/{id}/clients/{client_id}` | bearer | |
| `ApiService.ListApisForClient` | GET `/api/api-access/{clientId}/apis` | bearer | Client-scoped view added in phase 05; closes the `/api/api-access/*` gap. |
| `ApiService.ListAssignableApisForClient` | GET `/api/api-access/{clientId}/assignable-apis` | bearer | Client-scoped view added in phase 05. |
| `ApplicationConfigurationService.Get` | GET `/api/application-configuration` | bearer | Public bootstrap view; anonymous. |
| `ApplicationConfigurationService.GetAll` | GET `/api/application-configuration/all` | bearer | REST route removed in phase 05. |
| `ApplicationConfigurationService.Update` | PUT `/api/application-configuration` | bearer | REST route removed in phase 05. |
| `ApplicationConfigurationService.TestEmail` | POST `/api/application-configuration/test-email` | bearer | REST route removed in phase 05. |
| `AuditLogService.List` | GET `/api/audit-logs` | bearer | Self-scoped; REST route removed in phase 05. |
| `AuditLogService.ListAll` | GET `/api/audit-logs/all` | bearer | REST route removed in phase 05. |
| `AuditLogService.FilterOptions` | GET `/api/audit-logs/filters/{kind}` | bearer | `kind` ∈ `client-names`, `users`; REST route removed in phase 05. |

### Package `tango.federation.v1` — `api/connect/federation.proto`

| Service.Method | Replaces (method + path) | Auth | Notes |
| --- | --- | --- | --- |
| `OidcClientService.ListClients` | GET `/api/oidc/clients/` | bearer | REST route removed in phase 05. |
| `OidcClientService.CreateClient` | POST `/api/oidc/clients/` | bearer | Show-once secret; REST route removed in phase 05. |
| `OidcClientService.GetClient` | GET `/api/oidc/clients/{client_id}` | bearer | REST route removed in phase 05. |
| `OidcClientService.UpdateClient` | PUT `/api/oidc/clients/{client_id}` | bearer | |
| `OidcClientService.DeleteClient` | DELETE `/api/oidc/clients/{client_id}` | bearer | |
| `OidcClientService.UpdateAllowedUserGroups` | PUT `/api/oidc/clients/{client_id}/allowed-user-groups` | bearer | |
| `OidcClientService.GetClientMeta` | GET `/api/oidc/clients/{client_id}/meta` | bearer | |
| `OidcClientService.PreviewClient` | GET `/api/oidc/clients/{client_id}/preview/{user_id}` | bearer | |
| `OidcClientService.RefreshClient` | POST `/api/oidc/clients/{client_id}/refresh` | bearer | |
| `OidcClientService.UploadLogo` | POST `/api/oidc/clients/{client_id}/logo` | bearer | `bytes` payload. |
| `OidcClientService.DeleteLogo` | DELETE `/api/oidc/clients/{client_id}/logo` | bearer | |
| `OidcClientService.ListSecrets` | GET `/api/oidc/clients/{client_id}/secrets` | bearer | |
| `OidcClientService.CreateSecret` | POST `/api/oidc/clients/{client_id}/secrets` | bearer | Show-once. |
| `OidcClientService.DeleteSecret` | DELETE `/api/oidc/clients/{client_id}/secrets/{secret_id}` | bearer | |
| `OidcClientService.GetScimProvider` | GET `/api/oidc/clients/{client_id}/scim-service-provider` | bearer | Ambiguity A2 resolved: the scimsync store adapts onto the lookup port; REST route removed in phase 05. |
| `OidcConsentService.ListMyAuthorizedClients` | GET `/api/oidc/users/me/authorized-clients` | bearer | REST route removed in phase 05. |
| `OidcConsentService.RevokeMyAuthorizedClient` | DELETE `/api/oidc/users/me/authorized-clients/{client_id}` | bearer | |
| `OidcConsentService.ListMyClients` | GET `/api/oidc/users/me/clients` | bearer | |
| `OidcConsentService.ListUserAuthorizedClients` | GET `/api/oidc/users/{user_id}/authorized-clients` | bearer | Ambiguity A1 resolved: both listings live in OidcConsentService, admin-guarded; REST route removed in phase 05. |
| `OidcConsentService.ListAllAuthorizedClients` | GET `/api/oidc/authorized-clients` | bearer | Admin-wide listing. See ambiguity A1. |
| `ScimProviderService.Upsert` | POST `/api/scim/service-provider` | bearer | REST route removed in phase 05. |
| `ScimProviderService.Update` | PUT `/api/scim/service-provider/{id}` | bearer | |
| `ScimProviderService.Delete` | DELETE `/api/scim/service-provider/{id}` | bearer | |
| `ScimProviderService.Sync` | POST `/api/scim/service-provider/{id}/sync` | bearer | Queues outbound sync. |

### Package `tango.webhook.v1` — `api/connect/webhook.proto`

| Service.Method | Replaces (method + path) | Auth | Notes |
| --- | --- | --- | --- |
| `WebhookService.List` | GET `/api/webhooks` | bearer | REST route removed in phase 05. |
| `WebhookService.Create` | POST `/api/webhooks` | bearer | Show-once signing secret; REST route removed in phase 05. |
| `WebhookService.Get` | GET `/api/webhooks/{id}` | bearer | |
| `WebhookService.Update` | PUT `/api/webhooks/{id}` | bearer | |
| `WebhookService.Delete` | DELETE `/api/webhooks/{id}` | bearer | |
| `WebhookService.RotateSecret` | POST `/api/webhooks/{id}/rotate-secret` | bearer | Show-once; REST route removed in phase 05. |
| `WebhookService.Test` | POST `/api/webhooks/{id}/test` | bearer | Queues delivery. |
| `WebhookService.ListDeliveries` | GET `/api/webhooks/{id}/deliveries` | bearer | |
| `WebhookService.ListAllDeliveries` | GET `/api/webhook-deliveries` | bearer | |

### REST routes that never move (recap)

WebAuthn ceremonies (`/api/webauthn/*`), device-login request/exchange
(`/api/device-login/requests*`), the auth-worker cookie bridge (`/api/auth/token`) and its
sign-out fallback, password recovery (`/api/auth/forgot-password`, `/api/auth/reset-password`),
all `/api/oidc` protocol surfaces (token, introspect, par,
device/authorize, device/info, device/verify, userinfo, end-session, interaction, authorize),
the public client-logo read (`/api/oidc/clients/{id}/logo`), the public config bootstrap
(`/api/application-configuration`), `/api/one-time-access-token/{token}`, `/api/users/me/verify-email`,
`/api/users/{id}/profile-picture.png`, `/healthz`, `/api/healthz`, `/.well-known/*`, `/static/*`.
Their old REST routes are deleted after cutover; everything else in the matrices above is removed
from `/api` once its replacement and callers are verified.

## Generated code layout (decision)

- Contracts: `api/connect/*.proto` — hand-written only, flat layout with module-owning packages
  (`tango.<module>.v1`); committed, never generated into. The buf directory rules
  (`PACKAGE_DIRECTORY_MATCH`, `PACKAGE_SAME_DIRECTORY`, `DIRECTORY_SAME_PACKAGE`) and the RPC
  request/response naming rules (shared `PageRequest`/`Get*Request` reuse) are excluded in
  `buf.yaml` for that reason.
- Generated Go lands in `codegen/proto/go/` (package option
  `github.com/riipandi/tango/codegen/proto/go/tango/<module>/v1`, Go package suffix `<module>v1`) and
  generated TypeScript in `codegen/proto/ts/`; both are build outputs and stay untracked, so a
  clean checkout does not compile until generation runs. Every build path generates first — the
  Dockerfile builder stage, the GoReleaser `before` hooks, and both CI workflows — and buf itself
  resolves from the `go.mod` tool directive via `go tool buf`, so no global binary is installed.
  `task rpc:generate` produces both outputs and refreshes `.rpc-gen.stamp` (hash of contracts + buf
  configs, local tooling — gitignored); `test`, `dev`, `build`, `build:release`, `release`, and
  `typecheck` depend on the generation task, and `task rpc:stale` fails when the contracts change
  without regeneration.
- TypeScript runtime: `@bufbuild/protobuf` + `@connectrpc/connect` (pinned devDeps; the
  `protoc-gen-es` plugin resolves through `pnpm exec` and emits the service descriptors —
  connect-es v2 removed the separate `protoc-gen-connect-es` plugin).
- List RPCs have one shape:
  1. `common.v1.PageRequest` **is** the request when the list has no scope and no filter;
  2. a dedicated `List*Request` **embeds** `common.v1.PageRequest page = N` when the list is scoped
     or filtered;
  3. a list that cannot paginate documents why on its request message instead of silently omitting
     the field (a bounded per-owner collection such as passkeys, sessions, secrets, or claims).
- Every wrapper response carries `common.v1.ResponseMetadata metadata`, which mirrors the REST
  envelope metadata block: `status_code`, `request_id`, `rate_limit`, and the pagination fields.
  The item indices are zero-based and every field is `optional`, so an unknown value is omitted
  from the JSON exactly as the REST envelope omits it. Entity returns (`User`, `Webhook`, ...) and
  `google.protobuf.Empty` carry no metadata by design.
- Pagination rules are defined once in `pkg/responder` and consumed by both transports:
  `page` starts at 1, `limit` defaults to 25 and caps at 100, and `-1` returns every record. On the
  RPC surface a proto3 `int32` cannot express "unset", so `0` takes the default rather than
  returning an unbounded result.
- Task targets: `rpc:generate`, `rpc:lint`, `rpc:breaking`, `rpc:stale`.

## Connect error mapping (contract)

First-party RPC failures use Connect codes; REST equivalents listed for the cutover. Helper
constructors live in `internal/rpcerr` and are the only approved call sites.

| REST status | Connect code | Constructor |
| --- | --- | --- |
| 400 / 422 (validation) | `invalid_argument` | `rpcerr.InvalidArgument` |
| 401 | `unauthenticated` | `rpcerr.Unauthenticated` |
| 403 | `permission_denied` | `rpcerr.PermissionDenied` |
| 404 | `not_found` | `rpcerr.NotFound` |
| 409 | `already_exists` | `rpcerr.AlreadyExists` |
| 429 | `resource_exhausted` | `rpcerr.ResourceExhausted` |
| 5xx | `internal` | `rpcerr.Internal` |

Wire notes: proto JSON omits default-valued scalars (`disabled: false` is never emitted — RPC
consumers apply proto default semantics, not the REST "always present" convention); field-level
validation detail rides the error message until a details message type is frozen; TypeIDs are
strings at the wire boundary; timestamps are RFC 3339 strings, matching the REST DTOs.

## Yaak coverage map (phase 00 inventory)

Workspace `Tango` (`wk_kBiMYTkhPP`), environment `ev_h36MaRumeq`. Existing REST coverage (~100
requests, ~30 folders): Session & Password, TOTP (MFA), OAuth & Device Flows, Account [Tango],
Webhooks [Tango], Sign-up & Setup, User Management, Account (me), One-Time Access, Email
Verification, Passkeys (admin), User Groups, OIDC (Clients/Secrets/Logo/Authorizations/Protocol/
SCIM Providers/CIMD Access), APIs (API Management/Client Grants/CIMD), API Keys, Application
Configuration, Device Login, WebAuthn, SCIM, Version, Health Check, Well Known, OAuth.
Connect Protocol requests did not exist when this plan was written; each implementing phase created
`POST /rpc/<package>.<Service>/<Method>` requests via Yaak MCP. The workspace was later reorganised
by topic (`[Tango] Account`, `[Tango] Authentication`, `[Tango] Webhooks`, and per-domain folders),
so the `[ConnectRPC] <Module>` tree described here no longer exists. A matrix row without a sent
Yaak request is not complete.

## Ambiguity resolutions

A4 and A5 are resolved below. A1, A2, and A3 are closed as historical: the routes they question were
deleted when the ConnectRPC cutover retired them, so no decision is outstanding.

- **A1 — `GET /api/oidc/authorized-clients`** — **closed**: the admin-wide listing became
  `OidcConsentService.ListAllAuthorizedClients` (`/rpc`, admin-guarded) and the REST route was
  removed; the matrix carries the row.
- **A2 — `GET /api/oidc/clients/{id}/scim-service-provider`** — **closed**: the per-client binding
  became `OidcClientService.GetScimProvider`, whose scimsync store adapts onto the lookup port; the
  REST route was removed.
- **A3 — `/api/apis/{id}` trailing-slash chi pattern** — **closed as historical**: `/api/apis*` was
  deleted with the API-registry cutover (`internal/registry/rpc_inventory_test.go` lists it under
  `retiredREST`), so the canonical path shape question no longer applies. The ConnectRPC
  replacement is `ApiService`.
- **A4 — `PATCH /api/account` vs `PUT /api/users/me`** — **resolved**: both are intentional, and they
  own the same field set. `AccountService.UpdateAccount` and `UserService.UpdateMe` both take
  `UpdateProfileRequest` and both call `user.PostgresStore.UpdateProfile` with `first_name`,
  `last_name`, `display_name`, `avatar_url`, and `locale`; neither can touch identity, credentials,
  or admin flags. The pair exists because each replaces a distinct legacy route that the SPA and
  the upstream client already call, and collapsing them would break one of those callers for no
  contract gain. New self-service profile writes should extend both or neither.
- **A5 — `POST /authorize`** — **resolved**: the route serves both `GET` and `POST`
  (`modules/federation/oidc/handler.go:24-25`); `POST` is the form-post entry the provider needs for
  upstream parity. It stays a root-path REST protocol surface.
