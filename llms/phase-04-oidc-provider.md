---
status: done
updated: 2026-09-13
---

# Phase 4 — OIDC Provider

The core Pocket ID surface: authorization code flow with PKCE, token issuance, userinfo. Pocket ID
reference: `backend/internal/oidc/`, `controller/oidc_controller.go`.

## Goal

A relying party can redirect a user to `/authorize`, complete sign-in (Phase 1 session), receive a
code, exchange it for tokens, and read userinfo.

## Deliverables

- `modules/federation/oidc` — real implementation (module split: `handler.go` owns every HTTP
  surface; logic in area files; all stores in `store.go`):
    - `authorize.go` — `/authorize` (root router): client validation, redirect-pattern match
      (exact or trailing-`*` wildcard), PKCE S256 only (plain rejected, 400 before the
      redirect_uri is ever trusted), code issue (one-time, 2-minute TTL, SHA-256 at rest).
    - `token.go` — `/api/oidc/token`: client auth (Basic/form, constant-time secret-hash
      compare, public clients by ID), `authorization_code` + `refresh_token` grants, RS256
      access + ID tokens via the Phase 3 KeyProvider (sub/aud/azp/nonce/sid/auth_time/groups/
      custom claims).
    - Refresh rotation — per-use rotation inside an `oauth2_sessions` family
      (`request_id`); reuse of a rotated token deactivates the whole family
      (`oauth2_jtis` replay registry).
    - `userinfo.go` — Bearer introspection against the published JWKS; scope-filtered claims
      (email/profile/groups/custom), RFC 6750 `WWW-Authenticate` errors.
    - `client.go` — client CRUD with group allowlists (`oidc_clients_allowed_user_groups`),
      hashed secrets (raw shown once at create), group-restricted clients.
    - `interaction_sessions` — SPA bridge: `/authorize` parks unauthenticated or
      consent-pending requests; `GET /api/oidc/interaction/{id}` + `POST …/approve` resume.
- Registry: `oidc.Service` wired with the shared key provider, session authenticator, audit
  adapter; client management behind the admin guard (fail closed — routes skip mounting when
  the guard is absent).

## Tasks

- [x] `client.go` store/service — `oidc_clients` CRUD + `oidc_clients_allowed_user_groups` grants.
- [x] `authorize.go` — request validation (response_type, scope, redirect, PKCE), code issue into
      `oidc_authorization_codes`, interaction redirect when no session.
- [x] `token.go` — grant types: `authorization_code`, `refresh_token`; ID token claims (iss, sub,
      aud, azp, nonce, sid, groups, custom claims); `oauth2_sessions` bookkeeping (kinds:
      authorize_code / access_token / refresh_token).
- [x] Refresh rotation — new refresh token per use, `oauth2_jtis` replay check, revoke family on
      reuse detection.
- [x] `userinfo.go` — token introspection of claims; 401 mapping via `pkg/responder`.
- [x] Interaction session store — bridge for the SPA sign-in/consent flow.
- [x] Validate client DTOs via `pkg/validate` (ozzo: name + http(s) callback URLs).
- [x] E2E handler test: authorize (cookie session) → code → token → userinfo, against
      testcontainers Postgres; plus refresh-reuse revocation and one-time code replay.
- [x] Yaak: folders "OIDC" (clients, authorized clients) + "OAuth" (token, userinfo) —
      live-tested incl. refresh-reuse revocation (see progress log).

## Validation

- E2E flow test passes under `-race`; refresh reuse revokes the family.
- Three standard suites + lint + gofmt clean.

## Progress Log

- 2026-09-12 Phase created (planned); `pkg/validate` task added per plan update.
- 2026-09-13 Phase started; survey: full `oidc_*`/`oauth2_*`/`interaction_sessions` DDL (00006),
  oidc stubs, identity claim readers (`ListByUser`/`ListByGroup`, `GroupIDsForUser`).
- 2026-09-13 Landed per the user-module split (handlers in `handler.go`, logic per area file,
  one `store.go`). Protocol details settled by live failures: codes + refresh tokens + client
  secrets stored as SHA-256; sub claim normalized to the UUID form (typeid strings are not
  valid UUID columns — `userUUID()` normalizes at the store boundary); the
  `oidc_authorization_codes` table has no redirect_uri column, so the authorize context
  (redirect_uri + sid) parks in an `authorize_code` oauth2_sessions row; mount paths are
  relative to the `/api` group (absolute paths double the prefix). Chi panics on a nil
  middleware — client routes mount only when the admin guard exists. Registry: token + key
  issuance verified end to end (JS-decoded ID token carries iss/aud/azp/nonce/sid + profile).
- 2026-09-13 Live verification on :3080 (curl + Yaak): admin sign-in → client create 201
  (secret returned once, `has_secret` true) → `/authorize` 302 → interaction approve 200 →
  token exchange 200 (access/refresh/ID tokens, expires_in 3600) → userinfo 200 (scoped
  claims) → refresh rotation 200 → replay of the rotated token 401 `invalid_grant` → the
  surviving family token is revoked too (401). Yaak folders: OIDC (`fl_GfrmzYcuVx`: GET
  clients `rq_6n9EEpfm3D`, GET authorized-clients `rq_ec8TojUzzD`) + OAuth (`fl_wq663t6sTL`:
  userinfo `rq_7MXS5UmwqW` — 401 with a placeholder token proves the RFC 6750 error path;
  token `rq_UMpYMGevsB` — POST body filled manually in the UI, MCP harness double-escapes
  form bodies).
