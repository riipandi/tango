---
status: draft
updated: 2026-09-15
owner: tango-auth-porting
---

# Verification and Release Acceptance

## Test layers

1. **Unit tests**: pure validation, crypto prefix, signature, mapping, and policy behavior.
2. **Module tests**: service/store behavior with real Postgres and transaction checks.
3. **Handler tests**: request decoding, responder envelope, status, auth, cookies, and headers.
4. **Route tests**: public method/path mounts and middleware order.
5. **Yaak live tests**: real request/response behavior against a running tango server.
6. **Full gate**: `task test`, `task lint`, and `task check`, including debug/release suites.

## Yaak rules

- Maintain one request per endpoint and keep names in `<METHOD> <path>` form.
- Update method, URL, parameters, headers, authentication, body, and expected response whenever
  implementation changes.
- Re-send changed requests before marking a task done.
- Use a clean cookie jar for anonymous checks.
- Do not save live secrets, reset tokens, TOTP seeds, recovery codes, or webhook secrets.
- Remove or mark requests for removed/excluded routes.

## Release acceptance criteria

- Every in-scope endpoint has a matrix row, focused test reference, and current Yaak evidence.
- All documented deviations are intentional, reviewed, and recorded.
- No LDAP or Application Images route is mounted or represented as supported.
- Password recovery and TOTP MFA pass all security cases.
- Webhook signatures verify against exact delivered bytes and delivery attempts are observable.
- Database migrations apply cleanly to a fresh Postgres instance and existing test data remains
  consistent after cleanup migrations.
- Full test, lint, format, vet, and race checks pass.
- Focused and live checks fail fast with explicit timeouts; hangs, unavailable containers, and
  stalled Yaak requests are reported as the first failure.
- Ambiguous upstream behavior is not guessed. The agent records evidence and asks the project owner
  for confirmation before changing the dependent implementation, matrix, or Yaak request.
