---
status: done
updated: 2026-09-15
---

# Arch Phase 3 — Kernel Guard & Auth Contracts

`WithAdminGuard` is duplicated 11× in 3 different shapes, `RouteGuard` is defined 5× in identity
services, and services take `middleware.Authenticator` from `internal/transport/middleware` —
modules importing the HTTP transport layer. The kernel contract is missing one type.

## Evidence

- `WithAdminGuard` shapes: `func(http.Handler) http.Handler` on Feature/Module (signup, webauthn,
  onetimeaccess, appconfig, oidc, webhook), `WithAdminGuard(g RouteGuard) ServiceOption` (user,
  usergroup, apikey, apiaccess, customclaim), `guard chi.Router` variant (`ldapsync/handler.go:22`).
- `type RouteGuard func(next http.Handler) http.Handler` defined in `user`, `usergroup`, `apikey`,
  `apiaccess`, `customclaim` service.go files.
- `identity/user/service.go:50` (and webauthn, apikey) accept `middleware.Authenticator` — a
  transport package — inside `modules/identity/*`.

## Tasks

- [x] Add to `internal/kernel` (`auth.go`): `type Guard func(http.Handler) http.Handler`, a
      `Guarded` interface (`UseGuard(Guard)`), `type Authenticator`, and `Principal`.
- [x] `internal/transport/middleware`: `Principal`/`Authenticator` are now type aliases of the
      kernel contracts — structural satisfaction, no wrapper, single definition.
- [x] Replace the 5 per-service `RouteGuard` definitions (user, usergroup, apikey, apiaccess,
      customclaim) with `kernel.Guard`.
- [x] Pointer-backed holders implement `kernel.Guarded` with void `UseGuard`: webhook.Module,
      appconfig.Module, appimage.Service (guard moved from a package-level mutable var into the
      struct — the global side-effect wiring is gone), ldapsync.APIFeature. Registry switches
      those call sites to `UseGuard`.
- [x] Replace `middleware.Authenticator`/`middleware.Principal` in identity/federation/auditlog
      services with the kernel types (aliases — no behavior change).
- [x] Guard-gated routes verified live: `/api/users` 200 with admin session (see fix below),
      401 anonymous; X-API-KEY 200 valid / 401 invalid; appimage mutation 401 anonymous;
      sync-ldap 401 anonymous / 409 with admin (LDAP disabled) — see fix below.

## Fixes found during unification (pre-existing bugs, verified against HEAD)

- **`/api/users` machine mount shadowed the admin mount.** chi silently lets the LAST
  registration of an identical pattern win, so the `X-API-KEY` group answered every request —
  admin sessions got 401 "API key required". Fixed: one mount behind `eitherGuard`
  (session-admin path, `X-API-KEY` header routes to the machine guard), matching the documented
  "accept either guard" intent.
- **`POST /api/application-configuration/sync-ldap` was anonymous-reachable.** ldapsync was
  registered with a nil guard that the "caller below" never wired — fail-open against the
  AGENTS.md fail-closed rule. Fixed: registry wires `adminAuth`; the feature skips mounting
  entirely when no guard is present.

## Deliberate deviation

Value-type features (signup, webauthn, onetimeaccess, oidc) keep their fluent
`WithAdminGuard(kernel.Guard)` builders: `UseGuard` has a pointer receiver, and the stored
`identity.Feature` values cannot expose it (pointer-receiver method sets on copies). The options
are now typed with the single `kernel.Guard` — the 5 duplicate type definitions are gone; the
builder shape stays.

## Validation

- `task test:go`: 533 pass, 1 skipped, 0 fail. `task test:go:debug`: 35 pass.
  `go test -tags release ./...`: all ok. golangci-lint: 0 issues. vet + gofmt clean.
- Live curl checks recorded above (scratch DB + debug binary; artifacts cleaned up).

## Progress Log

- 2026-09-15 Phase planned.
- 2026-09-15 Implemented; two pre-existing guard bugs fixed (machine-mount shadowing, ldapsync
  fail-open) and documented above.
