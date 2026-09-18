---
status: done
updated: 2026-09-17
---

# Tango API Client SDK Plan

Goal: build the SPA-facing API client under `api/client/` as a namespaced SDK in the style of
supabase-js (`apiClient.auth.signInWithPassword(params)`), backed by `ofetch`, with vitest tests
under `api/client/tests/`.

Prerequisites: read `llms/endpoint-reference.md` for endpoint contracts and `pkg/responder` for
the envelope. The SDK mirrors the wire contract; it never redefines shapes that drift from the Go
DTOs.

## Principles

- SDK-style namespace: one client object, feature namespaces as properties
  (`auth`, `account`, `users`, `userGroups`, `appConfig`), methods per endpoint.
- ofetch is the only HTTP layer. The client unwraps the `{status, message?, data, error?,
  metadata, links}` envelope and throws a normalized `ApiClientError` on failures.
- Bare-document endpoints (WebAuthn begin payloads, JWKS, discovery, `.well-known/*`) are
  detected at runtime and returned as data without unwrapping.
- Types are hand-written per module and guarded by zod schemas in tests; the SDK ships types,
  not runtime validators, on the hot path.
- The client stays transport-only: no React, no TanStack Query imports, no state management.
  It composes cleanly with TanStack Query at the app layer.

## Files

| File | Purpose |
| --- | --- |
| [01-architecture.md](01-architecture.md) | Core design: client, envelope unwrap, errors, options |
| [02-modules.md](02-modules.md) | Namespace-by-namespace method → endpoint mapping |
| [03-schemas.md](03-schemas.md) | zod schema and TS type strategy per module |
| [04-tests-and-gates.md](04-tests-and-gates.md) | Vitest matrix in `api/client/tests/` and gates |

## Scope

Phase 1 (shipped, commit `734da80`): core client + `auth` (incl. `mfa`, `webauthn`), `account`,
`users`, `userGroups`, `appConfig` — 45 of the ±146 unique method+URL pairs Yaak exercises.

Phase 2 (shipped): every remaining SPA/admin namespace plus auth handling —

- `oidcClients` — client CRUD, secrets, logo (`logoUrl`), allowed groups, meta, preview, refresh
- `consent` — my authorized clients + revoke, accessible clients, admin views (me + by user + all)
- `scim` — provider CRUD, sync, client lookup
- `apis` + `apiAccess` — API resources, permissions, client grants, CIMD toggle, client-centric views
- `apiKeys` — the user's own X-API-KEY machine credentials
- `customClaims` — user and user-group claim CRUD + suggestions
- `auditLogs` — scoped/all listings + filter suggestions
- `webhooks` — endpoint CRUD, test delivery, rotate-secret, deliveries (global + per endpoint)
- `deviceLogin` — request/exchange/inspect/decide pairing flow
- `deviceApproval` — browser side of the OAuth device flow: consent info (`/oidc/device/info`,
  bare document) + form-encoded approve/deny (`/oidc/device/verify`)
- `signupTokens` — admin token-gated signup registry (issue shows the raw token once)
- `users.me` / `users.updateMe` — the `/users/me` self-service profile routes
- extensions: `users` gained webauthn-credential admin + one-time access issue; `account` gained
  email verification + `/users/me` picture; `auth` gained one-time access email/token exchange
- auth handling: `apiKey` option + `setApiKey` rotation → `X-API-KEY` header (machine clients);
  cookie sessions ride `credentials: 'include'`

Coverage after phase 2: 133 of the 145 unique method+URL pairs Yaak exercises; the remaining 10
are relying-party protocol documents and binary image routes reached by URL, not by `fetch`.

## Recommended protocol surface handling

The SDK targets the SPA, the admin console, and internal tools. RP-facing OAuth endpoints
(`/api/oidc/token`, `/api/oidc/introspect`, `/api/oidc/par`, `/api/oidc/device/authorize`,
`/api/oidc/userinfo`, `.well-known/*`) stay out of typed namespaces on purpose: they need client
authentication (secrets/Basic), form encoding, and bare OAuth error bodies — a different error
contract than the admin envelope. When a future module needs them (e.g. a first-party CLI doing
the device grant), prefer a small dedicated `protocol` client beside the SDK instead of widening
the shared executor; `raw()` remains the escape hatch for one-off calls.

Keep new namespaces aligned with future server modules using this workflow: dump the route
inventory (`chi.Walk` in `route_table_test.go`), diff against the SDK method table, and add the
missing namespace plus tests in the same change — this keeps coverage drift visible per module.

The core exposes a typed escape hatch so later namespaces slot in without breaking consumers.
