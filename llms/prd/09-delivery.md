---
status: draft
updated: 2026-09-15
owner: tango-auth-porting
---

# Delivery, Dependencies, and Risks

## Delivery sequence

1. Remove LDAP and Application Images paths and update scope documentation.
2. Define and test the final PostgreSQL ownership, constraints, and transaction boundaries.
3. Record and test the target modular-monolith boundaries.
4. Build the endpoint contract matrix and parity test helpers.
5. Normalize transport, responder, validation, and headers.
6. Replace generic registry wiring with an explicit runtime while preserving `cmd/launcher` and
   flat `internal/` packages.
7. Consolidate application behavior into identity, federation, admin, and webhook modules.
8. Apply and verify the strict `pkg/crypto` `enc:` contract for all recoverable values.
9. Close upstream endpoint parity by family.
10. Complete password authentication and recovery.
11. Add TOTP MFA and session assurance.
12. Harden and verify webhooks and transactional event delivery.
13. Run final matrix, Yaak, security, database, architecture, and full-project gates.

Each task is an atomic change with focused tests, updated docs, updated Yaak requests, and a suggested
commit message. Do not combine unrelated endpoint families.

## Dependencies

- Local Pocket ID v2.14.0 source checkout.
- `llms/endpoint-reference.md` and `llms/database-reference.sql`.
- `llms/porting-plan/architecture.md` and this PRD.
- `llms/porting-plan/database.md` and `llms/prd/12-database.md`.
- `pkg/responder`, `pkg/validate`, `internal/transport`, `internal/datastore`, queue, mailer,
  `pkg/crypto`, audit, and session services.
- Postgres testcontainers and a running tango server.
- Yaak MCP plus a controlled webhook receiver.

## Risks and mitigations

| Risk | Mitigation |
| --- | --- |
| Historical status is inaccurate | Rebuild matrix from source and live behavior; do not trust archived checkboxes |
| Upstream response shape leaks into tango | Enforce responder review in every handler task |
| Yaak requests become stale | Treat request update/re-send as a task acceptance criterion |
| LDAP removal breaks shared identity code | Inventory callers first; remove only LDAP-specific columns and wiring |
| Application Images removal breaks profile/email storage | Trace callers and retain only shared storage paths |
| Secret leakage in tests/logs | Redact fixtures, use disposable values, inspect responses and logs |
| Migration shape is incomplete | Test a fresh database and inspect final constraints/indexes directly |
| OIDC protocol regression | Preserve bare protocol responses and run end-to-end authorize/token checks |
| MFA creates an overcomplicated policy system | Keep policy local to password/session/multifactor services |
| Webhook retries duplicate side effects | Record attempts, expose delivery IDs, and document receiver idempotency |
| Architecture cleanup changes public behavior | Make runtime and module changes behavior-preserving; verify each route with Yaak |
| Flattening `internal/` hides too much composition logic | Keep one explicit composition root in `internal/registry` and test route/lifecycle ownership |
| Encrypted values use mixed formats | Make `pkg/crypto` emit `enc:` and reject every unprefixed value |
| Agent guesses ambiguous upstream behavior | Require evidence and owner confirmation before dependent code or contract changes |
| Tests wait on long defaults | Use focused commands, explicit timeouts, and fail-fast execution before broad gates |
| Compatibility code grows around old behavior | Reject old formats and remove obsolete paths instead of adding fallback branches |

## Open decisions before implementation

- Final custom route names for password recovery and TOTP management.
- Exact MFA policy scope: all users, selected users, or administrator-enforced configuration.
- TOTP algorithm/digits/period and permitted clock skew.
- Password reset token lifetime and session invalidation scope.
- Webhook signature header format and retry schedule.
- Whether any shared storage code remains after Application Images removal.
