---
status: done
updated: 2026-09-15
---

# Arch Phase 6 — Decompose `federation/oidc`

The largest package in the repo (4.033 LOC, 19 files) mixes five bounded contexts in one package:
client CRUD (+CIMD), authorize (+device flow), token minting/introspection, userinfo, and the
well-known/logo surfaces. Everything shares package-private scope, so unintended coupling is
invisible, and `store.go` alone is 1.100+ LOC serving every entity.

Do this last: it is the highest-effort phase and depends on the shared helpers from phases 1–3.

## Evidence

- Files: `authorize.go`, `cimd.go`, `client.go`, `device.go`, `token.go`, `userinfo.go`,
  `mint`/`resource`/`handler_*`, one `store.go` covering clients, authorized-clients, codes,
  and tokens.
- Externally mounted via `federation/oidc.New(...)` through `internal/registry` — the public
  seam is small, which keeps the split feasible.

## Tasks

- [x] Decide and record the target layout in the progress log **before** moving files:
      `oidc/client` (client CRUD + CIMD), `oidc/authz` (authorize + device), `oidc/token`
      (mint, introspect, refresh, userinfo), root `oidc` keeps discovery/meta surfaces and the
      `Feature` wiring. Avoid import cycles: `authz`/`token` may depend on `client`'s store
      interface, never the reverse.
- [x] Split the store per entity (`store_client.go`, `store_authz.go`, `store_token.go`) even if
      packages move later — each file owns its table + scans.
- [x] ~~Move files package by package~~ — superseded by the recorded deviation above: the
      subpackage move is rejected; flows stay as one-file-per-flow in the root package
      (constructor signatures untouched).
- [x] Confirm all `/.well-known/*` documents and token/introspect responses are byte-identical
      before/after (curl diff against the running server).

## Decision (recorded before moving) — minimal deviation: file-level split, NO subpackages

The planned subpackage move (`oidc/client`, `oidc/authz`, `oidc/token`) is rejected with cause:

1. `Service` is one stateful type over shared dependencies (`store`, `keys`, authenticator,
   cookie state, audit); the flows share `claimsFor` and `record`. A package split forces
   interface surgery across PKCE, refresh-family, and introspection paths — security-critical,
   byte-compat-required code — for a navigability-only win.
2. Package-private visibility IS the cohesion mechanism here; the real pain from the analysis
   was `store.go` (1.291 LOC mixing five entities' SQL), and that is fully addressed.

Executed instead — store split per entity (task 2), flows stay as package files (token.go,
authorize.go, device.go, userinfo.go, client.go, cimd.go — already one flow per file):

- `store.go` (1291 → ~110 LOC): table consts, `Store` contract, `PostgresStore` +
  constructor, shared helpers (`nullIfEmpty`, `orDefault`, `scanner`).
- `store_client.go` (~570): `Client`/`ClientSecret`/params + client CRUD, group allowlist,
  logo, multi-secret management, CIMD metadata refresh, `scanClient`.
- `store_authz.go` (~330): `AuthorizationCode`, `InteractionSession`, `AuthorizedClient` +
  codes, interactions, consent records, revocation cascade, `scanAuthorizedClient`.
- `store_token.go` (~130): `OAuth2Session` + kind consts + session bookkeeping + JTI replay.
- `store_identity.go` (~155): `UserProfile` + the claim readers (UserByID/UserGroups/
  CustomClaims/UserInGroup).

## Validation

- `task test:go`: 533 pass, 1 skipped, 0 fail. `task test:go:debug`: 35 pass.
  `go test -tags release ./...`: all ok. golangci-lint: 0 issues. vet + gofmt clean.
- Byte-diff against a HEAD build on the SAME scratch database (keys persist, so jwks is
  comparable): `/.well-known/openid-configuration`, `/.well-known/oauth-authorization-server`,
  `/.well-known/jwks.json`, `POST /api/oidc/token` (invalid_client path), `POST
  /api/oidc/introspect` (unauthorized), `GET /api/oidc/userinfo` (401 envelope), `POST
  /api/oidc/end-session` (204) — all IDENTICAL, modulo per-request `request_id` /
  rate-limit counters (not part of the wire contract; bare OAuth docs are exact).

## Progress Log

- 2026-09-15 Phase planned.
- 2026-09-15 Store split per entity done; subpackage move recorded as a deliberate deviation
  (rationale above); byte-compat verified against HEAD on a shared scratch DB.
