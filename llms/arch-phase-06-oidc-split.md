---
status: planned
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

- [ ] Decide and record the target layout in the progress log **before** moving files:
      `oidc/client` (client CRUD + CIMD), `oidc/authz` (authorize + device), `oidc/token`
      (mint, introspect, refresh, userinfo), root `oidc` keeps discovery/meta surfaces and the
      `Feature` wiring. Avoid import cycles: `authz`/`token` may depend on `client`'s store
      interface, never the reverse.
- [ ] Split the store per entity (`store_client.go`, `store_authz.go`, `store_token.go`) even if
      packages move later — each file owns its table + scans.
- [ ] Move files package by package, keeping the registry-facing constructor signature stable;
      run the full gate after each move.
- [ ] Confirm all `/.well-known/*` documents and token/introspect responses are byte-identical
      before/after (curl diff against the running server).

## Validation

`task test` (all suites — `modules/federation/oidc` tests move with their packages),
`task lint`, `task check`, curl diffs recorded in the progress log.

## Progress Log

- 2026-09-15 Phase planned.
