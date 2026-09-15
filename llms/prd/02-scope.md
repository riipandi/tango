---
status: draft
updated: 2026-09-15
owner: tango-auth-porting
---

# Scope and Exclusions

## In scope

### Upstream-compatible surfaces

- Health, version, well-known, and discovery documents.
- Users, user groups, signup, one-time access, and email verification.
- WebAuthn credentials, passkey login, and device login.
- OIDC clients, authorization, token, userinfo, introspection, end-session, client access, and
  related authorized-client views.
- API keys, APIs, permissions, grants, rate limits, custom claims, and audit logs.
- SCIM and non-LDAP application configuration that is required by the endpoint contract.

### Tango-specific surfaces

- Password sign-in and session management.
- Forgot-password, reset-password, and password change.
- TOTP MFA and recovery codes.
- Webhook CRUD, signing, delivery, retries, test delivery, and logs.
- Canonical encryption for recoverable secrets using `pkg/crypto` and the `enc:` prefix.

## Explicit exclusions

- LDAP configuration, synchronization, LDAP environment keys, LDAP runtime wiring, LDAP dev
  service, LDAP client dependency, LDAP tests, and LDAP-only schema columns/indexes.
- Application Images routes, image blob storage, image fixtures, and image-specific Yaak requests.
- SQLite/MySQL adapters and upstream-only internals.

LDAP is already present in the repository and must be removed before parity work. Application Images
must be removed or isolated when its callers are proven unrelated to the in-scope product.

## Target module ownership

```text
modules/identity     users, groups, credentials, sessions, MFA, WebAuthn, signup, verification
modules/federation   OIDC, JWKS, discovery, SCIM
modules/admin        app config, audit, API keys, APIs, permissions, custom-claim administration
modules/webhook      endpoint CRUD, signing, delivery, retries, and logs
```

These are application boundaries, not a requirement to create a runtime plugin for every feature.
Existing files may remain split for readability, but route mounting and lifecycle belong to the four
modules.

## Preserved infrastructure ownership

Keep `cmd/launcher` as the only CLI/server entrypoint. Keep the existing flat packages under
`internal/` (`config`, `datastore`, `fetcher`, `jobs`, `kernel`, `logger`, `mailer`, `queue`,
`registry`, `storage`, and `transport`). The registry becomes a concrete composition root; it is not
replaced by `internal/app` or `internal/platform`.

## Contract rule

An excluded endpoint must not be counted as an incomplete parity row or as a completed feature. The
endpoint reference must identify it as `excluded`, route tests must prove it is not mounted, and Yaak
requests must be removed or marked excluded.

## Change control

Any scope change requires updating this file, the porting plan, endpoint reference, deviations
register, affected Yaak requests, and the encryption inventory before implementation begins.
