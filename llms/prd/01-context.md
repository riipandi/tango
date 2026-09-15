---
status: draft
updated: 2026-09-15
owner: tango-auth-porting
---

# Product Context and Outcomes

## Problem

The tango port has the broad shape of Pocket ID but its architecture, persistence model, transport
layer, and response contract differ. Historical completion notes are not sufficient evidence that
the public API matches upstream. Some existing work also includes features that are intentionally
outside tango's target product.

## Product goal

Provide a maintainable, Postgres-backed authentication and OIDC service whose in-scope HTTP API is
behaviorally compatible with Pocket ID v2.14.0 while using tango's architecture and standard JSON
response envelope.

The architecture must preserve `cmd/launcher` and the current flat `internal/` layout. Complexity
reduction is focused on four explicit application modules under `modules/`: identity, federation,
admin, and webhook.

## Outcomes

1. Consumers can use the in-scope Pocket ID endpoints without changing method, path, parameters,
   request encoding, authentication headers, status behavior, or protocol-required headers.
2. Tango JSON responses are consistent and observable through `pkg/responder`.
3. Password authentication supports sign-in, sign-out, session inspection, password change, forgot
   password, and reset password.
4. Users can enroll and use TOTP MFA with safe recovery and session assurance.
5. Services can subscribe to signed webhook events with reliable delivery logs.
6. Agents can verify every endpoint through focused tests and Yaak MCP requests.
7. The code remains modular, understandable, Postgres-only, and free of excluded-feature residue.
8. Every new recoverable encrypted value uses the canonical `enc:` prefix from `pkg/crypto`,
   while hash-only values remain one-way hashes.
9. The database contains only the final in-scope schema, with no legacy code or backward-
   compatibility behavior.

## Non-goals

- Reproducing Pocket ID's internal framework or database implementation.
- Supporting SQLite or MySQL.
- Building LDAP, Application Images, or a generic identity-provider abstraction.
- Replacing Yaak verification with unit tests alone.
- Replacing the current command entrypoint or creating a new nested internal platform tree.

## Success metrics

- 100% of in-scope endpoint matrix rows have current contract and live-test evidence.
- 0 active LDAP or Application Images runtime routes after cleanup.
- 0 known responder-envelope violations outside documented bare-response exceptions.
- Password, MFA, and webhook security cases pass automated tests and live checks.
- Full project test, lint, format, and vet gates pass.
- No new recoverable encrypted value is stored without the `enc:` prefix.
- No legacy route, fallback reader, compatibility adapter, dual write, or transitional database
  column remains.
