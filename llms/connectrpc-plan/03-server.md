---
status: planned
updated: 2026-09-18
---

# Phase 03: Go ConnectRPC Server

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
5. Map `pkg/validate` failures to structured Connect invalid-argument errors.
6. Preserve security invariants: enumeration-safe auth responses, secret redaction, show-once
   values, TypeID validation, and transaction boundaries.
7. Add focused handler tests for every RPC method and authorization branch.
8. Add integration tests through the real transport, not only direct service calls.
9. For each service group, create and send Yaak MCP gRPC requests covering success, authentication,
   validation, and authorization behavior. Update the corresponding Yaak folder/request when the
   RPC contract changes.

## Boundaries

Connect handlers may depend on transport context and generated messages. Services must remain
independent of ConnectRPC, HTTP handlers, router state, and responder envelopes.

## Gate

Each pilot module works through `/connect/`, has auth/error tests, has Yaak MCP gRPC evidence, and
has no duplicated domain behavior between its REST and Connect handlers.
