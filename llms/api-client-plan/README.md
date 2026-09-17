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

- `oidcClients` — client CRUD, secrets, logo, allowed groups, meta, preview, refresh
- `consent` — my authorized clients + revoke, accessible clients, admin views (me + by user + all)
- `scim` — provider CRUD, sync, client lookup
- `apis` + `apiAccess` — API resources, permissions, client grants, CIMD toggle, client-centric views
- `apiKeys` — the user's own X-API-KEY machine credentials
- `customClaims` — user and user-group claim CRUD + suggestions
- `auditLogs` — scoped/all listings + filter suggestions
- `webhooks` — endpoint CRUD, test delivery, rotate-secret, deliveries (global + per endpoint)
- `deviceLogin` — request/exchange/inspect/decide pairing flow
- `system` — version current/latest, readiness probe
- extensions: `users` gained webauthn-credential admin + one-time access issue; `account` gained
  email verification + `/users/me` picture; `auth` gained one-time access email/token exchange
- auth handling: `apiKey` option + `setApiKey` rotation → `X-API-KEY` header (machine clients);
  cookie sessions ride `credentials: 'include'`

Protocol surfaces kept out of the SDK on purpose (called by relying parties, not the SPA):
`/api/oidc/token`, `/api/oidc/introspect`, `/api/oidc/par`, `/api/oidc/device/authorize`,
`/api/oidc/userinfo`, and the `.well-known` documents — reachable via `raw()` when needed.

The core exposes a typed escape hatch so later namespaces slot in without breaking consumers.
