---
status: planned
updated: 2026-09-15
---

# Scope and Rules

## Architecture constraints

- Keep `cmd/launcher` as the CLI and server entrypoint. Do not create a replacement command tree.
- Keep the current flat `internal/` package layout. Do not introduce `internal/app`,
  `internal/platform`, `internal/domain`, or another infrastructure tree.
- Keep `internal/registry` as the composition root, but simplify it into an explicit concrete
  runtime builder. Do not replace it with a generic plugin registry.
- Apply the main module reduction under `modules/`: identity, federation, admin, and webhook are
  the four application boundaries.
- Do not add a dependency-injection framework, service locator, generic event bus, microservice
  boundary, or message broker.
- This is a fresh target schema. Do not preserve legacy code, legacy routes, fallback readers,
  compatibility adapters, compatibility views, dual writes, or transitional database columns.

## In scope

- Pocket ID endpoints listed in `llms/endpoint-reference.md`, verified against the local v2.14.0
  checkout and the published API reference.
- Existing tango authentication, OIDC, WebAuthn, device-login, users, groups, API keys,
  API-access, audit, email verification, signup, and SCIM surfaces.
- Password sign-in, change-password, forgot-password, and reset-password flows.
- TOTP MFA enrollment, verification, recovery, disablement, and session integration.
- Tango webhooks, including signing, delivery, retries, and logs.
- Canonical recoverable-secret encryption through `pkg/crypto` with the `enc:` prefix.

## Explicit non-goals

- LDAP configuration and synchronization. Existing LDAP implementation must be removed and cleaned
  up before parity work continues.
- Application Images endpoints and image blob storage.
- SQLite and MySQL support.
- Upstream-only internals such as Gin, GORM, Francis actors, and upstream response types.

Excluded endpoints are not parity defects. If one already exists, document whether it is removed,
disabled, or left unreachable; never count it silently as completed parity.

## Rules for every task

- Read `llms/endpoint-reference.md`, `llms/database-reference.sql`, `pkg/responder/`,
  `internal/transport/`, and the owning module before editing an endpoint.
- Compare behavior with the local Pocket ID checkout at tag `v2.14.0`; use the public API docs to
  confirm the published contract without fetching or vendoring upstream code.
- Keep an endpoint matrix containing request, response, envelope, status, headers, auth, and test
  evidence. A route is complete only when its matrix row and live test agree.
- Create and send Yaak MCP requests for every HTTP task. Use a clean cookie jar for anonymous
  checks. If Yaak cannot encode a nested mutation body, retain the Yaak request and use the
  documented curl fallback for the live mutation check.
- Whenever an HTTP contract changes, update the matching Yaak request immediately. Keep its name,
  method, URL, path/query parameters, headers, authentication, body, and expected response aligned
  with the code. Re-send the request before marking the task complete. Delete or mark requests for
  removed/excluded endpoints so stale requests do not imply supported API surface.
- Keep each task in one atomic commit containing only the task, its tests, and required docs.
- Run focused tests, then `task test`, `task lint`, and `task check` before marking a task done.
- Run focused tests and Yaak requests with explicit timeouts and fail-fast behavior. Stop on the
  first failure or hang; do not wait for a long default integration/container timeout.
- If local Pocket ID behavior, a database field, or an endpoint contract is ambiguous, record the
  evidence and ask the project owner for confirmation. Do not guess or update the matrix/Yaak
  expectation until the dependent decision is confirmed.
- Never commit real reset tokens, TOTP seeds, webhook secrets, or other credentials.
- New recoverable encrypted values must be written as `enc:<ciphertext>` by `pkg/crypto`; do not
  add local encryption formats or encrypt values that only need one-way verification.
- An unprefixed encrypted value is invalid. Do not add a legacy decoder or backward-compatibility
  branch to make it readable.

## Context and completion rule

This file decides whether a change belongs in the port. Read it before creating a task. Each task
must leave a focused diff, tests for changed behavior, updated status metadata, and a suggested
atomic commit message. Compilation alone is not evidence that an HTTP contract is complete.
