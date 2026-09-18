---
status: planned
updated: 2026-09-18
---

# Phase 04: Frontend, REST SDK, and Connect Client

## Outcome

Make the SPA, admin console, and internal tools use typed ConnectRPC clients while keeping
`api/client` as the REST-only SDK for retained HTTP and protocol endpoints.

## Tasks

1. Add a Connect client transport generated from `api/connect/*.proto`, configured with the
   same-origin `/connect` base URL and credentials.
2. Add `/connect` to the Vite proxy and use a relative base URL in the browser.
3. Update `api/client` to retain only REST namespaces and methods for endpoints marked `REST` or
   REST portions of `REST + ConnectRPC`.
4. Remove `api/client` methods for routes that move exclusively to ConnectRPC, including their
   schemas, fixtures, exports, and tests.
5. Keep `raw()` or dedicated REST protocol methods for OAuth/OIDC, WebAuthn, binary, health,
   discovery, and other retained HTTP endpoints.
6. Update TanStack Query/Router call sites: internal application calls use generated Connect
   clients, while protocol and HTTP calls use `api/client`.
7. Mirror Connect errors in generated/typed RPC client errors without reintroducing the REST
   envelope; keep REST error handling in `api/client`.
8. Update vitest fixtures and tests for both transports, asserting RPC method paths/messages and
   REST method URLs/bodies/headers separately.
9. Update `api/client/README.md` to state clearly that the SDK is REST-only and document the
   generated Connect client as a separate integration.
10. Update Yaak requests through Yaak MCP whenever a REST request, gRPC request, auth header,
    metadata field, message, expected status, or response shape changes. Do not edit Yaak export
    files manually.

## Gate

The SPA and admin console have no calls to internal `/api` routes, `api/client` contains only
retained REST methods, generated Connect clients cover all internal RPC namespaces, and the
protocol client still passes OAuth/OIDC and WebAuthn tests. Yaak MCP REST and gRPC requests match
the active contracts.
