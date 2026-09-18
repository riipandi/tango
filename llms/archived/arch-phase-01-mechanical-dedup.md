---
status: done
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

- [x] Create `internal/datastore/conv` (or extend `internal/datastore`): `UserUUID(raw string)`,
      `TimePtr(pgtype.Timestamptz)`, generic `Ptr[T]`, `Deref[T]`, and the pgtype text/bool/int
      deref variants. Replace all duplicate definitions.
- [x] Add `datastore.MapNoRows(err, sentinel error) error` and `datastore.Wrap(op string, err error)
      error`; replace the per-store `mapErr`/`wrapErr` bodies (module prefix may move into the op
      string).
- [x] `apikey/service.go`: use `session.CookieName` instead of the string literal.
- [x] Move `pkg/antree` → `internal/antree`; update imports in `internal/queue`, `internal/jobs`,
      `internal/logger`, `internal/registry`.
- [x] Run the full gate; confirm no behavior change (`task test`, `task lint`, `task check`).

## Validation

- `task test:go`: 572 pass, 0 fail. `task test:go:debug`: 35 pass, 1 skipped.
  `go test -tags release ./...`: 0 fail. `golangci-lint`: 0 issues. `gofmt`: clean.
- `task test:ui` and `task check` fail in this environment for pre-existing JS-tooling reasons,
  independent of this phase (vitest finds zero `api/**/*.test.ts` files; oxlint needs the
  undeclared `oxlint-tsgolint` dependency for `typeAware: true`). Tracked as tooling fixes, not
  phase-1 scope.

## Progress Log

- 2026-09-15 Phase planned.
- 2026-09-15 Implemented: `datastore.MapErr(err, store, notFound, duplicate)` replaces the
  separate `MapNoRows` idea (one helper covers no-rows + unique-violation + wrap); 6 `mapErr`
  bodies and apikey's `wrapErr` delegate to it. Inline `ErrNoRows` scan sites keep their
  per-callsite sentinels. Helper renames: `userUUID` → `datastore.UserUUID` (5 defs), `timePtr` →
  `datastore.TimePtr` (6 defs), `textPtr` → `datastore.TextPtr` (2), `ptr`/`ptrTime` →
  `datastore.Ptr`, `deref`/`derefText`/`derefBool`/`derefInt` → `datastore.Deref`. Note:
  `internal/storage` `deref` kept (trims "/" — different semantics). `pkg/antree` moved to
  `internal/antree` incl. README + root README + porting-guide references.
