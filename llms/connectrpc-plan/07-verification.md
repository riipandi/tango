---
status: planned
updated: 2026-09-18
---

# Phase 07: Verification and Release Gate

## Tasks

1. Add route inventory tests for every Connect service and every retained REST endpoint.
2. Test browser access through Vite proxy in development and preview modes.
3. Test cookie sessions, API keys, admin guards, CORS/CSRF, request IDs, rate limits, deadlines,
   and structured errors through the Connect transport.
4. Test that OAuth/OIDC response bodies, form encoding, redirects, headers, and bare documents are
   unchanged by the refactor.
5. Test WebAuthn ceremonies, binary images, health probes, webhook signatures, and SCIM behavior
   on HTTP.
6. Through Yaak MCP, send representative requests for every endpoint group: REST requests for
   retained HTTP contracts and Connect Protocol HTTP requests for ConnectRPC services. Verify
   method/path, `/rpc` procedure, Connect headers, metadata, credentials, body/message, status,
   headers, and response shape.
7. Repeat representative Yaak REST and Connect Protocol requests through the proxied HTTPS endpoint
   provided by the `nginx` service in `compose.yaml` (including TLS, host forwarding, cookies, and
   Connect metadata forwarding). Do not treat direct Go-port success as sufficient proxy validation.
8. If Connect Protocol request behavior, content types, GET semantics, streaming, or optional gRPC
   compatibility is unclear, consult the official Connect Protocol documentation and record the
   selected transport in the Yaak request description.
9. Verify the server reflection policy, generated descriptor availability, and Yaak behavior with
   and without reflection as applicable.
10. Verify CORS, `Authorization`, and credentials explicitly. The current middleware's wildcard
    origin and disabled credentials must not be assumed compatible with the chosen worker/token
    bridge. Protected RPCs must fail without a bearer header even when an access-token cookie exists.
11. Verify the `vite-plugin-comlink` worker with production-built assets, including worker URL
    resolution, same-origin policy, worker restart, endpoint release, termination, and absence of
    raw refresh-token transfer across the worker boundary.
12. Run the repository gates: `task test`, `task lint`, `task check`, `task format`, and
   `task typecheck`.
13. Run a production build and verify that the embedded SPA calls the same-origin `/rpc` prefix,
    while retained REST calls continue using `/api`.
14. Update the main endpoint reference only if the active contract changed; this plan's reference
   remains the transport decision record.

## Completion criteria

- All first-party application calls use ConnectRPC.
- All external protocol integrations use their required HTTP contracts.
- No internal REST compatibility layer remains.
- Generated code is reproducible and checked in according to repository convention.
- Yaak requests are created, updated, and sent through Yaak MCP; no manual Yaak export edits are
  used as contract evidence.
- Full test, lint, format, vet, and typecheck gates pass.

## Commit

Commit the final verification and documentation updates as one atomic conventional commit only
after all phase commits are present and the complete gate passes. Do not fold unrelated fixes into
the verification commit.
