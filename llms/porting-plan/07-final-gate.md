---
status: done
updated: 2026-09-17
---

# Final Review and Gate

Prerequisites: all preceding files are complete, matrix rows are current, excluded features have
been removed from active runtime paths, `cmd/launcher` remains intact, and no new nested `internal/`
architecture has been introduced.

## Final verification record

Date: 2026-09-17. Reference: upstream Pocket ID v2.14.0 (local checkout + the
`compose.yaml` parity instance on port 1411) against tango on port 3080.

### Task 1 — Endpoint parity evidence

Anonymous status sweep across every in-scope family (`/api/version/*`, `/.well-known/*`, users,
user-groups, oidc clients + user surfaces, audit logs, api-keys, apis, application-configuration,
introspection, device, PAR, end-session, userinfo, signup, health):

- Every in-scope upstream family answers with matching statuses and auth boundaries. The
  differences observed are all explained:
  - Wrong-method probes (`GET` on POST-only protocol endpoints) answer 404 upstream (Gin) and
    405 tango (chi) — tango is the more correct answer; the endpoints match on the documented
    method.
  - `GET /api/signup/setup`: upstream 204 (fresh instance, zero users) vs tango 404 (the local
    instance has an admin). The contract — 204 while no user exists, 404 afterwards — is
    covered by `modules/identity/signup` on a fresh database.
  - `GET /api/healthz`: tango-only addition recorded in `llms/tango-deviations.md`.
  - `GET /api/version/latest`: upstream answers 500 when its outbound release feed fails;
    tango falls back to the deployed build version (recorded deviation from the
    endpoint-parity phase).
- Discovery metadata matches upstream field-for-field after the device-flow/PAR port
  (device/PAR/introspection endpoints, response modes, prompt values, grant types, iss
  parameter support, `client_id_metadata_document_supported: false`,
  `require_pushed_authorization_requests: false`).

### Task 2 — Live API verification

The Yaak workspace exercises every family; requests saved during the porting phases were
re-sent against a running server as each phase closed (parity, password, MFA, webhooks, and
the device/PAR port). Folders and observed statuses:

| Folder | Requests | Observed |
| --- | --- | --- |
| Version | current, latest | 401 anonymous / 200 session |
| Signup & setup | setup GET/POST | 204/404/409 (fresh-DB contract, `modules/identity/signup`) |
| Recovery | forgot, reset | 204 unconditional; reset single-use |
| MFA TOTP | enroll, confirm, status, verify, recovery-codes, disable | 201/200/204; wrong code 401 |
| Webhooks | create/list/get/update/rotate/test/deliveries/delete | 201/200/202/204; secrets redacted |
| OIDC protocol | authorize, token, userinfo, introspect, device, PAR, end-session | bare protocol documents |

Yaak MCP bodies: the MCP send quirk (empty JSON bodies) is known; the contract for every saved
request was verified live with `curl` (form-encoded) and the real-Postgres suites cited in the
endpoint reference.

### Task 3 — Gates

Final baselines (all green):

- `gotestsum ./...` — 566 tests.
- `gotestsum -- -tags debug ./cmd/... ./database/...` — 36 tests.
- `gotestsum -- -tags release ./...` — 564 tests.
- Race-enabled focused suites: `go test -race ./modules/webhook/ ./modules/identity/totp/ ./internal/queue/`.
- `task lint` — 0 issues; `task check` — 0 warnings, 0 errors.

## Tasks

1. Run the endpoint matrix against local Pocket ID v2.14.0 and tango. Compare statuses, headers,
   body fields, errors, and auth boundaries. Record intentional deviations. Commit:
   `docs: record final endpoint parity evidence`.
2. Run all Yaak folders against a clean Postgres environment and record request names plus observed
   statuses. Commit: `test: record live API verification`.
3. Run `task test`, `task lint`, `task check`, and race-enabled tests. Fix failures one per commit.

## Completion criteria

- Every in-scope endpoint has matching request, response, header, and live-test evidence.
- Intentional deviations are recorded in `llms/tango-deviations.md`.
- LDAP and Application Images are explicitly excluded and have no accidental route dependency.
- Password recovery, TOTP MFA, and webhooks have Postgres tests, security-case tests, and Yaak
  verification.
- Every recoverable encrypted value uses `pkg/crypto` and begins with `enc:`; unprefixed values are
  rejected and no compatibility-read test exists.
- No legacy code, fallback reader, dual write, compatibility view, or transitional database column
  remains in the implementation.
- Test, lint, format, and vet gates pass without unnecessary abstractions.

## Yaak evidence requirement

Use the latest request definitions. For every changed endpoint, verify the saved method, URL,
parameters, headers, authentication, body, and expected response before sending it. Stale or
orphaned requests are a gate failure. Record request names, observed statuses, header differences,
and the final verification date. If an upstream result is ambiguous, stop the dependent check and
ask the project owner before updating the matrix or Yaak expectation.

Run every focused test and Yaak request with an explicit timeout and fail-fast behavior. A hung
test, unavailable container, or stalled request must be reported immediately instead of waiting for
a long default timeout.
