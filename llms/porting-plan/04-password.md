---
status: done
updated: 2026-09-16
---

# Password Authentication

Goal: provide secure password authentication without coupling password policy to session storage.

Prerequisites: complete transport normalization and inspect the identity boundary. Password,
session, and MFA belong under `modules/identity`; they are use cases of one identity module, not
independent runtime plugins. Keep credential verification and session creation behind identity
services; handlers only coordinate validation and responder output.

## Security and Yaak acceptance criteria

- Invalid sign-in and forgot-password requests do not reveal account existence.
- Password hashes and reset tokens are never returned or logged; reset tokens are hashed, expiring,
  and single-use. Do not use `enc:` for values that only need verification.
- Reset invalidates the intended sessions and rotates authentication cookies.
- Cookies use configured Secure, HttpOnly, SameSite, and Path attributes.
- Update and re-send Yaak requests for cookie attributes, recovery bodies, auth headers, status codes,
  and generic forgot-password responses. Postgres tests cover expiry, replay, and concurrent use.

## Tasks

1. Define contracts for `POST /api/auth/sign-in`, `POST /api/auth/sign-out`,
   `GET /api/auth/session`, password change, forgot-password, and reset-password. Specify generic
   recovery responses, token expiry, one-time use, cookie flags, failures, rate limits, and audit
   events. Commit: `docs: define password authentication contracts`.
2. Complete Postgres password storage and policy using the existing crypto package. Prevent account
   enumeration and add only the rate-limit hooks needed by the current transport. Commit:
   `feat: complete password credential service`.
3. Implement password sign-in and session binding with secure cookies, disabled-user handling,
   invalid-credential behavior, audit events, and responder errors. Commit:
   `feat: add password sign-in flow`.
4. Implement hashed, expiring, single-use reset tokens, queued email delivery, generic responses,
   session invalidation after reset, replay protection, and expiry handling. Commit:
   `feat: add password recovery flow`.
5. Verify success, invalid credentials, disabled accounts, recovery, reset, replay, expiry, cookie
   rotation, and anonymous access through Yaak and focused tests. Commit:
   `test: verify password authentication lifecycle`.
