---
status: done
updated: 2026-09-19
---

# ConnectRPC Architecture Refactor Plan

This plan moves Tango's first-party application API from JSON REST handlers to ConnectRPC while
keeping protocol endpoints that are consumed by external applications as HTTP REST. The project
is still new, so this is a transport refactor rather than a compatibility migration: internal REST
routes may be removed once their ConnectRPC replacements and frontend callers are complete.

The authoritative route-by-route decision is in [endpoint-reference.md](./endpoint-reference.md).

## Goals

- Use ConnectRPC for the SPA, admin console, CLI, and other first-party internal clients.
- Keep OAuth/OIDC, WebAuthn, SCIM protocol, health, binary, and webhook delivery contracts on HTTP.
- Keep domain services and stores transport-neutral; REST and ConnectRPC handlers are adapters.
- Generate typed Go and TypeScript contracts from protobuf definitions.
- Use the existing Vite proxy during development for REST under `/api` and ConnectRPC under `/rpc`.
- Remove obsolete internal REST handlers, DTO duplication, and API-client REST plumbing after cutover.

## Decisions

- ConnectRPC routes live below `/rpc/` and are served by the same binary and port as REST under
  `/api/`.
- ConnectRPC uses the Connect protocol with JSON support for browser development and debugging.
- Protobuf contract files live directly in `api/connect/*.proto`; this directory is the source of
  truth for all internal RPC services.
- Generated Go and TypeScript files live outside `api/connect/` in tool-defined generated output
  directories; generated files must never be mixed with hand-written `.proto` contracts.
- Protobuf remains the source of truth for internal request and response types.
- Cookies, API keys, request IDs, CSRF policy, rate limits, and authorization are enforced by shared
  transport middleware, not reimplemented in individual RPC methods.
- The existing `/api` REST surface is not preserved for first-party consumers after its replacement
  is verified. Public protocol routes are not moved under `/rpc/`.
- `api/client` remains the REST-only SDK. It is updated to cover retained REST endpoints and
  protocol integrations, but it does not wrap ConnectRPC services.
- ConnectRPC clients are generated directly from `api/connect/*.proto` and are consumed by the SPA,
  admin console, CLI, and other first-party internal tools.
- The REST SDK keeps a small raw HTTP escape hatch for OAuth/OIDC operations that need form
  encoding, redirects, Basic authentication, or bare protocol errors.
- First-party application RPCs authenticate with `Authorization: Bearer <access-token>`. Cookies
  are storage for the access and refresh tokens, not the primary RPC authentication transport.
- Machine clients authenticate the admin application API with `X-API-KEY`; the credential is
  resolved into the same principal shape as a browser bearer, and API key self-management
  (create/renew) stays session-only so a leaked key cannot extend itself.
- The browser token lifecycle is handled by a dedicated web worker. The implementation must resolve
  the browser security boundary before coding: web workers cannot read `HttpOnly` cookies, so the
  plan must not assume that a worker can extract tokens from secure cookies.
- Browser-to-worker communication uses `comlink` from `GoogleChromeLabs/comlink` through
  `vite-plugin-comlink`. The plugin owns worker construction and Comlink wrapping; do not add a
  parallel hand-written `postMessage`, `Comlink.wrap`, or `Comlink.expose` integration.
- Internal application access/refresh tokens are distinct from OAuth/OIDC tokens issued to external
  relying parties. The OIDC protocol contract and its bearer/form/Basic authentication rules remain
  unchanged.
- The Connect Protocol is the canonical `/rpc` wire protocol. It uses ordinary HTTP semantics and
  does not require gRPC framing or HTTP/2 for unary RPCs.
- Native gRPC compatibility is optional and must not drive the primary browser, Vite, Nginx, or Yaak
  design. If streaming or external gRPC clients are later required, verify that transport separately.
- HTTPS proxy validation must cover the Nginx paths in `compose.yaml`: `3443` through Vite and
  `8443` directly to the Go server. Unary Connect Protocol requests must work through HTTP/1.1;
  HTTP/2 is required only for Connect streaming modes or separately supported gRPC compatibility.
- The Vite and Nginx paths must forward `Authorization` and relevant Connect metadata without
  falling back to cookie authentication for RPCs.
- Development/test reflection is allowed only under an explicit policy; production reflection must
  be disabled or access-controlled.

## Phase index

1. [Scope and route decisions](./00-scope.md)
2. [Transport and proxy foundation](./01-transport.md)
3. [Protobuf and code generation](./02-protobuf.md)
4. [Go ConnectRPC server](./03-server.md)
5. [Authentication transport and web worker](./04-client.md)
6. [Domain-by-domain cutover](./05-cutover.md)
7. [REST retirement and cleanup](./06-retirement.md)
8. [Verification and release gate](./07-verification.md)

## Required working rules

- Read the endpoint reference before changing a route.
- Keep REST for a route marked `REST`; do not create a second RPC contract for it without a new
  integration requirement.
- For a route marked `ConnectRPC`, update protobuf, generated code, server tests, client tests, and
  the affected SPA/admin callers in the same phase.
- Keep protocol response shapes, headers, status codes, redirects, cookies, and content types exact.
- Do not add compatibility aliases, dual writes, fallback readers, or a generic transport registry.
- Preserve the existing module boundaries: `identity`, `federation`, `admin`, and `webhook`.
- Define every Connect service and RPC method before implementation. The old REST path is a
  migration reference, not the canonical ConnectRPC contract.
- Treat Yaak requests as executable contract evidence. Create, update, and send REST and Connect Protocol
  requests through the Yaak MCP integration; never edit exported Yaak request files manually.
- ConnectRPC endpoints must be tested through Yaak as Connect Protocol HTTP requests. If a request
  involves streaming or optional gRPC compatibility and the transport is unclear, consult the
  official Connect Protocol documentation before choosing the request type or content type.
- Each completed phase must be delivered as one atomic conventional commit. The commit includes
  the implementation, generated code, tests, Yaak evidence updates, and documentation for that
  phase; it must not contain unrelated work or a partial phase. Do not push commits unless the
  user explicitly requests it.

## Completion criteria

The refactor is complete when every endpoint in the reference has an explicit protocol decision,
all ConnectRPC services have an explicit service/method matrix, generated Go and TypeScript clients, all first-party callers use the
Connect client, internal REST routes are removed, protocol REST tests remain green, Yaak request
evidence is current, every phase has its atomic commit, and the full Go, frontend, typecheck, lint,
format, and verification gates pass.
