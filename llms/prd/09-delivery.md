---
status: draft
updated: 2026-09-15
owner: tango-auth-porting
---

# Delivery, Dependencies, and Risks

## Delivery sequence

1. Remove LDAP and Application Images paths and update scope documentation.
2. Record and test the target modular-monolith boundaries.
3. Build the endpoint contract matrix and parity test helpers.
4. Normalize transport, responder, validation, and headers.
5. Replace generic registry wiring with an explicit runtime while preserving `cmd/launcher` and
   flat `internal/` packages.
6. Consolidate application behavior into identity, federation, admin, and webhook modules.
7. Close upstream endpoint parity by family.
8. Complete password authentication and recovery.
9. Add TOTP MFA and session assurance.
10. Harden and verify webhooks and transactional event delivery.
11. Apply and verify the `pkg/crypto` `enc:` contract for all recoverable values.
12. Run final matrix, Yaak, security, database, architecture, and full-project gates.

Each task is an atomic change with focused tests, updated docs, updated Yaak requests, and a suggested
commit message. Do not combine unrelated endpoint families.

## Dependencies

- Local Pocket ID v2.14.0 source checkout.
- `llms/endpoint-reference.md` and `llms/database-reference.sql`.
- `llms/porting-plan/architecture.md` and this PRD.
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
| Migration edit does not affect existing databases | Add new migration and test fresh plus upgraded schemas |
| OIDC protocol regression | Preserve bare protocol responses and run end-to-end authorize/token checks |
| MFA creates an overcomplicated policy system | Keep policy local to password/session/multifactor services |
| Webhook retries duplicate side effects | Record attempts, expose delivery IDs, and document receiver idempotency |
| Architecture cleanup changes public behavior | Make runtime and module changes behavior-preserving; verify each route with Yaak |
| Flattening `internal/` hides too much composition logic | Keep one explicit composition root in `internal/registry` and test route/lifecycle ownership |
| Encrypted values use mixed formats | Make `pkg/crypto` emit `enc:` and test every recoverable-value owner plus legacy reads |
| Agent guesses ambiguous upstream behavior | Require evidence and owner confirmation before dependent code or contract changes |
| Tests wait on long defaults | Use focused commands, explicit timeouts, and fail-fast execution before broad gates |

## Open decisions before implementation

- Final custom route names for password recovery and TOTP management.
- Exact MFA policy scope: all users, selected users, or administrator-enforced configuration.
- TOTP algorithm/digits/period and permitted clock skew.
- Password reset token lifetime and session invalidation scope.
- Webhook signature header format and retry schedule.
- Whether any shared storage code remains after Application Images removal.
