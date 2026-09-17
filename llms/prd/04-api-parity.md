---
status: done
updated: 2026-09-18
owner: tango-auth-porting
---

# Upstream API Parity Requirements

## Contract dimensions

Every in-scope method/path must be compared across these dimensions:

| Dimension | Requirement |
| --- | --- |
| Method and path | Exact upstream method and public path, including `/api` placement |
| Parameters | Same path/query names, requiredness, formats, defaults, and invalid-input behavior |
| Request body | Same JSON/form/multipart encoding and field semantics, translated only to tango naming rules |
| Authentication | Same accepted session/API-key/client credentials and authorization boundary |
| Headers | Match content type, cookies, redirects, CORS, cache, request ID, and protocol headers |
| Success | Same status semantics and fields, wrapped by `pkg/responder` where allowed |
| Errors | Consistent tango envelope with mapped status and stable error code/message fields |
| Protocol exceptions | OAuth/OIDC, well-known, health, and required binary responses remain bare |
| Pagination | Use tango page/limit/sort metadata and document the intentional structural deviation |
| IDs | Use tango TypeID at URL boundaries and convert to Postgres UUID at store edges |

## Response requirement

Normal JSON endpoints must use the envelope defined by `pkg/responder`, including status, request
metadata, pagination, links, and error fields where applicable. Do not copy upstream bare JSON or
error objects into normal tango handlers.

## Validation requirement

DTOs define validation through `pkg/validate` and ozzo rules. Handlers decode and validate once.
Invalid bodies, path parameters, and query parameters return the standard validation response.

## Verification requirement

For every route, an agent must:

1. Compare local upstream source, API docs, endpoint matrix, and current tango implementation.
2. Add or update focused tests using real Postgres where persistence is involved.
3. Update the matching Yaak request's name, method, URL, parameters, headers, auth, body, and
   expected response.
4. Send the updated Yaak request against the running server.
5. Record status/header/body evidence and intentional deviations.

Stale or orphaned Yaak requests are a parity failure.

## Architecture constraints for parity work

- `cmd/launcher` must continue to load config, open infrastructure, start the runtime, and shut it
  down.
- `internal/transport` remains responsible for server middleware and HTTP server setup.
- `internal/registry` remains the composition root but should expose an explicit runtime rather than
  discover modules through generic capability interfaces.
- Endpoint ownership must resolve to identity, federation, admin, or webhook. Do not create a new
  module just to own one endpoint.
- At-rest encryption changes must not alter public response shapes. Secret-bearing endpoints remain
  redacted or one-time-only and use `pkg/responder` for normal JSON responses.
- The target API is final-state only. Do not keep old routes, fallback request shapes, compatibility
  response branches, or adapters for superseded tango implementations.
