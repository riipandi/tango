---
status: planned
updated: 2026-09-18
---

# Phase 00: Scope and Route Decisions

## Outcome

Freeze the transport split before implementation. The route matrix in
[endpoint-reference.md](./endpoint-reference.md) is the decision record for this phase.

## Tasks

1. Inventory the live route table and compare it with `llms/endpoint-reference.md`.
2. Mark each route as `ConnectRPC`, `REST`, or `REST + ConnectRPC` with its consumer and reason.
3. Identify all first-party callers: SPA routes, admin screens, CLI commands, tests, Yaak requests,
   and the current `api/client` package.
4. Identify external contracts that must remain HTTP: OAuth/OIDC, WebAuthn, SCIM, health, binary,
   redirects, email links, and webhook delivery.
5. Keep every protobuf contract in `api/connect/*.proto`. Use protobuf packages and service names
   to express module ownership; do not add a generic `internal` architecture tree.
6. Decide generated Go and TypeScript output directories separately from `api/connect/`.
7. Inventory existing Yaak folders and requests through Yaak MCP. Do not inspect only exported
   files when MCP can provide the authoritative request definitions.
8. Record any endpoint whose current implementation is ambiguous before changing it.

## Deliverables

- Approved endpoint reference.
- Route inventory and caller inventory.
- `api/connect/*.proto` package and generated-code layout decision.
- Yaak folder/request mapping for retained REST and new gRPC/ConnectRPC coverage.
- A list of REST routes that may be deleted after cutover.

## Gate

No implementation phase starts until every live route has a protocol decision and every
`ConnectRPC` route has an identified first-party caller and a planned Yaak request/evidence path.
