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

## Explicit exclusions

- LDAP configuration, synchronization, LDAP environment keys, LDAP runtime wiring, LDAP dev
  service, LDAP client dependency, LDAP tests, and LDAP-only schema columns/indexes.
- Application Images routes, image blob storage, image fixtures, and image-specific Yaak requests.
- SQLite/MySQL adapters and upstream-only internals.

LDAP is already present in the repository and must be removed before parity work. Application Images
must be removed or isolated when its callers are proven unrelated to the in-scope product.

## Contract rule

An excluded endpoint must not be counted as an incomplete parity row or as a completed feature. The
endpoint reference must identify it as `excluded`, route tests must prove it is not mounted, and Yaak
requests must be removed or marked excluded.

## Change control

Any scope change requires updating this file, the porting plan, endpoint reference, deviations
register, and affected Yaak requests before implementation begins.
