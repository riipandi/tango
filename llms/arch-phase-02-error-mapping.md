---
status: planned
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

- [ ] Define in `pkg/responder`: `StatusError` interface (`error` + `HTTPStatus() int` + optional
      `Detail() string`) and a single `responder.WriteError(w, r, err)` that handles:
      `StatusError` → its status, `validate.IsValidationError` → 422 + field errors, fallback 500.
- [ ] Provide a small adapter so wrapped errors keep the status (`fmt.Errorf("%w")` chains still
      satisfy `errors.As`); add `responder.WithStatus(err, code)` helper for one-off mappings.
- [ ] Migrate module sentinels to carry their status (embed the interface in each module's error
      values or wrap at construction, e.g. `store.NewErr(ErrDuplicate, 409)` — pick one pattern,
      apply everywhere).
- [ ] Delete all per-handler `writeError` copies; handlers call `responder.WriteError` only.
- [ ] Spot-check status codes of representative endpoints with curl (404/409/422/401 cases from
      the deviations doc) — responses must stay byte-compatible (same status + message keys).

## Validation

`task test` (all suites — several store tests assert status mapping), `task lint`, `task check`,
curl spot-checks recorded in the progress log.

## Progress Log

- 2026-09-15 Phase planned.
