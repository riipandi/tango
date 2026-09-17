---
status: draft
updated: 2026-09-17
owner: tango-remediation
---

# Phase 5 — Runtime Verification and Live Contract

Prerequisite: every code/schema/contract task of the previous phases is done.

## Task 5.1 — Repair the focused test execution environment

Make sure tests run in an environment that provides:

- a Docker daemon for the testcontainers Postgres/Mailpit;
- HTTP listeners for `httptest`;
- explicit timeout and fail-fast behavior.

Do not turn tests into skips just to make the gate green. If the sandbox cannot support it, run
on a matching host/devbox and record the commands and results.

Commit: `test: make remediation verification reproducible`

## Task 5.2 — Run database and race verification

Run with explicit timeouts:

- fresh migration up/down/up;
- schema contract and migration tests;
- the full Go release/debug suites;
- race tests for identity MFA, webhook, queue, and the changed stores;
- `go vet`, `gofmt`, `task lint`, `task check`, and the typecheck.

Fix failure sources one at a time; every fix is its own atomic commit.

Commit: `test: verify database and race gates`

## Task 5.3 — Re-run the endpoint matrix

Compare tango against the local upstream Pocket ID v2.14.0 for every in-scope endpoint:

- method/path;
- query/path parameters;
- request encoding;
- auth boundary and headers;
- status, headers, envelope, bare response, and error fields;
- pagination and TypeID behavior.

Record only intentional deviations in `llms/tango-deviations.md`. Never adjust the matrix to
paper over a defect without fixing the implementation or getting an owner decision.

Commit: `docs: refresh endpoint parity evidence`

## Task 5.4 — Re-send Yaak live verification

Use a clean cookie jar and a fresh Postgres. Re-send every changed request, at minimum:

- password/recovery;
- TOTP enrollment/confirm/verify/recovery/disable;
- the OIDC client secret lifecycle;
- device/PAR/token/userinfo;
- SCIM/JWKS;
- webhook CRUD/rotation/test/delivery/retry;
- excluded route negative checks.

Record the request names, observed statuses, header differences, and verification dates. Never
store secrets, tokens, recovery codes, seeds, or ciphertext.

Commit: `test: record live remediation verification`

## Phase acceptance criteria

- The full gates really run in a matching environment.
- A fresh database holds only the final schema.
- Race tests and security cases pass.
- Every in-scope endpoint has a current evidence matrix and Yaak run.
- No stale request or undocumented deviation remains.
