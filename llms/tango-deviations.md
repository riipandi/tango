---
status: active
updated: 2026-09-16
---

# Tango Deviations from Upstream Pocket ID

Deliberate contract differences from upstream Pocket ID v2.14.0. Anything not listed here must
match the upstream endpoint contract.

## Excluded upstream features

- **LDAP directory synchronization** — no LDAP config surface, no sync endpoint, no runtime
  wiring, and no directory-key columns. Do not reintroduce LDAP settings or clients.
- **Application Images management** — the `/api/application-images/*` endpoints are not mounted
  and their Yaak requests were removed. The bundled default profile picture is still served
  through the user profile-picture fallback; OIDC client logos use the shared blob store
  directly.
- **SQLite and MySQL** — Postgres is the only supported database.

## Additions beyond upstream

- **`/api/healthz`** — an envelope-form health probe inside the API group for deploy tooling
  that cannot reach the root router; the upstream-parity `GET /healthz` (204, no body) stays
  mounted at the root.
- **`/.well-known/version`** — a bare version document under `.well-known` for instance
  fingerprinting; the upstream-parity version endpoints stay under `/api/version/*`.

## Encrypted and hashed value inventory

Recoverable values are sealed by `pkg/crypto` in the canonical `enc:<ciphertext>` form
(AES-256-GCM, base64 RawStdEncoding). Unprefixed values are invalid; there is no legacy format.
Verification-only values are one-way hashes and must never be encrypted.

| Value | Owner | Storage |
| --- | --- | --- |
| Webhook signing secret | webhook | `enc:` — workers recover it to sign deliveries; DB CHECK enforces the prefix |
| SCIM service-provider token | federation | `enc:` — the server must send it; DB CHECK enforces the prefix |
| JWKS private key PEM | federation | `enc:` — rotation needs recovery; public key material stays plain |
| TOTP seed (planned, MFA phase) | identity | `enc:` — verification requires recovery; DB CHECK planned with the table |
| OIDC client secrets | federation | SHA-256 hash — comparison only, raw value shown once at creation |
| Passwords | identity | scrypt/Argon2id PHC hash (`pkg/crypto.PasswordHasher`) |
| Session tokens | identity | SHA-256 `token_hash` on sessions; the raw token lives only in the cookie |
| Auth tokens (email verification, one-time access, reauthentication) | identity | SHA-256 hash keyed by purpose |
| Signup tokens | identity | SHA-256 hash |
| API keys | admin | SHA-256 hash; raw value shown once at creation/renewal |
| Device login device token | identity | SHA-256 hash |
| Recovery codes (planned, MFA phase) | identity | one hash row per code with a single-use timestamp |

Cipher consumers (the only `crypto.Cipher` wirings): the webhook module, the SCIM store, and the
JWKS key service — each keyed from a SHA-256 digest of `AUTH_SECRET_KEY` at the composition root.
