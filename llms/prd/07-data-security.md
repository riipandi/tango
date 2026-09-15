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
  is sufficient. Encrypt values that must be recovered, such as active TOTP seeds, using
  `pkg/crypto` with the `enc:` prefix.
- New tables require constraints, indexes, foreign keys, ownership rules, and cleanup behavior.

## Security

- Fail closed for missing auth, missing guards, invalid session state, and unavailable dependencies.
- Apply least privilege to user, admin, API-key, OIDC-client, MFA, and webhook operations.
- Use generic public responses where account existence must not be disclosed.
- Do not log passwords, reset tokens, TOTP seeds/codes, recovery codes, API keys, or webhook secrets.
- Keep security-sensitive actions auditable without recording secret values.
- Use bounded timeouts and rate limits for credential, MFA, and webhook operations.
- Every new recoverable encrypted database value starts with exactly `enc:`. There is one bounded
  compatibility-read path for known legacy ciphertext; it never accepts plaintext as encrypted
  data and rewrites successful legacy reads when safe.

## Maintainability

- Use four application boundaries: `modules/identity`, `modules/federation`, `modules/admin`, and
  `modules/webhook`. Keep `schema`, `service`, `store`, and `handler` files where they make a
  boundary clearer, but do not register each feature as an independent plugin.
- Keep `cmd/launcher` unchanged as the entrypoint and keep the existing flat `internal/` packages.
- Keep `internal/registry` as a concrete composition root; do not create `internal/app` or
  `internal/platform`.
- Keep `pkg/` independent of `internal/`.
- Prefer direct, focused services and stores over speculative interfaces.
- Keep comments concise and limited to non-obvious invariants, protocols, security rules,
  compatibility constraints, or side effects.
- Remove dead LDAP and Application Images paths rather than keeping dormant abstractions.
- Preserve behavior with tests before simplifying touched modules.

## Dependency direction

```text
cmd/launcher
    -> internal/registry
    -> internal/transport
    -> modules
modules
    -> narrow ports and existing internal infrastructure
identity <-> federation/admin only through consumer-side ports
webhook   -> queue/outbox ports, never concrete identity/federation services
```

Domain services must not store routers, `http.Handler`, middleware guards, or responder status
errors. HTTP handlers may translate module errors into the standard response envelope.
