---
status: done
updated: 2026-09-13
---

# Phase 3 — JWKS & Well Known

Signing-key lifecycle (generation, encrypted storage, rotation with overlap) and the RFC 8615
discovery endpoints. Pocket ID reference: `backend/internal/bootstrap` key bootstrap and
`backend/internal/handler/well_known.go`.

## Goal

The application signs OIDC tokens with a rotating RSA key pair whose private half is encrypted
at rest, and publishes its public keys plus the discovery document on the well-known endpoints.

## Deliverables

- `pkg/jwtutils` — `KeyProvider` contract (`SignKey`/`VerifyKeySet`) + `CachedKeyProvider`
  (TTL cache, failure-uncached, `Invalidate`); synctest-verified.
- `modules/federation/jwks` — key generation (RS256 default, ES256 ready), Postgres store,
  AES-256-GCM-encrypted private halves (digest of `auth.secret_key`), rotation keeping the
  retired key published for a 24h overlap so pre-rotation tokens stay verifiable; the service
  is a startable feature — registry startup guarantees a signing key exists before bind.
- `modules/federation/discovery` — real handlers: `GET /.well-known/jwks.json` (bare JWKS
  document, `Cache-Control: public, max-age=300`, no envelope) and
  `GET /.well-known/openid-configuration` (issuer = `PUBLIC_BASE_URL`; advertise the
  authorize/token/userinfo/end-session paths phase 4 implements).
- Registry wiring: `jwks.Service` registered as a startable federation feature; discovery
  mounts the TTL-cached provider.

## Tasks

- [x] `KeyProvider` in `pkg/jwtutils` — `SignKey`, `VerifyKeySet`, cache + invalidation.
- [x] Key generation: RSA 2048 default (ES256/P-256 supported); `kid` = typed `jwk_` ID,
      minted once and stable across restarts.
- [x] `jwks` store — insert, active signing key, published set, retire-with-overlap; private
      column encrypted via `pkg/crypto`.
- [x] Rotation: new key signs, previous stays in JWKS until the overlap passes.
- [x] `discovery` handlers — real JWKS + discovery document; module re-registered in
      `internal/registry`.
- [x] Tests: kid uniqueness, rotation overlap, JWKS shape (no private material), sign→verify
      round-trip through the published set, discovery document snapshot, cache TTL/invalidate.
- [x] Yaak: folder "Well Known" — live-tested on :3080 (see progress log).

## Validation

- Three suites + `-race` + lint + gofmt clean; new endpoints covered with a real Postgres
  container and live-verified against the running server.

## Progress Log

- 2026-09-12 Phase created (planned).
- 2026-09-13 Phase started.
- 2026-09-13 Landed: KeyProvider + cache, jwks module (keygen/store/service with encrypted
  private halves and rotation overlap), real discovery handlers, registry wiring. Live checks
  exposed that the `jwks` DDL lives inside migration 00006 which had already been applied —
  `db migrate:reset --up --force` rebuilt the database cleanly; a second wiring bug (key
  service missing from the feature list, so `Start` never ran) fixed by registering it as a
  startable federation feature. Validated: 3 suites + `-race` 0 FAIL, golangci-lint 0 issues,
  gofmt clean.
- 2026-09-13 Yaak live verification on :3080: folder "Well Known" (`fl_YNuU3gaJNQ`) with
  `GET /.well-known/openid-configuration` 200 (`rq_uKvVKhb83C`) and `GET /.well-known/jwks.json`
  200 with `Cache-Control: public, max-age=300` (`rq_Jn8KwRJ5KV`); JWKS serves the generated
  RS256 key (`kid` = `jwk_…`, `kty` RSA, public members only).
