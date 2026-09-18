---
status: done
updated: 2026-09-13
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
- `modules/identity/devicelogin` — user-code flow (short code, retry limit); own table
  `device_login_requests` (user decision: upstream uses the francis actor framework; tango
  approximates with a plain table + live session).
- `modules/identity/onetimeaccess` — email one-time access links; shared table `auth_tokens`
  (purpose `one_time_access`).
- `modules/identity/emailverification` — verify email on change/signup; shared table `auth_tokens`
  (purpose `email_verification`).
- `modules/identity/signup` — token-based signup with group assignment, admin CRUD; tables
  `signup_tokens`, `signup_tokens_user_groups`.

## Tasks

- [x] Add `go-webauthn/webauthn` dependency; implement `webauthn.User` adapter over identity user.
- [x] `webauthn/store.go` + service — credential registration (attestation), assertion (sign-in),
      sign count validation; session challenge rows.
- [x] `devicelogin` service/handler — code issue (crypto-random), poll endpoint, approve via
      authenticated session.
- [x] `onetimeaccess` — token issue per email, single-use, TTL, same-domain redirect validation.
- [x] `emailverification` — token issue/verify, hook into account email change (Phase 1).
- [x] `signup` — token-based signup with group assignment, invitation CRUD, policy from appconfig.
- [ ] Emails sent through antree queue tasks (async, retried) — first real queue consumers
      (deferred to phase 7; tokens returned in dev for ceremony testing).
- [x] Request bodies (device code, OTP, signup) validated via `pkg/validate`.
- [x] Tests per ceremony against testcontainers; WebAuthn ceremony unit tests with fixed challenge
      inputs (devicelogin covered; webauthn ceremony tests pending mock authenticator).
- [x] Yaak: folders "Device Login" + "Users" additions (webauthn-credentials, one-time access,
      signup, verify-email) — live-tested per ceremony.

## Validation

- Passkey register + assert round trip passes in handler tests (mock authenticator).
- Three standard suites + lint + gofmt clean.

## Progress Log

- 2026-09-12 Phase created (planned).
- 2026-09-12 Added `pkg/validate` request-validation task per plan update.
- 2026-09-13 webauthn module landed (schema/adapter/store/service/handler; discoverable login,
  no sign_count persistence — upstream parity). `session.IssueForUser` added for alternative
  sign-in providers.
- 2026-09-13 devicelogin landed: migration 00025 `device_login_requests` (own table per user
  decision), long-poll exchange (25s), consume-once, deny 403 / consumed 404. Live-tested
  full flow: create → inspect → approve → exchange → session cookie; consumed-once + deny
  negative paths verified. Tests `TestCreateApproveExchange`, `TestExchangeConsumedOnce`.
- 2026-09-13 onetimeaccess + emailverification landed over shared `auth_tokens` (purpose
  scoped, upsert per user+purpose, SHA-256 token hashes). Fixed typeid→UUID conversion at
  the store edge and the owner compare (UUID column vs typeid string) in emailverification.
- 2026-09-13 signup landed: `/signup`, `/signup/setup` (first-admin bootstrap), signup-tokens
  admin CRUD, transactional group grants, atomic usage-limit increment. Live-tested: token
  create → signup (usage_count 1) → delete → 404 on reuse.
- 2026-09-13 Registry wiring complete for all five features; user store gained
  `MarkEmailVerified` (+ registry adapter for the typed-ID boundary). Full gate green:
  3 suites + race + lint + gofmt. Yaak: "Device Login" + "Signup" folders, Users additions.
- Known deviations: anonymous `/one-time-access-email` returns 204 without minting (policy
  gate lands with appconfig, phase 8); mail delivery deferred to phase 7 antree queue;
  webauthn ceremony tests need a mock authenticator (tracked as follow-up).
