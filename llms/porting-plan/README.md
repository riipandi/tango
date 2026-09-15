---
status: planned
updated: 2026-09-15
---

# Pocket ID Authentication Porting Plan

This folder is the active execution plan for porting Pocket ID v2.14.0 into tango. Files in
`llms/archived/` are historical context only and their completion status is not evidence of parity.
The architecture target preserves `cmd/launcher`, keeps the existing flat `internal/` package
layout, and applies the main simplification inside `modules/`.

## Target

Match every in-scope upstream endpoint's method, path, parameters, request body, authentication
headers, response status, response headers, and error behavior. JSON responses use `pkg/responder`
unless the protocol requires a bare OAuth/OIDC document, well-known document, health response, or
binary response.

Recoverable encrypted values use `pkg/crypto` and the canonical `enc:<ciphertext>` format. Values
that only need verification remain one-way hashes.

The implementation is Postgres-only, follows existing module boundaries, and avoids generic
abstractions without a current caller. Do not copy Pocket ID's Gin, GORM, database, or response
types into tango.

## Execution order

1. [Scope and rules](./00-scope.md)
2. [Excluded-feature cleanup](./01-excluded-cleanup.md)
3. [Target architecture](./architecture.md)
4. [Contract baseline](./01-baseline.md)
5. [Transport and responder](./02-transport.md)
6. [Encrypted value prefix](./08-crypto-prefix.md)
7. [Upstream endpoint parity](./03-endpoint-parity.md)
8. [Password authentication](./04-password.md)
9. [TOTP MFA](./05-mfa-totp.md)
10. [Webhooks](./06-webhooks.md)
11. [Final review and gate](./07-final-gate.md)

Every numbered task is intended to be one atomic commit. Agents must update the relevant status,
contract matrix, tests, and Yaak evidence in the same task. Suggested commit messages are included
in each file. Every file in this folder starts with `status` and `updated` metadata; agents must
update both when work begins or finishes.
