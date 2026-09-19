---
status: done
updated: 2026-09-19
---

# Phase 03: Go ConnectRPC Server

> Completed 2026-09-19 (pilot groups: system.VersionService + admin.ApiKeyService). Delivered:
> `MountRPC` on the route set (module services register into the shared /rpc chi tree; unknown
> procedures answer Connect 404s, reflection is not mounted and a test pins that); bearer-only RPC
> authentication — `middleware.RPCSessionAuth` resolves `Authorization: Bearer` through the same
> session store (cookies never authorize an RPC, expired/revoked/unknown tokens share one
> enumeration-safe message), and the version surface applies it per-procedure via a unary
> interceptor (Current protected, Latest public); `pkg/validate` failures map to structured
> invalid-argument errors via `internal/rpcerr`; TypeID-only path IDs; show-once secrets ride the
> create/renew responses only. Integration tests run through the real transport with
> testcontainers, covering every ApiKeyService method plus the missing/malformed/expired/revoked
> bearer branches. Yaak evidence (folder `[ConnectRPC] System (smoke)`): Latest anonymous 200,
> Current anonymous 401, Current with bearer 200, ApiKey Create 200 (show-once), ApiKey List 200,
> ApiKey Create with a malformed expiry → invalid_argument 400. All three protocols that connect-go
> installs are served on every procedure — Connect Protocol (what the browser uses), gRPC, and
> gRPC-Web — because no handler option restricts them; gRPC reflection is the transport that is
> genuinely absent. Unary Connect over HTTP/1.1 is the only *claimed* transport, and
> `internal/transport.TestRPCServesAllProtocols` pins the other two so the capability is stated
> rather than assumed. Remaining service groups follow in their cutover phases with the same shape.

## Outcome

Expose internal services through ConnectRPC while keeping business logic in existing module
services and stores.

## Tasks

1. Add explicit ConnectRPC handlers beside the owning module handlers; do not put business logic
   in generated adapters.
2. Register generated service implementations from contracts in `api/connect/*.proto`; keep the
   generated files in the chosen generated output directories.
3. Reuse the existing principal, session, API-key, admin, and rate-limit authorization contracts.
4. Add shared middleware/interceptors for request IDs, authentication, authorization, panic
   recovery, logging, deadlines, and error conversion.
5. Authenticate protected RPCs only from the `Authorization: Bearer` header. Treat access and
   refresh cookies as token storage and refresh/session state, not as an RPC auth fallback.
6. Map `pkg/validate` failures to structured Connect invalid-argument errors.
7. Preserve security invariants: enumeration-safe auth responses, secret redaction, show-once
   values, TypeID validation, and transaction boundaries.
8. Add focused handler tests for every RPC method and authorization branch, including missing,
   malformed, expired, revoked, and insufficient-scope bearer tokens.
9. Add integration tests through the real transport, not only direct service calls.
10. For each service group, create and send Yaak MCP Connect Protocol HTTP requests covering
   success, authentication, validation, and authorization behavior. Update the corresponding Yaak
   folder/request when the RPC contract changes.
11. Add a reflection implementation or descriptor-serving test for the selected development/test
    policy. Do not expose unauthenticated production reflection by accident.
12. Verify unary Connect Protocol behavior as the required transport. Verify native gRPC or
    gRPC-Web only if explicitly claimed as compatibility surfaces; document unsupported transports
    instead of silently accepting them.
13. Verify metadata propagation for cookies, API keys, `Authorization`, request IDs, deadlines,
    `Connect-Protocol-Version`, `Connect-Timeout-Ms`, and Connect response metadata through direct
    and proxied requests.

## Boundaries

Connect handlers may depend on transport context and generated messages. Services must remain
independent of ConnectRPC, HTTP handlers, router state, and responder envelopes.

## Gate

Each pilot module works through `/rpc/`, has auth/error tests, has Yaak MCP Connect Protocol evidence, and
has no duplicated domain behavior between its REST and Connect handlers.

## Commit

Commit each completed server service group as one atomic conventional commit. Include the Connect
handler, middleware/interceptors, authorization tests, reflection policy/tests, transport tests,
and Yaak evidence in the same commit. Do not commit generated server code without its matching
proto and handler changes.
