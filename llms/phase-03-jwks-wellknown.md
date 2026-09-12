---
status: planned
updated: 2026-09-12
---

# Phase 3 — JWKS + Well-Known

Key material provider and discovery endpoints. Prerequisite for every OIDC flow. Pocket ID
reference: `backend/internal/service/jwt_service.go`, `controller/well_known_controller.go`.

## Goal

RSA/EC keys generated, rotated, and served as a JWKS; OIDC discovery document served at
`/.well-known/openid-configuration`.

## Deliverables

- `pkg/jwtutils` extension — `KeyProvider` interface loading active keys from Postgres; signing
  key selection by `kid`.
- `modules/identity/jwks` (or inside `identity` core) — key generation on first boot, rotation
  with overlap window, private keys encrypted at rest via `pkg/crypto.Cipher`; table `jwks`.
- `modules/wellknown` — restored module: `/.well-known/openid-configuration` (issuer, endpoints,
  scopes, claims) + `/.well-known/jwks.json` (public keys only).

## Tasks

- [ ] `KeyProvider` in `pkg/jwtutils` — `SignKey(ctx) (jwk.Key, bool)`, `VerifyKeySet(ctx)`
      (cache + invalidation on rotation).
- [ ] Key generation: RSA 2048 default, EC P-256 optional via config; `jwk.AssignKeyID`.
- [ ] `jwks` store — insert, list active, mark retired; encryption of `private_key` column.
- [ ] Rotation: new key becomes signing key, previous stays in JWKS until `overlap` passes
      (antree delayed task or lazy check on load).
- [ ] `modules/wellknown/handler.go` — discovery document from config (`PUBLIC_BASE_URL` issuer)
      + JWKS endpoint; module re-registered in `internal/registry`.
- [ ] Tests: keygen determinism of `kid`, rotation overlap, JWKS shape; discovery snapshot test.

## Validation

- A token signed by the active key verifies via fetched JWKS (round-trip test with jwx).
- Three standard suites + lint + gofmt clean.

## Progress Log

- 2026-09-12 Phase created (planned).
