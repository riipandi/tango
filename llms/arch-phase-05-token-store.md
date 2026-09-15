---
status: planned
updated: 2026-09-15
---

# Arch Phase 5 — Token Store Consolidation

Two modules each carry a near-identical store for the same physical table
(`public.auth_tokens`): upsert by `(user_id, purpose)`, SHA-256 hash lookup, expiry check, delete
on consume. The domain is one (one-shot token per user + purpose); the code is duplicated.

## Evidence

- `identity/emailverification/store.go` and `identity/onetimeaccess/store.go`: both define
  `authTokensTable = "public.auth_tokens"`, mirrored `Token` models, `scanToken`, upsert
  `ON CONFLICT (user_id, purpose)`, and `ErrNoRows` mapping.
- `signup` tokens and `devicelogin` requests use different tables — out of scope here.
- Known boundary from `llms/README.md` gotchas: `userUUID()` at the store edge, compare
  `userID.UUID()` (not the typeid string) — the consolidated store must keep both.

## Tasks

- [ ] Create `modules/identity/token`: model `Token{UserID, Purpose, TokenHash, ExpiresAt}`,
      purpose enum values for email-verification and one-time-access, one `store.go`, one
      `service.go` (mint/verify/consume helpers with the expiry + single-use policy).
- [ ] Rewire `emailverification` and `onetimeaccess` services to the shared store; delete their
      duplicated token stores. Handlers and endpoints do not change.
- [ ] Keep the `purpose` values exactly as stored today (existing rows must stay verifiable).
- [ ] Re-run the existing token tests plus a live check: request a one-time-access email and an
      email verification against the running server, exchange the token — old rows in the DB
      must still verify.

## Validation

`task test` (all suites), `task lint`, `task check`, live token exchange recorded in the
progress log.

## Progress Log

- 2026-09-15 Phase planned.
