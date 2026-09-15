---
status: planned
updated: 2026-09-15
---

# Upstream Endpoint Parity

Goal: fix existing modules in endpoint-matrix order. For every task, compare upstream source,
change only the owning DTO/store/service/handler, add real-Postgres tests, send Yaak requests, and
update the matrix.

Prerequisites: complete transport normalization. Work in the listed order. Before editing an
endpoint, reproduce its current request and response, then compare it with upstream. Do not refactor
unrelated modules during a parity task.

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
dependency at the module boundary and retain only the in-scope behavior.

## Acceptance criteria for each family

- Every method/path has a route test and a real-Postgres handler or service test.
- Request body, parameters, auth headers, cookies, status, response headers, and error fields match
  the matrix.
- JSON uses the responder envelope; protocol-defined bare responses remain bare.
- Yaak requests cover success, unauthorized, and invalid input cases and are updated and re-sent
  whenever any route, parameter, header, body, status, or response field changes.
- The matrix and `llms/tango-deviations.md` are updated before the commit.
