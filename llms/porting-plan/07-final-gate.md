---
status: planned
updated: 2026-09-15
---

# Final Review and Gate

Prerequisites: all preceding files are complete, matrix rows are current, excluded features have
been removed from active runtime paths, `cmd/launcher` remains intact, and no new nested `internal/`
architecture has been introduced.

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
