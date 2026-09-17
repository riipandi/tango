---
status: in-progress
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

Phase 1 (this plan): core client + `auth` (incl. `mfa`, `webauthn`), `account`, `users`,
`userGroups`, `appConfig` — the five module files already scaffolded in `api/client/modules/`.

Follow-up namespaces (not in this plan): `oidcClients`, `apiKeys`, `apiAccess`, `auditLogs`,
`customClaims`, `webhooks`, `scim`, `deviceLogin`. The core exposes a typed escape hatch so later
namespaces slot in without breaking consumers.
