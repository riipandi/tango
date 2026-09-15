---
status: planned
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

- [ ] Add to `internal/kernel`: `type Guard func(http.Handler) http.Handler`, an optional
      `Guarded` interface (`UseGuard(Guard)`), and `type Authenticator` (minimal interface the
      identity modules actually use; keep it smaller than the middleware concrete type).
- [ ] `internal/transport/middleware`: satisfy `kernel.Authenticator` (structural, no wrapper) and
      pass `kernel.Guard` in the registry.
- [ ] Replace all per-module `WithAdminGuard` options with `kernel.Guarded` implementations;
      registry injects the guard into every module that implements the interface.
- [ ] Replace the 5 `RouteGuard` definitions with `kernel.Guard`.
- [ ] Replace `middleware.Authenticator` parameters in identity services with `kernel.Authenticator`.
- [ ] Verify guard-gated routes still mount identically: curl one admin route (401 anonymous, 200
      with admin session) and one machine-guarded route (`X-API-KEY`).

## Validation

`task test` (all suites; registry + kernel tests updated), `task lint`, `task check`, curl checks
recorded in the progress log.

## Progress Log

- 2026-09-15 Phase planned.
