---
status: planned
updated: 2026-09-12
---

# Phase 4 — OIDC Provider

The core Pocket ID surface: authorization code flow with PKCE, token issuance, userinfo. Pocket ID
reference: `backend/internal/oidc/`, `controller/oidc_controller.go`.

## Goal

A relying party can redirect a user to `/authorize`, complete sign-in (Phase 1 session), receive a
code, exchange it for tokens, and read userinfo.

## Deliverables

- `modules/federation/oidc` — real implementation replacing placeholders:
    - `authorize.go` — `/authorize` (root router): client validation, PKCE (S256 required, plain
      rejected), consent/interaction hand-off, code issue (one-time, short TTL).
    - `token.go` — `/api/oidc/token`: code exchange, refresh token rotation with `oauth2_jtis`
      reuse detection, access tokens signed via Phase 3 KeyProvider.
    - `userinfo.go` — `/api/oidc/userinfo`: Bearer access token, claims incl. custom claims
      (Phase 2) and group claims.
    - `client.go` — OIDC client CRUD: secrets hashed, redirect URI patterns with wildcards
      (callback-url-wildcards doc), public clients (no secret, PKCE only).
- `interaction_sessions` flow — minimal consent/interaction state machine for the SPA.

## Tasks

- [ ] `client.go` store/service — `oidc_clients` CRUD + `oidc_clients_allowed_user_groups` grants.
- [ ] `authorize.go` — request validation (response_type, scope, redirect, PKCE), code issue into
      `oidc_authorization_codes`, login redirect when no session.
- [ ] `token.go` — grant types: `authorization_code`, `refresh_token`; ID token claims (iss, sub,
      aud, azp, nonce, sid, groups, custom claims); `oauth2_sessions` bookkeeping.
- [ ] Refresh rotation — new refresh token per use, `oauth2_jtis` family check, revoke family on
      reuse detection.
- [ ] `userinfo.go` — token introspection of claims; 401/403 mapping via `pkg/responder`.
- [ ] Interaction session store — bridge table for the SPA sign-in flow.
- [ ] Validate authorize/token inputs via `pkg/validate` (redirect URI shape, PKCE pair, scopes).
- [ ] End-to-end handler test: discover → authorize (cookie session) → code → token → userinfo,
      against testcontainers Postgres.
- [ ] Yaak: folders "OIDC" (client CRUD, authorized clients) + "OAuth" (token, userinfo) —
      live-tested incl. refresh-reuse revocation.

## Validation

- E2E flow test passes under `-race`; refresh reuse revokes the family.
- Three standard suites + lint + gofmt clean.

## Progress Log

- 2026-09-12 Phase created (planned).
- 2026-09-12 Added `pkg/validate` request-validation task per plan update.
