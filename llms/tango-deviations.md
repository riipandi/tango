---
status: active
updated: 2026-09-20
---

# Tango Deviations from Upstream Pocket ID

Deliberate contract differences from upstream Pocket ID v2.14.0. Anything not listed here must
match the upstream endpoint contract.

## Excluded upstream features

- **LDAP directory synchronization** — no LDAP config surface, no sync endpoint, no runtime
  wiring, and no directory-key columns. Do not reintroduce LDAP settings or clients.
- **Application Images management** — the `/api/application-images/*` endpoints are not mounted
  and their Yaak artifacts (folder and requests) were removed. The bundled default profile picture
  is still served through the user profile-picture fallback; OIDC client logos use the shared
  blob store directly (`logo_path`; logo presence derives from it).
- **SQLite and MySQL** — Postgres is the only supported database.

## Additions beyond upstream

- **Password authentication surface** — upstream signs users in with passkeys only; tango adds
  `/api/auth/*` (sign-in, sign-out, session inspection, forgot/reset password) and the
  self-service account endpoints. The full contract lives in the endpoint reference
  ("Authentication (tango-only)"): generic enumeration-safe failures, SHA-256 hashed single-use
  reset tokens (15-minute TTL), session invalidation on reset, and per-endpoint rate limits.
- **`AccountService`** — tango-only, and deliberately narrow: `ChangePassword`, `ListSessions`,
  and `RevokeSession`. Upstream has no password or session API because it authenticates with
  passkeys. Self-profile read and write are **not** here — they stay on `UserService`, matching
  upstream's `GET`/`PUT /api/users/me` (read via `AuthService.GetSession`, which already returns
  the full user; write via `UserService.UpdateMe`). Upstream has no `/api/account` route at all,
  so nothing may be added to this service without a deviation entry.
- **Rate limiting** — upstream throttles every `/api` route with a shared token-bucket budget
  plus per-route limiters. Tango instead limits only sensitive endpoints, each with its own
  fixed-window per-IP budget (`internal/transport/middleware/ratelimit.go`): sign-in,
  forgot/reset password, TOTP enroll/confirm/verify/rotation/disablement, signup and initial
  setup, one-time access email/token, device login create/exchange/verify/decision, WebAuthn
  login finish and re-authentication, and email verification send/verify. Admin CRUD, session
  inspection, OIDC token/introspect/PAR/device-authorize/userinfo, health, and version routes
  are intentionally unthrottled. Store failures fail open; throttled responses use the shared
  429 envelope with `Retry-After`.
- **TOTP MFA surface** — `/api/mfa/totp/*` (enroll, confirm, status, verify, recovery-code
  rotation, disablement) per the endpoint reference's MFA contract: `enc:`-sealed seeds, hashed
  single-use recovery codes, pending-auth bridging between password sign-in and full session.
- **`/api/healthz`** — a bare readiness probe inside the API group for deploy tooling that cannot
  reach the root router. Built on `github.com/alexliesenfeld/health`: dependency checks (Postgres
  ping, owned by the composition root) run with bounded timeouts and a 5s result cache; the
  response is the library document `{"status": "up"|"down", "details": {...}}` — 200 only when
  every check passes, 503 otherwise. The upstream-parity `GET /healthz` (204, no body) stays
  mounted at the root as the liveness probe and never touches dependencies.
- **`/.well-known/version`** — a bare version document under `.well-known` for instance
  fingerprinting; the upstream-parity version endpoints stay under `/api/version/*`.

## Response contract

- **JSON field names are snake_case on both transports.** Upstream serves camelCase from its Go
  struct tags; tango normalizes REST and ConnectRPC onto snake_case so one client reads one
  spelling. REST gets it from struct tags, and each RPC service registers a codec that serializes
  protobuf under its declared field names (protobuf's JSON default is lowerCamelCase). Requests
  stay tolerant — protojson accepts either spelling on input.
- **SCIM and WebAuthn keep camelCase.** Both specifications mandate it (`userName`,
  `displayName`, `Resources`; `publicKey`, `challenge`), and neither is part of the envelope
  contract.

## Signup and setup contract

- The setup contract counts **every** existing user, matching upstream: `GET /api/signup/setup`
  answers 204 while no user exists and 404 afterwards; a repeated `POST /api/signup/setup`
  conflicts with 409. Migrations never seed users — fresh installs provision the first admin
  through `tango setup` or the setup endpoint.
- `POST /api/signup` requires `email` (upstream leaves it optional); addresses stay NOT NULL in
  the tango schema, so token signups without an email are rejected with 422.

## Rate limiting

- Two budgets replace upstream's twelve per-route token buckets: the default budget
  (100 requests / 900s) and a tight auth budget (20 requests / 60s) applied by path — sign-in
  surfaces, password change, MFA verification, token minting/exchange, signup, one-time access,
  device login, passkey ceremonies, and email verification. The check is a fixed-window SQL
  function keyed by client IP and class.
- Audit-log entries render `device` from the user agent with `ua-parser/uap-go` (upstream's
  `mileusna/useragent` equivalent format: "<agent family> on <os family> <version>"); geo
  fields (`country`, `city`) are not collected.
- The public configuration payload lists catalog keys only. Upstream appends synthetic
  `uiConfigDisabled` and `tracingEnabled` entries for its SPA; tango has neither a UI-config
  toggle nor OpenTelemetry frontend tracing.

## Passkey ceremony shape

- Ceremony begins answer `{publicKey: <options>, session_id}` — upstream's bare `{publicKey}`
  document plus an explicit ceremony id instead of the upstream session cookie, so clients stay
  stateless across begin/finish.
- Ceremony finishes use `POST` bodies and, for registration, a `session_id` query parameter
  (upstream: `GET /webauthn/*/start` with the ceremony id in a cookie). Responses for a completed
  registration use 201 with the credential view; login sets the session cookie.
- Passkey management is admin-side per user (`/api/users/{id}/webauthn-credentials`,
  `GET`/`PUT`/`DELETE`) instead of upstream's self-service `/api/webauthn/credentials`;
  rename uses `PUT` with `{name}` rather than `PATCH`. Upstream's `/webauthn/logout` is not
  mounted — sign-out runs through the session surface; the reauthentication token purpose
  exists in the token feature without a dedicated webauthn route.

## OIDC protocol failure mapping

Tango maps OAuth/OIDC protocol failures to RFC 6749/6750 error responses instead of copying
upstream's per-endpoint habits (verified live against the upstream parity instance, 2026-09-18):

- **`invalid_client` for all client-authentication failures** — token, introspect, and PAR
  answer 401 `invalid_client` with a short generic description when client credentials are
  missing or unknown. Upstream mixes 400/401 with long library-generated descriptions.
- **`/authorize` with a malformed request answers 400** (JSON envelope) instead of redirecting
  to the `redirect_uri` with `error=invalid_request`; redirect-based error reporting is used
  only for validated requests whose client and redirect URI are known.
- **PAR without client credentials answers 400 `invalid_request`** (the pushed request itself
  is malformed before authentication is evaluated) instead of upstream's 401.
- **userinfo lives at `/api/oidc/userinfo`** (401 without a bearer token); upstream also
  exposes a root-level `/userinfo` alias that tango does not mount.
- **Discovery omits `service_documentation`** (tango ships no self-hosted docs page; the field
  is optional in the discovery spec) and adds `request_parameter_supported: true`; the JWKS
  document marks keys with `"use": "sig"` like upstream.

## Encrypted and hashed value inventory

Recoverable values are sealed by `pkg/crypto` in the canonical `enc:<ciphertext>` form
(AES-256-GCM, base64 RawStdEncoding). Unprefixed values are invalid; there is no legacy format.
Verification-only values are one-way hashes and must never be encrypted.

| Value | Owner | Storage |
| --- | --- | --- |
| Webhook signing secret | webhook | `enc:` — workers recover it to sign deliveries; DB CHECK enforces the prefix |
| SCIM service-provider token | federation | `enc:` — the server must send it; DB CHECK enforces the prefix; reads decrypt strictly (an undecryptable token is an error, never a plaintext fallback) |
| JWKS private key PEM | federation | `enc:` — rotation needs recovery; public key material stays plain |
| TOTP seed | identity | `enc:` — verification requires recovery; DB CHECK enforces the prefix |
| Sensitive app settings (`smtp_password`) | admin | `enc:` — sealed on write, decrypted on read through the module cipher; the DB CHECK rejects plaintext for sensitive keys |
| OIDC client secrets | federation | SHA-256 hashes in the credentials JSONB (multi-secret with per-entry expiry/active state); raw value shown once at creation |
| Passwords | identity | scrypt/Argon2id PHC hash (`pkg/crypto.PasswordHasher`) |
| Session tokens | identity | SHA-256 `token_hash` on sessions; the raw token lives only in the cookie |
| Auth tokens (email verification, one-time access, reauthentication) | identity | SHA-256 hash keyed by purpose |
| Signup tokens | identity | SHA-256 hash |
| API keys | admin | SHA-256 hash; raw value shown once at creation/renewal |
| Device login device token | identity | SHA-256 hash |
| Recovery codes | identity | one hash row per code with a single-use timestamp |

## Database shape deviations

Deltas against the upstream schema dump (`llms/database-reference.sql`) beyond the excluded
features:

- **Authorization codes carry two coordinated rows** — upstream stores the authorize code only
  as a fosite `oauth2_sessions` row (`kind=authorize_code`). Tango splits it: the one-time
  code row (PKCE, scope, nonce) lives in `oidc_authorization_codes` and is consumed by an
  atomic DELETE, while the parked `/authorize` context (redirect URI, session id, audience)
  lives in an `oauth2_sessions` row that is deactivated once the code is spent. The token
  exchange verifies both.
- **API grants are an explicit grant model** — upstream's `oidc_clients_allowed_apis` /
  `oidc_clients_allowed_api_permissions` junction rows carry a `subject_type` column
  (`user`/`client`). Tango replaces them with `oidc_client_api_grants` (per client+API grant
  flags: `user_delegated_access`, `client_access`, `cimd_granted_access`) and
  `oidc_client_api_grant_permissions` (granted scopes with `subject`
  `client`/`user_delegated`/`cimd`), so a grant's access mode is a column, not a duplicated
  junction row.
- **One SCIM provider per client** — upstream allows several `scim_service_providers` rows per
  OIDC client; tango enforces a UNIQUE index on `oidc_client_id` because the provider is the
  client's single outbound provisioning target.
- **`reauthentication_tokens` is folded into `auth_tokens`** — upstream keeps a separate
  table for reauthentication tokens; tango stores them as `auth_tokens` rows with purpose
  `reauthentication`, sharing the SHA-256 hash, expiry, and cleanup path.
- **Device codes keep a dedicated table** — upstream stores device/user codes as
  `oauth2_sessions` kinds; tango models the RFC 8628 state machine explicitly in
  `oidc_device_codes` (status, `last_polled_at`, hashed codes).
- **Group-side client restriction** — `user_groups_allowed_oidc_clients` (a group may only see
  the OIDC clients on its allowlist) is a tango addition; upstream only restricts from the
  client side (`oidc_clients_allowed_user_groups`).

Cipher consumers (the only `crypto.Cipher` wirings): the webhook module, the SCIM store, the JWKS
key service, and the appconfig module — each keyed from a SHA-256 digest of `AUTH_SECRET_KEY` at
the composition root.
