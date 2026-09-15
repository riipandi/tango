---
status: planned
updated: 2026-09-15
---

# Arch Phase 1 — Mechanical Dedup

Zero-risk, purely mechanical consolidation. No signatures change at call sites beyond the import
path. Do this phase first so later phases build on shared helpers.

## Evidence

- `userUUID(raw string) string` — 5 identical copies: `federation/oidc/store.go:465`,
  `identity/apikey/store.go:202`, `identity/emailverification/store.go:112`,
  `identity/onetimeaccess/store.go:125`, `identity/devicelogin/store.go:160`.
- `timePtr(pgtype.Timestamptz)` — 6 copies (user, usergroup, apiaccess, apikey, session, password).
- Scattered `ptr`/`deref`/`derefText`/`ptrTime` variants (~10 definitions, same purpose).
- `pgx.ErrNoRows` → sentinel mapping repeated ~34× across 14 stores; `mapErr`/`wrapErr` duplicated
  in 8+ stores, differing only by prefix string.
- `apikey/service.go:137` hardcodes `r.Cookie("tango_session")` while
  `identity/session/cookie.go:10` defines `CookieName = "tango_session"`.
- `pkg/antree` (1.250 LOC, private port of Backlite) lives in `pkg/` but is only consumed by
  `internal/queue` + `internal/jobs` — `pkg/` implies reusable; it is not.

## Tasks

- [ ] Create `internal/datastore/conv` (or extend `internal/datastore`): `UserUUID(raw string)`,
      `TimePtr(pgtype.Timestamptz)`, generic `Ptr[T]`, `Deref[T]`, and the pgtype text/bool/int
      deref variants. Replace all duplicate definitions.
- [ ] Add `datastore.MapNoRows(err, sentinel error) error` and `datastore.Wrap(op string, err error)
      error`; replace the per-store `mapErr`/`wrapErr` bodies (module prefix may move into the op
      string).
- [ ] `apikey/service.go`: use `session.CookieName` instead of the string literal.
- [ ] Move `pkg/antree` → `internal/antree`; update imports in `internal/queue`, `internal/jobs`,
      `internal/logger`, `internal/registry`.
- [ ] Run the full gate; confirm no behavior change (`task test`, `task lint`, `task check`).

## Validation

`task test` (0 FAIL across all three suites), `task lint` (0 issues), `task check` (clean).

## Progress Log

- 2026-09-15 Phase planned.
