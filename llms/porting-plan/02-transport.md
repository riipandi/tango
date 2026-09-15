---
status: planned
updated: 2026-09-15
---

# Transport and Responder

Goal: make route and response behavior consistent before feature-specific fixes.

Prerequisites: complete excluded-feature cleanup and the contract baseline. Use `pkg/responder` and
`internal/transport` as the response and error contract sources.

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
response as the matrix. Verify no route is mounted twice or under `/api/api/...`.
