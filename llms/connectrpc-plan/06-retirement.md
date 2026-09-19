---
status: done
updated: 2026-09-19
---

# Phase 06: REST Retirement and Cleanup

## Outcome

Remove obsolete internal REST infrastructure without touching public protocol contracts.

## Tasks

1. Delete internal REST handlers, route registrations, DTOs, responder adapters, and client methods
   that have a ConnectRPC replacement.
2. Remove unused REST envelope conversion and internal namespace methods from `api/client`.
3. Keep `api/client` as the REST-only SDK for retained HTTP/protocol endpoints; do not remove its
   REST transport, raw escape hatch, or protocol namespaces.
4. Keep protocol-only HTTP helpers and REST clients isolated from generated Connect clients.
5. Remove Vite proxy entries only for routes that no longer exist; retain `/api` for protocol REST
   and any explicitly retained REST route.
6. Update route-table tests, REST SDK documentation, Yaak scope, and endpoint references. Perform
   all Yaak changes through Yaak MCP; do not edit request export files manually.
7. Search for stale internal `/api` calls in TypeScript, Go, tests, docs, and scripts.
8. Delete temporary migration code. No aliases, fallback transports, dual registration, or feature
   flags remain unless a live operational requirement is documented.

## Gate

The route table contains only intended REST routes and ConnectRPC routes; stale internal REST calls
are absent; production builds do not include deleted handlers; Yaak MCP requests cover the retained
REST and Connect Protocol surfaces.

## Commit

Commit REST retirement as one atomic conventional commit containing route deletion, dead client/DTO
cleanup, Vite proxy cleanup, route tests, documentation, and Yaak updates. Do not delete an internal
REST route in a commit that lacks its verified ConnectRPC replacement.
