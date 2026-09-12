---
status: planned
updated: 2026-09-12
---

# Phase 6 — API Keys, Resource APIs, Rate Limiter

Machine-to-machine surface: `X-API-KEY` guards, Pocket ID "APIs" (resource servers with scopes),
and request rate limiting. Pocket ID reference: `backend/internal/apikey/`, `api/` module,
`middleware/rate_limit*.go`.

## Goal

Third-party services authenticate to the admin API with API keys; OIDC clients get audience/scope
pairs for resource APIs; abusive clients get throttled.

## Deliverables

- `modules/identity/apikey` — key generation (`pik_` prefix, shown once), expiry, description;
  table `api_keys`; hash at rest via `pkg/crypto`.
- `modules/identity/apiaccess` — "APIs" resource registry: permissions/scopes per API, client
  grants, CIMD access flag; middleware resolves `aud` + scope → allow/deny.
- `internal/transport/middleware/ratelimit.go` — fixed-window limiter backed by `rate_limits`
  table (multi-instance safe), keyed by IP + route class; config via env.

## Tasks

- [ ] `apikey` store/service/handler — CRUD, `X-API-KEY` verification middleware (cache hits in
      process, TTL-bounded), last-used tracking.
- [ ] `apiaccess` store/service/handler — API + permission CRUD, client grant endpoints
      (`/api-access/*`, `/apis/*` mirroring Pocket ID routes).
- [ ] Scope enforcement in token/userinfo paths (Phase 4 hook: `AllowedScopesForAudience`).
- [ ] `ratelimit.go` — atomic `INSERT ... ON CONFLICT` window counter; categories (auth, default,
      strict); 429 with `Retry-After` via `pkg/responder`.
- [ ] Wire limiter into `/api` group + auth endpoints (tighter class).
- [ ] Tests: key verification, scope denial, limiter windows on the shared container (limit-based
      assertions, per-test cleanup).

## Validation

- Authenticated API-key request passes; denied scope returns 403; limiter trips at configured
  window.
- Three standard suites + lint + gofmt clean.

## Progress Log

- 2026-09-12 Phase created (planned).
