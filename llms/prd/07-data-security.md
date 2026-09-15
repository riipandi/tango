---
status: draft
updated: 2026-09-15
owner: tango-auth-porting
---

# Data, Security, and Maintainability Requirements

## Persistence

- PostgreSQL is the only supported database.
- Schema changes are new files under `database/migrations/`; never rewrite an applied migration.
- Stores use `internal/datastore` and transaction boundaries are explicit.
- Typed IDs are used at URL and cross-module boundaries; stores convert them to UUID values.
- Tokens, reset codes, recovery codes, and client secrets are stored as hashes where verification
  is sufficient. Encrypt values that must be recovered, such as active TOTP seeds.
- New tables require constraints, indexes, foreign keys, ownership rules, and cleanup behavior.

## Security

- Fail closed for missing auth, missing guards, invalid session state, and unavailable dependencies.
- Apply least privilege to user, admin, API-key, OIDC-client, MFA, and webhook operations.
- Use generic public responses where account existence must not be disclosed.
- Do not log passwords, reset tokens, TOTP seeds/codes, recovery codes, API keys, or webhook secrets.
- Keep security-sensitive actions auditable without recording secret values.
- Use bounded timeouts and rate limits for credential, MFA, and webhook operations.

## Maintainability

- Follow `modules/<area>/<feature>/{schema,service,store,handler}.go`.
- Keep `pkg/` independent of `internal/`.
- Prefer direct, focused services and stores over speculative interfaces.
- Keep comments concise and limited to non-obvious invariants, protocols, security rules,
  compatibility constraints, or side effects.
- Remove dead LDAP and Application Images paths rather than keeping dormant abstractions.
- Preserve behavior with tests before simplifying touched modules.
