---
status: planned
updated: 2026-09-12
---

# Phase 1 — Auth Core

Session-cookie auth middleware, session/password/account features. Unblocks every admin and user
endpoint. Pocket ID reference: `backend/internal/middleware/auth*.go`, `backend/internal/service/*_service.go`.

## Goal

A signed-in browser session (cookie) and machine guards (API key) protect `/api` endpoints; users
can sign in with username/password and manage their account.

## Deliverables

- `internal/transport/middleware/auth.go` — guard chain: session cookie (browser), `X-API-KEY`
  (machines), injected per-route from the registry, not a global.
- `modules/identity/session` — real store/service/handler: issue (opaque token, hashed at rest),
  validate, refresh, revoke; table `sessions`.
- `modules/identity/password` — argon2id hashing via `pkg/crypto`, login throttling; table
  `user_passwords`.
- `modules/identity/account` — self-service: profile, change password, active sessions list/revoke.
- `pkg/validate` — ozzo-validation adapter: jsonv2 decode+`Validate()` helper for handlers,
  `validation.Errors` → field-error mapping.
- Wire features in `internal/registry`; protect admin routes as a smoke test.

## Tasks

- [ ] Typed IDs: `session.SessionID`, `password.PasswordID` in module `schema.go`.
- [ ] `pkg/validate` — ozzo-validation adapter: decode+`Validate()` helper, 422 `validation_failed`
      mapping; first consumers are the account/password handlers below.
- [ ] `session/store.go` — sqlbuilder CRUD over `sessions` (find by selector, delete by user).
- [ ] `session/service.go` — issue/validate/refresh/revoke; expiry from app config; cookie helpers
      (HttpOnly, Secure, SameSite=Lax).
- [ ] `password/service.go` — argon2id hash/verify (`pkg/crypto.Password`), login rate guard.
- [ ] `account` handler/service/store — profile GET/PATCH, password change, session revoke.
- [ ] `internal/transport/middleware/auth.go` — `RequireUser`, `RequireAdmin`, `RequireAPIKey`;
      request-scoped principal in context (typed key).
- [ ] Registry wiring: replace `session.Feature{}`/`password.Feature{}` placeholders with real
      constructors; mount guards on existing admin-capable routes.
- [ ] Tests: store tests (testcontainers), service tests, middleware tests with `httptest`;
      `httptest` cookie round-trip for session flow.

## Validation

- Sign-in → cookie → authenticated request → sign-out round trip passes under `-race`.
- Three standard suites + lint + gofmt clean.

## Progress Log

- 2026-09-12 Phase created (planned).
- 2026-09-12 Added `pkg/validate` (ozzo-validation) deliverable + task per plan update.
