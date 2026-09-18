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
   retained HTTP contracts and gRPC requests for ConnectRPC services. Verify method/path or
   service/method, metadata, credentials, body/message, status, headers, and response shape.
7. Repeat representative Yaak REST and gRPC requests through the proxied HTTPS endpoint provided
   by the `nginx` service in `compose.yaml` (including TLS, host forwarding, cookies, and gRPC
   forwarding). Do not treat direct Go-port success as sufficient proxy validation.
8. If the gRPC request behavior or protocol mapping is unclear, consult the official ConnectRPC
   documentation and record the selected transport in the Yaak request description.
9. Run the repository gates: `task test`, `task lint`, `task check`, `task format`, and
   `task typecheck`.
10. Run a production build and verify that the embedded SPA calls the same-origin Connect prefix.
11. Update the main endpoint reference only if the active contract changed; this plan's reference
   remains the transport decision record.

## Completion criteria

- All first-party application calls use ConnectRPC.
- All external protocol integrations use their required HTTP contracts.
- No internal REST compatibility layer remains.
- Generated code is reproducible and checked in according to repository convention.
- Yaak requests are created, updated, and sent through Yaak MCP; no manual Yaak export edits are
  used as contract evidence.
- Full test, lint, format, vet, and typecheck gates pass.
