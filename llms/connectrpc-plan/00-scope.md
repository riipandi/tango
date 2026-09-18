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
2. Mark each exact method/path as `ConnectRPC`, `REST`, or `REST + ConnectRPC` with its consumer
   and reason. Wildcard route families are temporary inventory shortcuts and must be expanded before
   implementation begins.
3. Identify all first-party callers: SPA routes, admin screens, CLI commands, tests, Yaak requests,
   and the current `api/client` package.
4. Identify external contracts that must remain HTTP: OAuth/OIDC, WebAuthn, SCIM, health, binary,
   redirects, email links, and webhook delivery.
5. Keep every protobuf contract in `api/connect/*.proto`. Use protobuf packages and service names
   to express module ownership; do not add a generic `internal` architecture tree.
6. Decide generated Go and TypeScript output directories separately from `api/connect/`.
7. Inventory existing Yaak folders and requests through Yaak MCP. Do not inspect only exported
   files when MCP can provide the authoritative request definitions.
8. Define the canonical protobuf package, service, and RPC method for every ConnectRPC entry.
9. Record any endpoint whose current implementation is ambiguous before changing it.

## Deliverables

- Approved endpoint reference.
- Route inventory and caller inventory.
- `api/connect/*.proto` package and generated-code layout decision.
- Yaak folder/request mapping for retained REST and new Connect Protocol coverage.
- Exact Connect service/method matrix, including authorization and message ownership.
- A list of REST routes that may be deleted after cutover.

## Gate

No implementation phase starts until every live route has a protocol decision and every
`ConnectRPC` route has an identified first-party caller, a canonical service/method, and a planned
Yaak request/evidence path.

## Commit

Commit the completed scope decision as one atomic conventional commit containing only the route
matrix, service/method matrix, Yaak mapping, and related planning changes.
