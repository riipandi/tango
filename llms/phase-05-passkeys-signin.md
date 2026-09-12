---
status: planned
updated: 2026-09-12
---

# Phase 5 — Passkeys & Alternative Sign-In

WebAuthn (primary Pocket ID sign-in method), device login, one-time access, email verification,
signup. Pocket ID reference: `backend/internal/webauthn/`, `devicelogin/`, `onetimeaccess/`,
`usersignup/`, `emailverification/`.

## Goal

Passwordless sign-in via passkeys; link-based and code-based sign-in alternatives; self-service
signup.

## Deliverables

- `modules/identity/webauthn` — registration + assertion ceremonies via `github.com/go-webauthn/webauthn`;
  tables `webauthn_credentials`, `webauthn_sessions` (challenge state).
- `modules/identity/devicelogin` — user-code flow (short code, retry limit); table `auth_tokens`
  (device scope).
- `modules/identity/onetimeaccess` — email one-time access links; table `signup_tokens` (or
  dedicated scope).
- `modules/identity/emailverification` — verify email on change/signup; antree task for mail send.
- `modules/identity/signup` — admin-policy signup (open, on request, off), group assignment from
  token; tables `signup_tokens`, `signup_tokens_user_groups`, `invitations`.

## Tasks

- [ ] Add `go-webauthn/webauthn` dependency; implement `webauthn.User` adapter over identity user.
- [ ] `webauthn/store.go` + service — credential registration (attestation), assertion (sign-in),
      sign count validation; session challenge rows.
- [ ] `devicelogin` service/handler — code issue (crypto-random), poll endpoint, approve via
      authenticated session.
- [ ] `onetimeaccess` — token issue per email, single-use, TTL, same-domain redirect validation.
- [ ] `emailverification` — token issue/verify, hook into account email change (Phase 1).
- [ ] `signup` — token-based signup with group assignment, invitation CRUD, policy from appconfig.
- [ ] Emails sent through antree queue tasks (async, retried) — first real queue consumers.
- [ ] Request bodies (device code, OTP, signup) validated via `pkg/validate`.
- [ ] Tests per ceremony against testcontainers; WebAuthn ceremony unit tests with fixed challenge
      inputs.

## Validation

- Passkey register + assert round trip passes in handler tests (mock authenticator).
- Three standard suites + lint + gofmt clean.

## Progress Log

- 2026-09-12 Phase created (planned).
- 2026-09-12 Added `pkg/validate` request-validation task per plan update.
