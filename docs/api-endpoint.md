# API Endpoint

Summary of the public API surface. The row-by-row contract with test evidence lives in
`llms/endpoint-reference.md`; the transport decision record and service matrix live in
`llms/connectrpc-plan/endpoint-reference.md`.

## Transport

- **ConnectRPC** below `/rpc`: the SPA, admin console, and internal tools call the generated
  clients from `api/connect/*.proto`. Unary POST with `Content-Type: application/json` and
  `Connect-Protocol-Version: 1`; Connect error bodies use the code/message document.
- **REST** below `/api` (or the documented root path): protocol and infrastructure surfaces only —
  OAuth/OIDC, WebAuthn ceremonies, device-login request/exchange, email links, the auth worker's
  cookie bridge, health, and discovery.
- **Auth**: `Authorization: Bearer <access-token>` for protected RPCs; machine clients may send
  `X-API-KEY` on the admin application API (API key create/renew stay session-only). Cookies are
  token storage and never authorize an RPC.

## OAuth (protocol, REST)

| Method | Endpoint                                   | Protocol   | Auth                 | Summary                          |
| ------ | ------------------------------------------ | ---------- | -------------------- | -------------------------------- |
| GET    | `/authorize`                               | HTTP/REST  | none                 | Authorization endpoint           |
| POST   | `/api/oidc/token`                          | HTTP/REST  | client credentials   | Token endpoint (form encoded)    |
| POST   | `/api/oidc/introspect`                     | HTTP/REST  | client credentials   | Token introspection (RFC 7662)   |
| POST   | `/api/oidc/par`                            | HTTP/REST  | client credentials   | Pushed authorization (RFC 9126)  |
| POST   | `/api/oidc/device/authorize`               | HTTP/REST  | client credentials   | Device authorization (RFC 8628)  |
| GET    | `/api/oidc/userinfo`                       | HTTP/REST  | relying-party bearer | UserInfo (RFC-style errors)      |
| GET, POST | `/api/oidc/end-session`                 | HTTP/REST  | none                 | RP-initiated logout              |

## Well Known (REST)

| Method | Endpoint                                  | Protocol   | Auth | Summary                                     |
| ------ | ----------------------------------------- | ---------- | ---- | ------------------------------------------- |
| GET    | `/.well-known/openid-configuration`       | HTTP/REST  | none | OpenID Connect discovery                    |
| GET    | `/.well-known/oauth-authorization-server` | HTTP/REST  | none | OAuth 2.0 authorization server metadata     |
| GET    | `/.well-known/jwks.json`                  | HTTP/REST  | none | JSON Web Key Set (JWKS)                     |

## WebAuthn (REST)

| Method | Endpoint                        | Protocol   | Auth                    | Summary                                  |
| ------ | ------------------------------- | ---------- | ----------------------- | ---------------------------------------- |
| POST   | `/api/webauthn/register/begin`  | HTTP/REST  | bearer                  | Begin passkey registration               |
| POST   | `/api/webauthn/register/finish` | HTTP/REST  | bearer                  | Finish passkey registration              |
| POST   | `/api/webauthn/login/begin`     | HTTP/REST  | none                    | Begin discoverable passkey login         |
| POST   | `/api/webauthn/login/finish`    | HTTP/REST  | pending session         | Finish discoverable passkey login        |

## Health

| Method | Endpoint   | Protocol   | Auth | Summary                                 |
| ------ | ---------- | ---------- | ---- | --------------------------------------- |
| GET    | `/healthz` | HTTP/REST  | none | Liveness probe (process up)             |
| GET    | `/api/healthz` | HTTP/REST | none | Readiness with per-dependency results |

## ConnectRPC (first-party)

Everything else the SPA and admin console call lives below `/rpc`, generated from
`api/connect/*.proto`. Highlights per service:

| Service                                  | Package          | Auth                    | Summary                                          |
| ---------------------------------------- | ---------------- | ----------------------- | ------------------------------------------------ |
| `AuthService`                            | `tango.identity` | public + bearer         | Password sign-in, sign-out, session read         |
| `AccountService`                         | `tango.identity` | bearer                  | Self-service profile, password, sessions         |
| `MfaService`                             | `tango.identity` | bearer + pending cookie | TOTP enrollment, verification, recovery codes    |
| `SignupService`                          | `tango.identity` | public + bearer         | Sign-up, initial admin, signup-token admin       |
| `OneTimeAccessService`                   | `tango.identity` | public + bearer         | Email request, admin minting                     |
| `EmailVerificationService`               | `tango.identity` | bearer                  | Send the verification email                      |
| `UserService`                            | `tango.identity` | bearer                  | Admin user CRUD, profile, passkeys               |
| `UserGroupService`                       | `tango.identity` | bearer                  | Admin groups, members, OIDC allowlist            |
| `DeviceApprovalService`                  | `tango.identity` | bearer                  | Approve or deny a device pairing                 |
| `CustomClaimService`                     | `tango.identity` | bearer                  | Admin custom claims for users and groups         |
| `ApiKeyService`                          | `tango.admin`    | bearer + `X-API-KEY`    | Self-scoped machine credentials (create is session-only) |
| `ApiService`                             | `tango.admin`    | bearer + `X-API-KEY`    | Resource API registry and client grants          |
| `ApplicationConfigurationService`        | `tango.admin`    | public + bearer         | Public bootstrap read, admin settings            |
| `AuditLogService`                        | `tango.admin`    | bearer                  | Self listing, admin listing and facets           |
| `OidcClientService`                      | `tango.federation` | bearer + `X-API-KEY`  | OIDC client CRUD, secrets, logos, CIMD           |
| `OidcConsentService`                     | `tango.federation` | bearer                | Authorized-client listing and revocation         |
| `ScimProviderService`                    | `tango.federation` | bearer + `X-API-KEY`  | Outbound SCIM provider configuration             |
| `WebhookService`                         | `tango.webhook`  | bearer + `X-API-KEY`    | Webhook registration, secrets, deliveries        |
| `VersionService`, `HealthService`        | `tango.system`   | mixed                   | Deployed version, transport smoke                |
