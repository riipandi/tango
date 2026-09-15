---
status: done
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

- [x] Create `modules/identity/token`: `Purpose` enum (`email_verification`,
      `one_time_access`, plus `reauthentication` matching the DB CHECK), model
      `Token{UserID, TokenHash, ExpiresAt, LastSentAt}`, one `store.go`
      (`NewStore(exec, purpose)` → upsert by `(user_id, purpose)` + single-use `Consume` with the
      expiry check), `service.go` (`NewRaw` 256-bit token + `Hash` SHA-256/base64url), one
      shared `ErrNotFound` sentinel consumers map onto their own messages.
- [x] Rewire `emailverification` and `onetimeaccess` services to the shared store; delete their
      duplicated token stores (both `store.go` files gone). Handlers and endpoints do not change.
      `onetimeaccess` now stores `Token.UserID` in the bare-UUID column form (the shared store's
      canonical form) and resolves it with `user.MustID` on consume.
- [x] `purpose` values are exactly as stored today (`email_verification`, `one_time_access`) —
      old rows verify (live-verified with a hand-inserted old-format row, see below).
- [x] Live checks: admin mint → 201 + row written (purpose/last_sent_at/expiry correct);
      anonymous exchange → 200 + session issued + row consumed (single-use); old-format row
      exchange → 200; `send-email-verification` → 204 with Mailpit delivery; `verify-email` with
      the emailed token → 204, row consumed, `email_verified_at` set.

## Validation

- `task test:go`: 533 pass, 1 skipped, 0 fail. `task test:go:debug`: 35 pass.
  `go test -tags release ./...`: all ok (41 packages). golangci-lint: 0 issues. vet + gofmt clean.
- Live checks recorded above (scratch DB + debug binary + Mailpit; artifacts cleaned up).

## Progress Log

- 2026-09-15 Phase planned.
- 2026-09-15 Implemented. Live-check note: the mint 404s observed mid-session were
  self-inflicted (hand-built hex "TypeID" instead of the base32 TypeID form — `parseUserIDParam`
  correctly rejects it per the AGENTS.md gotcha). With real TypeIDs from the API everything
  passes on both HEAD and the working tree.
