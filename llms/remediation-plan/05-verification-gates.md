---
status: draft
updated: 2026-09-18
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

### Evidence (2026-09-18)

- Docker daemon `29.4.0`; dev stack healthy (`docker compose up -d`): pgsql, mailpit, redis,
  nginx, silo, and the upstream `pocketid` parity instance.
- `gotestsum` (dev) and `golangci-lint` on PATH; testcontainers Postgres/Mailpit spin up.
- Every suite command pins `-count=1 -failfast -timeout <600-1200s>`; no test was skipped
  to force the gate green.
- Reproducible commands:
  - `task test:go -- ./...` (release-tag suite via gotestsum);
  - `go test -tags release -count=1 -failfast -timeout 1200s ./...`;
  - `go test -tags debug -count=1 -failfast -timeout 900s ./cmd/... ./database/...`;
  - `pnpm exec vitest run` (76 tests, 9 files);
  - `task lint`, `task check`, `task typecheck`.

## Task 5.2 — Run database and race verification

Run with explicit timeouts:

- fresh migration up/down/up;
- schema contract and migration tests;
- the full Go release/debug suites;
- race tests for identity MFA, webhook, queue, and the changed stores;
- `go vet`, `gofmt`, `task lint`, `task check`, and the typecheck.

Fix failure sources one at a time; every fix is its own atomic commit.

Commit: `test: verify database and race gates`

### Evidence (2026-09-18)

- Fresh migration up/down/up: `TestMigrationsLifecycle` PASS (3.9s), schema contract suite
  (`TestSecretEncConstraints`, `TestLoginUniquenessIsNormalized`, `TestTokenExpiryIsEnforced`,
  `TestOutboxAtomicity`, `TestQueueNotifyAfterCommit`, `TestCleanupIndexesExist`) PASS,
  `TestFreshSchemaHasNoObsoleteTables` PASS.
- Race suite (`-race -failfast`): webauthn, webhook, queue, recovery, scimsync, oidc, apikey,
  apiaccess — no data races, no failures.
- Full release suite: 42 packages ok. Debug suite (`./cmd/... ./database/...`): ok.
- Frontend: vitest 76/76 (9 files). `task lint` 0 issues, `task check` clean, `task typecheck`
  clean.
- Fixes made while verifying (each verified before commit): staticcheck QF1001 in
  `internal/registry/architecture_test.go` (De Morgan rewrite), govet shadow in
  `modules/identity/devicelogin/handler_test.go`, oxfmt on `api/client/tests/modules.test.ts`.

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

### Evidence (2026-09-18)

Live comparison against the upstream parity instance (`tango-pocketid-1`, host port 1411) with
a freshly migrated tango build on :3081 (`tango_verify` database, all 11 migrations applied):

- Method/path/auth-boundary statuses match for the admin and account surfaces (users, oidc
  clients, api keys, audit logs, users/me — 401 unauthenticated; device login request creation
  201 both sides).
- Discovery: field sets now match except the recorded deviation (see below); JWKS documents
  match including the `use: sig` marker after the fix below.
- Fixes made while verifying: JWKS keys now carry `use: sig`
  (`modules/federation/jwks/service.go`), and the discovery document adds
  `request_parameter_supported: true` (`modules/federation/discovery/discovery.go`) — both
  verified live after rebuild and covered by unit tests.
- Remaining live diffs classified as deliberate deviations, recorded in
  `llms/tango-deviations.md` ("OIDC protocol failure mapping"): invalid_client 401 mapping,
  /authorize 400 for malformed requests, PAR 400 before client auth, userinfo path without the
  root alias, omitted `service_documentation`. Tango-only surfaces (auth, account sessions,
  forgot-password) 404 on upstream by design.

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
