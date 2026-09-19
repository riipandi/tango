---
status: done
updated: 2026-09-19
---

# Phase 05: Domain-by-Domain Cutover

## Outcome

Move first-party API surfaces in dependency order and remove each old internal REST handler after
its callers and tests are migrated.

## Order

1. Identity read surfaces: current user, users, groups, sessions, and account settings.
2. Authentication and MFA: sign-in, sign-out, password lifecycle, TOTP, signup, and one-time
   access. Keep email-link and passkey protocol considerations from the endpoint reference.
3. Admin surfaces: API keys, APIs, grants, application configuration, audit logs, and custom claims.
4. Federation administration: OIDC client CRUD, secrets, logos, metadata, grants, and SCIM provider
   configuration.
5. Webhook administration and delivery inspection.
6. Internal device-login approval UI and version information where marked `ConnectRPC`.

For each domain:

- add protobuf and generated clients;
- add Connect server handlers and authorization tests;
- migrate SPA/admin/CLI callers;
- run focused Go and vitest suites;
- create or update the matching Yaak request through Yaak MCP and send it against the running
  server; use a Connect Protocol HTTP request for ConnectRPC services and a REST request for retained
  HTTP routes;
- record the Yaak request/evidence identifier in the phase or endpoint reference;
- verify the request through the supported direct and HTTPS proxy transport, including TLS and
  HTTP/2 where gRPC is claimed;
- remove the old internal REST route and its dead DTO/client code;
- update the endpoint reference and route inventory.

## Gate

Each completed domain has one active first-party transport, no compatibility route, and a passing
focused test suite before the next domain starts.

## Commit

Each domain cutover must be one atomic conventional commit. Include proto changes, generated code,
server/client changes, caller migration, REST route removal, tests, Yaak evidence, and endpoint
reference updates together. If a domain is too large, split it into independently complete
service-level atomic commits and document the boundary before implementation.
