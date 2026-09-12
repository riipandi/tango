# Pocket ID Porting Plan

Phased plan for porting Pocket ID (https://github.com/pocket-id/pocket-id) features onto the
tango foundation. Reference: https://pocket-id.org/docs/api

## Phase Index

| Phase | File                                            | Scope                                          | Status  | Updated    |
| ----- | ----------------------------------------------- | ---------------------------------------------- | ------- | ---------- |
| 1     | [phase-01-auth-core.md](./phase-01-auth-core.md)  | Auth middleware, session, password, account     | planned | 2026-09-12 |
| 2     | [phase-02-identity-admin.md](./phase-02-identity-admin.md) | User groups, custom claims, audit API, admin CRUD | planned | 2026-09-12 |
| 3     | [phase-03-jwks-wellknown.md](./phase-03-jwks-wellknown.md) | JWKS provider, discovery endpoints   | planned | 2026-09-12 |
| 4     | [phase-04-oidc-provider.md](./phase-04-oidc-provider.md)   | Authorize (PKCE), token, userinfo    | planned | 2026-09-12 |
| 5     | [phase-05-passkeys-signin.md](./phase-05-passkeys-signin.md) | WebAuthn, device login, one-time access, signup | planned | 2026-09-12 |
| 6     | [phase-06-apikeys-ratelimit.md](./phase-06-apikeys-ratelimit.md) | API keys, resource APIs, rate limiter | planned | 2026-09-12 |
| 7     | [phase-07-jobs-webhooks.md](./phase-07-jobs-webhooks.md)   | Antree consumers, webhooks, scheduler | planned | 2026-09-12 |
| 8     | [phase-08-sync-storage.md](./phase-08-sync-storage.md)     | LDAP, SCIM, S3 storage, app images    | planned | 2026-09-12 |

## Status Protocol (mandatory)

Every phase file carries YAML front matter and a progress log. When work on a phase happens:

1. Update `status:` in front matter: `planned` → `in_progress` → `done` (or `blocked`, with the
   blocker named in the progress log).
2. Update `updated:` (format `YYYY-MM-DD`) on the same edit.
3. Tick checkboxes (`- [ ]` → `- [x]`) as tasks complete — one commit per completed task group.
4. Append one line to **Progress Log**: `- 2026-09-12 <what happened>`.
5. Update the matching row in the phase index above (Status + Updated columns).

A checkbox may only be ticked when its validation command passes.

## Language & Style Rules (all phases)

- Chat: Indonesian; code, comments, commit messages, and these docs: English.
- No abbreviated folder names; no section separators; comments explain *why*.
- **Comments: always concise, avoid unnecessary ones.** Never restate what the code or identifier
  already says (`// db is the database`); no doc comment on obvious setters/getters. When in doubt,
  delete the comment. Keep only race invariants, protocols, and non-obvious constraints.

## Go >= 1.26 Idioms (mandatory, toolchain is Go 1.27)

- **Stdlib `uuid`** (`uuid.NewV7()`, `uuid.Parse`) — never `github.com/google/uuid`.
- **`encoding/json/v2`** (alias `jsonv2`) with `omitzero` for optional fields — never v1 in new code;
  migrate v1 call sites on touch.
- `for range n` instead of `for i := 0; i < n; i++`.
- `min`/`max` builtins, `slices`/`maps` stdlib packages over hand-rolled loops.
- `any` over `interface{}`; generics where they remove boilerplate.
- `t.Context()` in tests; `context.WithoutCancel` instead of detached goroutine contexts.
- `errors.Join` for multi-error aggregation.
- Loop variables are per-iteration (1.22+) — no `x := x` copies.
- `time.Until`/`time.Since` over manual subtraction.

## Standing Conventions (all phases)

- Module layout: `modules/<area>/<feature>/{schema,service,store,handler}.go`; exactly one store
  file named `store.go` (Postgres). No memory-store implementations, ever.
- Tests run against real Postgres via `pkg/testutils.StartPostgres` (testcontainers).
- Schema is owned by `database/migrations/` — never embed or auto-create.
- Typed IDs per module `schema.go` (`go.jetify.com/typeid`); no shared domain vocabulary package.
- `pkg/` packages have zero `internal/` dependencies.
- Responses via `pkg/responder` (envelope + pagination); errors via typed module errors mapped to
  HTTP in transport.
- Validation gate for every task: `go test ./...`, `go test -tags debug ./...`,
  `go test -tags release ./...` (0 FAIL), `golangci-lint run ./...` (0 issues), `gofmt` clean.

## Pocket ID Source Map (porting reference)

| Pocket ID (`backend/internal/...`) | Tango target                    |
| ---------------------------------- | ------------------------------- |
| `middleware`, `apikey`             | `internal/transport/middleware`, `modules/identity/apikey` |
| `webauthn`, `devicelogin`, `onetimeaccess`, `usersignup`, `emailverification` | `modules/identity/*` |
| `oidc`, `api` (apis resource)      | `modules/federation/oidc`, `modules/identity/apiaccess` |
| `appconfig`, `auditlogs`, `storage`, `email`, `job` | `modules/appconfig`, `modules/auditlog`, `internal/storage`, `internal/mailer`, `pkg/antree` |
| `ldapsync`, `scimsync`             | `modules/identity/ldapsync`, `modules/federation/scimsync` |
