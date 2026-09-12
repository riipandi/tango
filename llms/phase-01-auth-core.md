---
status: done
updated: 2026-09-12
---

# Phase 1 — Auth Core

Session-cookie auth middleware, session/password/account features. Unblocks every admin and user
endpoint. Pocket ID reference: `backend/internal/middleware/auth*.go`, `backend/internal/service/*_service.go`.

## Goal

A signed-in browser session (cookie) and machine guards (API key) protect `/api` endpoints; users
can sign in with username/password and manage their account.

## Deliverables

- `internal/transport/middleware/auth.go` — guard chain: `RequireAuth` (session cookie, principal
  in context), `RequireAdmin`, `RequireAPIKey` (function adapter, wired in phase 6); the
  middleware imports no modules — resolvers are injected.
- `modules/identity/session` — real store/service/handler: opaque 256-bit tokens, only SHA-256
  hash stored, sliding expiry (refresh past half-life), revoke by token / by ID (owner-checked) /
  all-for-user; cookie helpers (HttpOnly, SameSite=Lax, Secure by mode); routes
  `POST /api/auth/sign-in`, `POST /api/auth/sign-out`, `GET /api/auth/session`.
- `modules/identity/password` — argon2id hashing via `pkg/crypto`, identity lookup (username or
  email, single join query), verify-for-user for sensitive ops; headless feature.
- `modules/identity/account` — self-service under the guard: profile GET/PATCH, password change
  (verifies current, revokes other sessions), session list (token-hash-free view) + revoke.
- `pkg/validate` — ozzo-validation adapter: jsonv2 decode + `Validate()` contract, field-error
  mapping, 422 envelope.
- Wire features in `internal/registry`; user core admin routes behind `RequireAuth` + `RequireAdmin`.

## Tasks

- [x] Typed IDs: `session.SessionID` in module `schema.go` (no `password.PasswordID`: the table
      PK is `user_id`).
- [x] `pkg/validate` — ozzo-validation adapter: decode+`Validate()` helper, 422 `validation_failed`
      mapping; first consumers are the account/password handlers below.
- [x] `session/store.go` — sqlbuilder CRUD over `sessions` (token-hash lookup joins `users`).
- [x] `session/service.go` — issue/validate/revoke; lifetime from `AUTH_SESSION_LIFETIME`
      (default 30 days); cookie helpers.
- [x] `password/service.go` — argon2id hash/verify (`pkg/crypto.PasswordHasher`).
- [x] `account` handler/service — profile GET/PATCH, password change, session revoke.
- [x] `internal/transport/middleware/auth.go` — `RequireAuth`, `RequireAdmin`, `RequireAPIKey`;
      request-scoped principal (typed key).
- [x] Registry wiring: `password`/`session`/`account` real constructors replace placeholders;
      admin `/api/users` routes guarded.
- [x] Tests: store tests (testcontainers), service tests, middleware tests with `httptest`;
      sign-in → cookie → session → sign-out round trip.

## Validation

- Sign-in → cookie → authenticated request → sign-out round trip passes under `-race`.
- Three standard suites + lint + gofmt clean.

## Progress Log

- 2026-09-12 Phase created (planned).
- 2026-09-12 Added `pkg/validate` (ozzo-validation) deliverable + task per plan update.
- 2026-09-12 Phase started. Note: `user_passwords` has no ID column (PK = user_id), so no
  `password.PasswordID` typed ID — the planned one would not map to the schema. Login throttling
  defers to the phase-6 rate limiter.
- 2026-09-12 Auth middleware + session/password/account features landed; user core admin routes
  guarded (anonymous `/api/users` now 401 — lifecycle probe moved to `/api/healthz`).
  Validated: 3 suites + `-race` 0 FAIL, golangci-lint 0 issues, gofmt clean.
