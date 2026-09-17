---
status: done
updated: 2026-09-18
owner: tango-remediation
---

# Phase 4 — Validation and Error Safety

Prerequisite: Phase 3 done.

## Task 4.1 — Normalize request validation

Inventory the manual checks such as `missing code`, `session_id is required`, multipart field
checks, and invalid body parsing. For ordinary JSON/form endpoints, create request DTOs with
`Validate()` through `pkg/validate` and map errors to the 422 envelope.

Protocol-required parsing may stay manual, but it must have a documented status/error contract
and invalid-input tests.

Commit: `refactor: normalize request validation`

## Task 4.2 — Stabilize public error mapping

Replace direct `err.Error()` responses on public surfaces with sentinel/typed error mapping.
Provider, database, crypto, and implementation details must never leave through a response.

Keep the error codes/messages that are part of a protocol contract, especially OAuth/OIDC. Add
tests proving internal wrapped errors are invisible to clients.

Commit: `fix: prevent internal error disclosure`

## Task 4.3 — Review security and redaction boundaries

Audit responses, loggers, audit payloads, Yaak bodies, and test fixtures for passwords, reset
tokens, TOTP seeds/codes, recovery codes, API keys, webhook secrets, provider tokens, private
keys, and ciphertext.

Add regression tests for one-time secret responses, redacted list/get responses, log redaction,
and audit payloads.

Commit: `test: harden secret redaction boundaries`

## Phase acceptance criteria

- Request validation uses `pkg/validate` on non-protocol endpoints.
- No internal error detail appears in public responses.
- Protocol errors stay RFC- and endpoint-contract compliant.
- No secret or ciphertext appears in logs, audit, Yaak, or forbidden responses.
