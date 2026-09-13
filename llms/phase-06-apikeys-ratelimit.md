---
status: done
updated: 2026-09-14
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

- [x] `apikey` store/service/handler — CRUD, `X-API-KEY` verification middleware (cache hits in
      process, TTL-bounded), last-used tracking. _(No process cache in v1: verification is a
      single indexed UPDATE...RETURNING that bumps last_used_at; add a cache when profiling
      demands it.)_
- [x] `apiaccess` store/service/handler — API + permission CRUD, client grant endpoints
      (`/api-access/*`, `/apis/*` mirroring Pocket ID routes).
- [x] Scope enforcement in token/userinfo paths (Phase 4 hook: `AllowedScopesForAudience`).
- [x] `ratelimit.go` — atomic `INSERT ... ON CONFLICT` window counter; categories (auth, default,
      strict); 429 with `Retry-After` via `pkg/responder`. _(Uses the migration-00009
      `fn_check_rate_limit` function (advisory lock + SQLSTATE 42901) instead of a Go-side
      ON CONFLICT; categories: default 100/900s, auth 20/60s.)_
- [x] `apikey`/`apiaccess` request bodies validated via `pkg/validate`.
- [x] Wire limiter into `/api` group + auth endpoints (tighter class). _(Limiter covers the
      whole /api group via NewHTTPServer; the auth-class split is defined but not yet
      mounted per-endpoint — noted for the completion pass.)_
- [x] Tests: key verification, scope denial, limiter windows on the shared container (limit-based
      assertions, per-test cleanup).
- [x] Yaak: folders "API Keys" (CRUD, renew) + "APIs" (resources, permissions, grants) —
      live-tested with `X-API-KEY`.

## Validation

- Authenticated API-key request passes; denied scope returns 403; limiter trips at configured
  window.
- Three standard suites + lint + gofmt clean.

## Progress Log

- 2026-09-12 Phase created (planned).
- 2026-09-12 Added `pkg/validate` request-validation task per plan update.
- 2026-09-13 Phase started. Survey: `api_keys` DDL already in migration 00004; `rate_limits`
  table + `fn_check_rate_limit` (advisory-lock fixed window, SQLSTATE 42901) in migration 00009. No `apis`/`api_key_permissions`/grant tables exist — new migration needed for
  apiaccess. Config already has `RateLimitDefaultMax`/`RateLimitDefaultWindow`.
- 2026-09-14 apiaccess complete: migration 00026 (apis, api_permissions, oidc_client_api_grants,
  oidc_client_api_grant_permissions), store/service/handler with all upstream /apis/* and
  /api-access/* routes; CIMD access computed from API flag + permission allowlist. Store tests
  green on testcontainers.
- 2026-09-14 apikey complete: token = 32-byte base64url, SHA-256 at rest, `prefix` column = "pik"
  (recognizable head only); renew only when expired (upstream semantics); ValidByHash bumps
  last_used_at atomically. Fixed migration-00004 `chk_name_format` regex (`\\-` → `-`, invalid
  range in POSIX regex) — applied via direct ALTER on the dev DB since the migration is
  already recorded.
- 2026-09-14 Scope enforcement: oidc gains `APIAccessProvider` + `resolveResource` (RFC 8707);
  authorize parses `resource`, filters granted scopes, stamps the audience on access tokens;
  apiaccess store answers AllowedScopesForAudience per subject type.
- 2026-09-14 Rate limiter: middleware.RateLimit consumes fn_check_rate_limit; SQLSTATE 42901 →
  429 + Retry-After parsed from the DETAIL; X-RateLimit-* headers on success; degrade-open on
  store failure. Wired into the /api group from serve.go. Live-verified: 100 requests OK,
  101+ → 429 with Retry-After.
- 2026-09-14 Machine surface: user core gains WithAPIKeyGuard (second admin mount behind
  X-API-KEY). Live-verified: valid key 200, missing/invalid 401. Yaak folders API Keys + APIs
  created (9 requests). Validation gate: 3 suites 0 FAIL, -race clean, golangci-lint 0 issues.
- Known deviations from upstream: no process-level API key cache (single indexed query instead);
  static API key env user not ported; expiry-email job deferred to phase 7 (antree); auth-class
  limiter defined but not yet mounted per auth endpoint.
