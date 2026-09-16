---
status: done
updated: 2026-09-16
---

# Transport and Responder

Goal: make route and response behavior consistent before feature-specific fixes.

Prerequisites: complete excluded-feature cleanup, target architecture, and the contract baseline.
Keep `cmd/launcher` unchanged as the caller. `internal/transport` remains the HTTP server boundary;
use `pkg/responder` and `internal/transport` as the response and error contract sources.

## Tasks

1. Verify `/api` relative mounts, root well-known mounts, middleware order, CORS, request IDs,
   rate limits, panic recovery, and content negotiation. Add route-table tests. Commit:
   `test: lock route mounts and middleware order`.
2. Ensure JSON handlers use the responder envelope, pagination metadata, request ID, and snake_case
   fields. Map validation, not-found, conflict, unauthorized, forbidden, and internal errors through
   transport. Preserve protocol-specific bare responses. Commit: `fix: normalize API envelopes`.
3. Match JSON/form/multipart decoding and `Content-Type`, `Location`, `Set-Cookie`,
   `WWW-Authenticate`, cache, CORS, and security headers. Keep validation in DTO `Validate`
   methods and remove duplicate handler guards. Commit: `fix: align request and headers`.

## Validation and Yaak requirement

Add tests for route mounts, envelope shape, validation errors, auth errors, pagination metadata,
bare protocol responses, cookies, and required headers. Whenever a route or header changes, update
and re-send its Yaak request with the same method, URL, parameters, headers, body, auth, and expected
response as the matrix. Verify no route is mounted twice or under `/api/api/...`. Use explicit
timeouts and fail-fast execution; stop a stalled request instead of waiting for the default timeout.
Ask the project owner before resolving an ambiguous upstream header or response behavior.
