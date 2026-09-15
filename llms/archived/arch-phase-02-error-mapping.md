---
status: done
updated: 2026-09-15
---

# Arch Phase 2 — Central Error→HTTP Mapping

Eight near-identical `writeError` switches map store sentinel errors to HTTP status in handlers.
They have already drifted (different case order and messages) and force every handler to know the
full sentinel list of its store — a handler↔store coupling.

## Evidence

- `writeError` copies: `identity/user/handler.go:269`, `identity/usergroup/handler.go:378`,
  `identity/apikey/handler.go:180`, `identity/apiaccess/handler.go:414`,
  `identity/customclaim/handler.go:296`, `identity/account/handler.go:193`,
  `webhook/handler.go:288` (8th copy among audit/appconfig handlers — grep `func writeError`).
- Mapping is always the same shape: sentinel → status, `validate.IsValidationError` → 422 with
  field errors, fallback 500 via `pkg/responder`.

## Tasks

- [x] Define in `pkg/responder` (`errors.go`): `StatusedError` interface (`error` +
      `HTTPStatus() int`; named to avoid the existing `StatusError` envelope const) and a single
      `responder.WriteError(w, r, err)`: validation → 422 + field errors, statused → its status
      (404 keeps the fixed "not found" message), fallback 500 "internal error".
- [x] Statused sentinels survive `fmt.Errorf("%w")` wrapping via `errors.As` — no extra adapter
      needed; `responder.NewError(status, msg)` is the constructor (the `WithStatus(err, code)`
      variant was unnecessary once sentinels carry status at definition).
- [x] Migrate module sentinels: user (404/409/400/400), usergroup (404/409), webhook
      (404/409/409/413), apiaccess (404/409/422/422), apikey (404/409/409), customclaim
      (404/409), session.ErrNotFound (404), password.ErrWeakPassword (422) +
      ErrInvalidCredentials (400). Sentinels without a writeError case stay plain `errors.New`
      (they must keep the 500 fallback: usergroup.ErrInvalidIDs, apiaccess.ErrInvalidSubject,
      apikey.ErrInvalidCreds).
- [x] Delete the 7 package-level `writeError` copies (user, usergroup, webhook, apiaccess,
      apikey, customclaim, account); call sites use `responder.WriteError`.
- [x] Live spot-checks (scratch DB, debug binary): 201 create / 409 duplicate with sentinel
      message / 404 fixed "not found" / 422 validation + field errors / 400 fixed
      "current password is incorrect" (account override) — all as before the refactor.

## Deliberate deviations

- `signup`'s method form `(s *Service) writeError` stays: it maps the same foreign sentinels to
  different statuses **by design** (user.ErrInvalidUsername/Email → 422 there vs 400 in user;
  404 carries the token message, not the fixed "not found"). Forcing it onto the sentinels would
  change behavior.
- `account` keeps a 5-line `writeError` adapter: the wrong-current-password message
  ("current password is incorrect") is account-specific wording over password.ErrInvalidCredentials
  (status 400 lives on the sentinel). Everything else delegates.

## Validation

- `task test:go`: 532 pass, 1 skipped, 0 fail. `task test:go:debug`: 35 pass.
  `go test -tags release ./...`: all ok. golangci-lint: 0 issues. `task check` (vet + format):
  clean. Live curl checks recorded above.
- `task test:ui` / oxlint still fail pre-existing (no `api/**/*.test.ts`; `oxlint-tsgolint`
  undeclared) — see phase 1.

## Progress Log

- 2026-09-15 Phase planned.
- 2026-09-15 Implemented. Found a pre-existing anomaly while spot-checking: on `/api/users` the
  machine-auth mount answers before the admin mount even with a valid admin session (401 "API
  key required"; identical on a baseline binary built from HEAD). The two mounts register the
  same paths — first match wins. Guard wiring gets unified in arch phase 3; flagged there.
