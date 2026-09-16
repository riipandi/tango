---
status: done
updated: 2026-09-16
---

# Upstream Endpoint Parity

Goal: fix existing modules in endpoint-matrix order. For every task, compare upstream source,
change only the owning DTO/store/service/handler, add real-Postgres tests, send Yaak requests, and
update the matrix.

Prerequisites: complete transport normalization and the target architecture decisions. Work in the
listed order. Before editing an endpoint, reproduce its current request and response, then compare
it with upstream. Keep route ownership in the four module boundaries; do not create one new module
per endpoint family.

## Tasks

1. Health, version, well-known, and discovery. Commit: `fix: align health and discovery contracts`.
2. Users, groups, signup, one-time access, and email verification. Commit:
   `fix: align user and signup contracts`.
3. Passkeys and device login. Commit: `fix: align passkey and device-login contracts`.
4. OIDC clients, authorize, token, userinfo, introspection, and end-session. Commit:
   `fix: align OIDC protocol contracts`.
5. APIs, permissions, API access, API keys, and rate limits. Commit:
   `fix: align API access contracts`.
6. Custom claims and audit logs. Commit: `fix: align claims and audit contracts`.
7. SCIM and remaining in-scope application configuration. Commit:
   `fix: align SCIM and configuration contracts`.

Do not port LDAP or Application Images. If shared code depends on either feature, split the
dependency at the module boundary and retain only the in-scope behavior. The target module owners
are `modules/identity`, `modules/federation`, `modules/admin`, and `modules/webhook`.

## Acceptance criteria for each family

- Every method/path has a route test and a real-Postgres handler or service test.
- Request body, parameters, auth headers, cookies, status, response headers, and error fields match
  the matrix.
- JSON uses the responder envelope; protocol-defined bare responses remain bare.
- Yaak requests cover success, unauthorized, and invalid input cases and are updated and re-sent
  whenever any route, parameter, header, body, status, or response field changes.
- The matrix and `llms/tango-deviations.md` are updated before the commit.
- Focused tests and Yaak requests use explicit timeouts and fail-fast behavior. An unavailable
  dependency or hanging request is reported immediately.
- If upstream source, docs, or live behavior conflict, record the evidence and ask the project owner
  before selecting a contract or changing the parity matrix.
