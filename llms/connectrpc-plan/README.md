---
status: planned
updated: 2026-09-18
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
- Use the existing Vite proxy during development for both `/api` and the ConnectRPC prefix.
- Remove obsolete internal REST handlers, DTO duplication, and API-client REST plumbing after cutover.

## Decisions

- ConnectRPC routes live below `/connect/` and are served by the same binary and port as REST.
- ConnectRPC uses the Connect protocol with JSON support for browser development and debugging.
- Protobuf contract files live directly in `api/connect/*.proto`; this directory is the source of
  truth for all internal RPC services.
- Generated Go and TypeScript files live outside `api/connect/` in tool-defined generated output
  directories; generated files must never be mixed with hand-written `.proto` contracts.
- Protobuf remains the source of truth for internal request and response types.
- Cookies, API keys, request IDs, CSRF policy, rate limits, and authorization are enforced by shared
  transport middleware, not reimplemented in individual RPC methods.
- The existing `/api` REST surface is not preserved for first-party consumers after its replacement
  is verified. Public protocol routes are not moved under `/connect/`.
- `api/client` remains the REST-only SDK. It is updated to cover retained REST endpoints and
  protocol integrations, but it does not wrap ConnectRPC services.
- ConnectRPC clients are generated directly from `api/connect/*.proto` and are consumed by the SPA,
  admin console, CLI, and other first-party internal tools.
- The REST SDK keeps a small raw HTTP escape hatch for OAuth/OIDC operations that need form
  encoding, redirects, Basic authentication, or bare protocol errors.

## Phase index

1. [Scope and route decisions](./00-scope.md)
2. [Transport and proxy foundation](./01-transport.md)
3. [Protobuf and code generation](./02-protobuf.md)
4. [Go ConnectRPC server](./03-server.md)
5. [Frontend and internal client](./04-client.md)
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
- Treat Yaak requests as executable contract evidence. Create, update, and send REST and gRPC
  requests through the Yaak MCP integration; never edit exported Yaak request files manually.
- ConnectRPC endpoints may be tested through Yaak's gRPC request support when the server exposes
  the gRPC protocol. If the correct Connect, gRPC, or gRPC-Web transport is unclear, consult the
  official ConnectRPC documentation before choosing the Yaak request type or content type.

## Completion criteria

The refactor is complete when every endpoint in the reference has an explicit protocol decision,
all ConnectRPC routes have generated Go and TypeScript clients, all first-party callers use the
Connect client, internal REST routes are removed, protocol REST tests remain green, Yaak request
evidence is current, and the full Go, frontend, typecheck, lint, and format gates pass.
