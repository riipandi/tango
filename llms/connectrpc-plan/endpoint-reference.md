---
status: planned
updated: 2026-09-18
---

# ConnectRPC Refactor Endpoint Reference

This is the transport decision list for the refactor. `ConnectRPC` means the first-party API is
served under `/rpc/`. `REST` means the existing HTTP route remains authoritative under `/api/` or
its documented root path. `REST +
ConnectRPC` means the same capability has separate protocol-specific and first-party surfaces;
the two contracts must not be merged.

`api/client` is the REST-only SDK. It covers entries marked `REST` and the REST half of entries
marked `REST + ConnectRPC`. It must not expose wrappers for ConnectRPC services; those clients are
generated from `api/connect/*.proto`.

Every entry must have Yaak evidence. Use Yaak MCP to create, update, and send REST requests for
`REST` entries and gRPC requests for ConnectRPC entries. Do not edit `api/specs/*.yaml` or other
Yaak export files manually. If a ConnectRPC request's gRPC versus Connect transport details are
unclear, consult the official ConnectRPC documentation before creating the Yaak request.

This file is an inventory and transport decision record. Before implementation, expand every
wildcard entry into one row per exact method/path and add the canonical Connect service/method.
Each row must receive a Yaak request identifier and evidence status after the request is sent.

## Protocol rules

| Protocol | Use for | Primary consumers |
| --- | --- | --- |
| ConnectRPC | Typed application API | Tango SPA, admin console, CLI, internal tools |
| REST/HTTP | Standard external protocol and retained HTTP API | OAuth/OIDC RPs, SCIM clients, WebAuthn browser APIs, monitoring, REST SDK consumers, webhook receivers |
| REST + ConnectRPC | One domain with distinct internal and external contracts | Device login, OIDC administration, webhook administration |

## Yaak evidence

For each route group, the Yaak request must verify the active wire contract:

- REST: HTTP method, URL, query, headers, cookies/API key, body encoding, status, headers, and
  response body.
- gRPC/ConnectRPC: package/service/method, metadata, credentials, protobuf message, status code,
  error details, and decoded response.

The endpoint row is not complete until the request has been sent through Yaak MCP against the
running server. Request names should use `<METHOD> <path>` for REST and the generated
`<package>.<Service>/<Method>` form for gRPC requests.

## Authentication and account

| Method | Endpoint | Protocol | Consumer | Notes |
| --- | --- | --- | --- | --- |
| POST | `/api/auth/sign-in` | ConnectRPC | SPA/internal | Session cookie may be set by the RPC response. |
| POST | `/api/auth/sign-out` | ConnectRPC | SPA/internal | Keep cookie/session middleware shared. |
| GET | `/api/auth/session` | ConnectRPC | SPA/internal | Replace with `SessionService.GetCurrent`. |
| PUT | `/api/account/password` | ConnectRPC | SPA/internal | Sensitive operation; retain auth rate limit. |
| GET | `/api/account/sessions` | ConnectRPC | SPA/internal | |
| DELETE | `/api/account/sessions/{id}` | ConnectRPC | SPA/internal | |
| POST | `/api/auth/forgot-password` | REST + ConnectRPC | Browser/email bootstrap | ConnectRPC for SPA; retain HTTP option for unauthenticated email flows if needed. |
| POST | `/api/auth/reset-password` | REST + ConnectRPC | Browser/email bootstrap | Do not expose reset secrets in logs or generic RPC errors. |
| POST | `/api/mfa/totp/enroll` | ConnectRPC | SPA/internal | Show-once secret in typed response. |
| POST | `/api/mfa/totp/confirm` | ConnectRPC | SPA/internal | Recovery codes are show-once. |
| GET | `/api/mfa/totp/status` | ConnectRPC | SPA/internal | |
| POST | `/api/mfa/totp/verify` | ConnectRPC | SPA/internal | Pending-auth cookie/session behavior remains unchanged. |
| POST | `/api/mfa/totp/recovery-codes` | ConnectRPC | SPA/internal | Show-once codes. |
| DELETE | `/api/mfa/totp` | ConnectRPC | SPA/internal | |
| POST | `/api/signup` | REST + ConnectRPC | Signup UI / email link | ConnectRPC for first-party UI; retain HTTP if signup is reached without the SPA client. |
| GET | `/api/signup/setup` | ConnectRPC | Initial setup UI | Public but owned by the Tango UI. |
| POST | `/api/signup/setup` | ConnectRPC | Initial setup UI | |
| POST | `/api/one-time-access-email` | REST + ConnectRPC | Browser/email bootstrap | Keep HTTP semantics available for email-driven flows. |
| POST | `/api/one-time-access-token/{token}` | REST | Email link | Token-in-path exchange is an externalized browser link; keep simple HTTP behavior. |
| POST | `/api/users/me/send-email-verification` | REST + ConnectRPC | SPA/email flow | ConnectRPC for the action; email verification link remains HTTP-compatible. |
| POST | `/api/users/me/verify-email` | REST | Email link | Single-use link endpoint; keep HTTP. |

## Users and groups

| Methods | Endpoint | Protocol | Consumer | Notes |
| --- | --- | --- | --- | --- |
| GET, POST | `/api/users` | ConnectRPC | Admin console/internal | `UserService.List` and `Create`. |
| GET, PUT, DELETE | `/api/users/{id}` | ConnectRPC | Admin console/internal | |
| GET, PUT | `/api/users/me` | ConnectRPC | SPA/internal | |
| PUT, DELETE | `/api/users/me/profile-picture` | ConnectRPC | SPA/internal | Multipart/image encoding needs a deliberate RPC bytes message. |
| GET | `/api/users/{id}/profile-picture.png` | REST | Browser/assets | Bare binary response. |
| PUT, DELETE | `/api/users/{id}/profile-picture` | ConnectRPC | Admin console/internal | Use bytes in the RPC message, not a REST multipart envelope. |
| GET | `/api/users/{id}/groups` | ConnectRPC | Admin console/internal | |
| PUT | `/api/users/{id}/user-groups` | ConnectRPC | Admin console/internal | Atomic replacement. |
| GET, POST, DELETE | `/api/user-groups` | ConnectRPC | Admin console/internal | |
| GET, PUT | `/api/user-groups/{id}` | ConnectRPC | Admin console/internal | |
| PUT | `/api/user-groups/{id}/users` | ConnectRPC | Admin console/internal | |
| PUT | `/api/user-groups/{id}/allowed-oidc-clients` | ConnectRPC | Admin console/internal | |
| GET, PUT, DELETE | `/api/users/{id}/webauthn-credentials/*` | ConnectRPC | Admin console/internal | Management CRUD is internal; ceremony endpoints remain REST. |
| POST | `/api/users/{id}/one-time-access-email` | ConnectRPC | Admin console/internal | Admin action. |
| POST | `/api/users/{id}/one-time-access-token` | ConnectRPC | Admin console/internal | Raw token remains show-once. |

## WebAuthn and device login

| Methods | Endpoint | Protocol | Consumer | Notes |
| --- | --- | --- | --- | --- |
| POST | `/api/webauthn/register/begin` | REST | Browser WebAuthn client | Preserve browser ceremony payload and bare options. |
| POST | `/api/webauthn/register/finish` | REST | Browser WebAuthn client | Preserve authenticator response encoding. |
| POST | `/api/webauthn/login/begin` | REST | Browser WebAuthn client | Anonymous discovery ceremony. |
| POST | `/api/webauthn/login/finish` | REST | Browser WebAuthn client | Sets the session cookie. |
| POST | `/api/device-login/requests` | REST | External device / CLI | Device login is an integration flow. |
| POST | `/api/device-login/requests/{id}/exchange` | REST | External device / CLI | Keep exchange HTTP-compatible. |
| POST | `/api/device-login/verification` | ConnectRPC | Tango approval UI | Internal UI lookup. |
| POST | `/api/device-login/verification/decision` | ConnectRPC | Tango approval UI | Internal UI action. |

## Admin APIs and configuration

| Methods | Endpoint | Protocol | Consumer | Notes |
| --- | --- | --- | --- | --- |
| GET, DELETE, POST | `/api/api-keys/*` | ConnectRPC | Admin console/internal | API keys remain a machine-auth credential, but management is internal. |
| GET, POST, PUT, DELETE | `/api/apis/*` | ConnectRPC | Admin console/internal | |
| GET | `/api/api-access/{clientId}/apis` | ConnectRPC | Admin console/internal | |
| GET | `/api/api-access/{clientId}/assignable-apis` | ConnectRPC | Admin console/internal | |
| GET | `/api/apis/{id}/assignable-clients` | ConnectRPC | Admin console/internal | |
| GET | `/api/apis/{id}/clients` | ConnectRPC | Admin console/internal | |
| PUT, DELETE | `/api/apis/{id}/clients/{clientId}` | ConnectRPC | Admin console/internal | |
| PUT | `/api/apis/{id}/permissions` | ConnectRPC | Admin console/internal | |
| PUT | `/api/apis/{id}/cimd-access` | ConnectRPC | Admin console/internal | |
| GET, PUT | `/api/application-configuration` | ConnectRPC | Admin console/internal | Sensitive values remain redacted in read responses. |
| GET | `/api/application-configuration/all` | ConnectRPC | Admin console/internal | |
| POST | `/api/application-configuration/test-email` | ConnectRPC | Admin console/internal | Queues the test email. |
| GET | `/api/audit-logs` | ConnectRPC | SPA/internal | |
| GET | `/api/audit-logs/all` | ConnectRPC | Admin console/internal | |
| GET | `/api/audit-logs/filters/*` | ConnectRPC | Admin console/internal | |
| GET | `/api/custom-claims/suggestions` | ConnectRPC | Admin console/internal | |
| PUT | `/api/custom-claims/user/{userId}` | ConnectRPC | Admin console/internal | |
| PUT | `/api/custom-claims/user-group/{userGroupId}` | ConnectRPC | Admin console/internal | |

## OIDC administration and protocol

| Methods | Endpoint | Protocol | Consumer | Notes |
| --- | --- | --- | --- | --- |
| GET, POST, PUT, DELETE | `/api/oidc/clients*` | ConnectRPC | Admin console/internal | Client administration, metadata, logos, secrets, and group access. |
| GET, DELETE | `/api/oidc/users/me/authorized-clients*` | ConnectRPC | SPA/internal | User consent/revocation management. |
| GET | `/api/oidc/users/me/clients` | ConnectRPC | SPA/internal | |
| GET | `/api/oidc/users/{id}/authorized-clients` | ConnectRPC | Admin console/internal | |
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
| POST, PUT, DELETE | `/api/scim/service-provider*` | ConnectRPC | Admin console/internal | Configuration of Tango's outbound SCIM provider. |
| POST | `/api/scim/service-provider/{id}/sync` | ConnectRPC | Admin console/internal | Internal command that triggers outbound HTTP. |
| GET, POST, PUT, DELETE | `/api/webhooks*` | ConnectRPC | Admin console/internal | Registration and secret lifecycle. |
| POST | `/api/webhooks/{id}/test` | ConnectRPC | Admin console/internal | Queues an outbound delivery. |
| GET | `/api/webhooks/{id}/deliveries` | ConnectRPC | Admin console/internal | |
| GET | `/api/webhook-deliveries` | ConnectRPC | Admin console/internal | |
| Outbound delivery | Configured webhook URL | REST/HTTP | External application | HMAC signature, retry, and canonical body are HTTP contracts. |
| Outbound sync | Configured SCIM provider URL | REST/SCIM | External provider | Use the SCIM protocol, not ConnectRPC. |

## Health, version, and excluded routes

| Method | Endpoint | Protocol | Consumer | Notes |
| --- | --- | --- | --- | --- |
| GET | `/healthz` | REST | Load balancer/orchestrator | Liveness probe. |
| GET | `/api/healthz` | REST | Deploy tooling | Readiness document. |
| GET | `/api/version/current` | ConnectRPC | Admin/internal | |
| GET | `/api/version/latest` | ConnectRPC | Admin/internal | |
| All | `/api/application-images/*` | None | None | Excluded and not mounted. |
| POST | `/api/application-configuration/sync-ldap` | None | None | Excluded; LDAP must not be reintroduced. |

## Route migration rule

When a `ConnectRPC` entry is cut over, its old `/api` route is deleted after the generated client,
server handler, first-party callers, and focused tests are complete. Entries marked `REST` remain
HTTP even if a Connect implementation would be technically possible, because their wire contract
belongs to an external standard or an infrastructure consumer.
