---
status: planned
updated: 2026-09-18
---

# Phase 02: Protobuf and Code Generation

## Outcome

Create stable internal RPC contracts without copying transport-specific REST envelopes into RPC
messages.

All hand-written protobuf contracts in this phase are placed directly in `api/connect/*.proto`.
Use files such as `api/connect/identity.proto`, `api/connect/admin.proto`,
`api/connect/federation.proto`, and `api/connect/webhook.proto` unless the service split requires
more focused files. Do not create nested contract directories under `api/connect/`.

## Tasks

1. Add protobuf definitions grouped by owning module in `api/connect/*.proto`: identity, admin,
   federation, and webhook.
2. Define request, response, pagination, filtering, and validation messages in protobuf terms.
3. Represent show-once secrets explicitly and document that they are never returned by read RPCs.
4. Keep TypeID values as strings at the wire boundary; do not expose raw UUIDs in RPC paths.
5. Define a consistent Connect error mapping for validation, authentication, authorization,
   not-found, conflict, rate-limit, and internal errors.
6. Configure deterministic generation for Go and TypeScript from `api/connect/*.proto`. Generated
   output must be reproducible in CI and must not be edited manually or written back into the
   contract directory.
7. Add a task target for generation and a check that fails when generated output is stale.
8. Keep `api/client` schemas and methods limited to retained REST endpoints. Do not make the REST
   SDK wrap generated ConnectRPC clients or duplicate protobuf service contracts.
9. Decide whether generated TypeScript protobuf types are consumed directly by the Connect client
   or adapted by a thin generated transport layer; do not maintain hand-written RPC DTOs beside
   the protobuf messages.
10. Create or update Yaak gRPC requests through Yaak MCP for representative generated services.
    Use the generated service and method names from the proto contract; never hand-edit exported
    Yaak request YAML.

## Contract rules

- RPC messages use protobuf naming and generated JSON mapping; they do not use the REST envelope.
- `api/connect/*.proto` contains contracts only: no generated code, server implementation, client
  wrappers, or transport-specific helper logic.
- Pagination has one shared message shape across internal list RPCs.
- Empty success responses use `google.protobuf.Empty` or an explicit result only when metadata is
  needed; do not encode HTTP 204 semantics into every RPC.
- Protocol REST DTOs remain hand-written where RFC wire behavior requires it.

## Gate

Generation works from a clean checkout, generated Go and TypeScript compile, and one representative
RPC has a request/response compatibility test plus a Yaak MCP gRPC request that uses the generated
service contract.
