# Pocket ID Porting Plan

Phased plan for porting Pocket ID (<https://github.com/pocket-id/pocket-id>) features onto the
tango foundation.

References:

- <https://pocket-id.org/docs/api>
- <https://pocket-id.org/swagger.yaml>
- [database-reference.sql](./database-reference.sql) — live schema dump of upstream Pocket ID
  v2.14.0 (cut-off: migration `20260814120000_api_client_access`, dumped 2026-09-14).
- Local upstream clone: `~/Developer/github.com/pocket-id/pocket-id` @ tag `v2.14.0` — read
  upstream source from this clone instead of fetching files from the web.

## Phase Index

| Phase | File                                                             | Scope                                             | Status      | Updated    |
| ----- | ---------------------------------------------------------------- | ------------------------------------------------- | ----------- | ---------- |
| 1     | [phase-01-auth-core.md](./phase-01-auth-core.md)                 | Auth middleware, session, password, account       | done        | 2026-09-12 |
| 2     | [phase-02-identity-admin.md](./phase-02-identity-admin.md)       | User groups, custom claims, audit API, admin CRUD | done        | 2026-09-12 |
| 3     | [phase-03-jwks-wellknown.md](./phase-03-jwks-wellknown.md)       | JWKS provider, discovery endpoints                | done        | 2026-09-13 |
| 4     | [phase-04-oidc-provider.md](./phase-04-oidc-provider.md)         | Authorize (PKCE), token, userinfo                 | done        | 2026-09-13 |
| 5     | [phase-05-passkeys-signin.md](./phase-05-passkeys-signin.md)     | WebAuthn, device login, one-time access, signup   | done        | 2026-09-13 |
| 6     | [phase-06-apikeys-ratelimit.md](./phase-06-apikeys-ratelimit.md) | API keys, resource APIs, rate limiter             | done        | 2026-09-14 |
| 7     | [phase-07-jobs-webhooks.md](./phase-07-jobs-webhooks.md)         | Antree consumers, webhooks, scheduler             | done        | 2026-09-14 |
| 8     | [phase-08-sync-storage.md](./phase-08-sync-storage.md)           | LDAP, SCIM, S3 storage, app images                | done    | 2026-09-14 |

## Architecture Improvement Plan (post-port)

Refactor plan from the 2026-09-15 architecture analysis — behavior-preserving dedup and module
separation work. Index: [architecture-improvement.md](./architecture-improvement.md) with phases
`arch-phase-01` … `arch-phase-06` (same status protocol as the porting phases; no Yaak checks
required unless a task touches route mounting).

## Status Protocol (mandatory)

Every phase file carries YAML front matter and a progress log. When work on a phase happens:

1. Update `status:` in front matter: `planned` → `in_progress` → `done` (or `blocked`, with the
   blocker named in the progress log).
2. Update `updated:` (format `YYYY-MM-DD`) on the same edit.
3. Tick checkboxes (`- [ ]` → `- [x]`) as tasks complete — one commit per completed task group.
4. For every HTTP feature: create the requests in Yaak (MCP) and send them against the running
   server **before** ticking the checkbox — see Yaak Live Verification below.
5. Append one line to **Progress Log**: `- 2026-09-12 <what happened>`.
6. Update the matching row in the phase index above (Status + Updated columns).

A checkbox may only be ticked when its validation command passes.

## Language & Style Rules (all phases)

- Chat: Indonesian; code, comments, commit messages, and these docs: English.
- No abbreviated folder names; no section separators; comments explain _why_.
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

## Request Validation (mandatory, all phases)

Library: `github.com/go-ozzo/ozzo-validation/v4` (>= v4.4.1, code-first rules — no validation tags).
Chosen for: rules are compiled code (no stringly-typed tag drift), no tag collision with
`encoding/json/v2` `omitzero`, context-aware rules, revival-maintained since Aug 2026.

- Rules live next to the DTO: `func (r CreateXRequest) Validate() error` with ozzo rules.
- `pkg/validate` (zero `internal/` deps): decode+validate helper for handlers (jsonv2 read, then
  `Validate()`), maps `validation.Errors` to `[]FieldError{Field, Message}`.
- Handler flow: decode+validate once → failure responds 422 `validation_failed` with field errors
  via `pkg/responder`; no ad-hoc `if req.X == ""` guards in handlers.
- Query/path params validated with ozzo at the handler (`validation.Required`, `validation.Match`).
- Rule values (lengths, formats) mirror Pocket ID semantics, not gin binding tags.

## Yaak Live Verification (mandatory for HTTP features)

Every finished HTTP feature gets a live check: create the requests in Yaak via MCP, send them
against the running server (`/tmp/tango serve --port 3080` over the compose Postgres), and record
the observed status codes in the phase progress log. A checkbox for an HTTP feature may only be
ticked after its Yaak check passes.

Organization mirrors the upstream spec (<https://pocket-id.org/docs/api>, source
`https://pocket-id.org/swagger.yaml`) — one Yaak folder per upstream tag:

| Yaak folder (upstream tag) | Phase    | Coverage                                                                                           |
| -------------------------- | -------- | -------------------------------------------------------------------------------------------------- |
| Users                      | 1, 2, 5  | `/api/users*`, profile, webauthn-credentials, one-time access, email verification, signup          |
| User Groups                | 2        | `/api/user-groups*`                                                                                |
| Custom Claims              | 2        | `/api/custom-claims*`                                                                              |
| Audit Logs                 | 2        | `/api/audit-logs*`                                                                                 |
| Well Known                 | 3        | `/.well-known/jwks.json`, `/.well-known/openid-configuration`                                      |
| OIDC                       | 4        | `/api/oidc/clients*`, introspect, end-session, authorized clients                                  |
| OAuth                      | 4        | `/api/oidc/token`, `/api/oidc/userinfo`                                                            |
| API Keys                   | 6        | `/api/api-keys*`                                                                                   |
| APIs                       | 6        | `/api/apis*`, `/api/api-access*`                                                                   |
| Device Login               | 5        | `/api/device-login*`                                                                               |
| Application Configuration  | 8        | `/api/application-configuration*` (incl. `sync-ldap`, `test-email`)                                |
| Application Images         | 8        | `/api/application-images*`                                                                         |
| SCIM                       | 8        | `/api/scim/service-provider*`                                                                      |
| Version                    | 1 (done) | `/api/version/*`, `/healthz`                                                                       |
| Storage                    | 8        | `/api/storage/*`                                                                                   |
| Tango Extensions (auth)    | 1 (done) | Not in upstream spec: `POST /api/auth/sign-in`, `POST /api/auth/sign-out`, `GET /api/auth/session` |
| Tango Extensions (webhooks)| 7 (done) | Not in upstream spec: `/api/webhooks*` CRUD + logs + test delivery, `/api/webhook-logs`            |

Request naming: `<METHOD> <path>` (e.g. `GET /api/users/{id}`). Session cookies flow through
Yaak's cookie jar; for anonymous-401 checks use curl (the jar re-sends cookies).

## Pocket ID Source Map (porting reference)

| Pocket ID (`backend/internal/...`)                                            | Tango target                                                                                 |
| ----------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- |
| `middleware`, `apikey`                                                        | `internal/transport/middleware`, `modules/identity/apikey`                                   |
| `webauthn`, `devicelogin`, `onetimeaccess`, `usersignup`, `emailverification` | `modules/identity/*`                                                                         |
| `oidc`, `api` (apis resource)                                                 | `modules/federation/oidc`, `modules/identity/apiaccess`                                      |
| `appconfig`, `auditlogs`, `storage`, `email`, `job`                           | `modules/appconfig`, `modules/auditlog`, `internal/storage`, `internal/mailer`, `internal/queue` |
| `ldapsync`, `scimsync`                                                        | `modules/identity/ldapsync`, `modules/federation/scimsync`                                   |

## Gotchas (phases 1–5)

Hard-won notes; re-read before touching the same area.

- **jwx v3 API drift vs v2**: no `jwk.FromRaw` (use `jwk.Import`), `jwa.ES256` is a _function_
  (`jwa.ES256()`), `jwk.Key.Get(key, &out)` returns `error` (not `(value, ok)`), and
  `Set(jwk.AlgorithmKey, …)` takes a `jwa.SignatureAlgorithm` struct (`jwa.NewSignatureAlgorithm`
  or `jwa.RS256()`), never a raw string.
- **`jwk` private claims**: `jwtutils.Signer` rejects private claims colliding with registered
  names — `jti` goes through `Standard.JWTID`, `aud` through `Standard.Audience`, not the private
  map.
- **typeid ↔ UUID boundary**: DB UUID columns reject typeid strings (`user_01m2…`). Convert at the
  store edge (`userUUID()`); never bind a typeid string into a UUID column. Conversely,
  `typeid.FromUUID` expects the bare UUID — not the prefixed string.
- **go-sqlbuilder has no `OnConflictDoNothing/DoUpdate`** — inject with
  `ib.SQL("ON CONFLICT … DO NOTHING")` / `DO UPDATE SET … = EXCLUDED.…` after `Values`.
- **API mount paths are relative to the `/api` group**: `APIRoutes(r)` receives the `/api`
  router; mounting `/api/oidc/token` inside it produces `/api/api/oidc/token` (all 404s in phase
  4 live checks came from this). Keep _mount_ constants relative and _discovery-document_
  constants absolute.
- **chi panics on a nil middleware** (`r.With(nil)` → SIGSEGV at mount): guard-gated routes
  (oidc clients) must _skip mounting_ when no guard is wired — fail closed structurally.
- **Migration files are versioned once applied**: the `jwks` DDL lives inside
  `00006_create_federation_tables.sql`; editing an already-applied migration does NOT re-run it.
  Schema changes for applied migrations need a reset in dev (`db migrate:reset --up --force`,
  debug build only) or a new migration file.
- **`serve` does not migrate**: registry start fails fast when tables are missing (`jwks`
  bootstrap queries on start) — tests must run `database.MigrateUp` after `StartPostgres` (see
  `modules/auditlog/module_test.go` for the pattern).
- **RSA keygen adds ~0.3s to first boot** (`jwks.Service.Start` → `GenerateKey`); lifecycle tests
  must budget for it (serve probe timeout ≥ 20s).
- **Upstream stores authorization codes in plaintext** — tango hashes codes, refresh tokens, and
  client secrets (SHA-256); token exchange looks up by hash. Don't "restore" plaintext when
  diffing against Pocket ID.
- **`oidc_authorization_codes` has no redirect_uri column** (verbatim DDL): the authorize context
  (redirect_uri, sid) parks in an `authorize_code`-kind `oauth2_sessions` row, written at code
  issue, read at exchange.
- **Dev server must use `--env-file=.env.local`** (auto-loaded by the CLI when present, per
  Taskfile `run`): plain `tango serve` falls back to default DSN `postgres:postgres@…` and boots
  an empty-looking database.
- **Yaak MCP harness double-escapes nested JSON in request bodies** — POST/PUT/DELETE bodies
  cannot be filled via MCP (`"…" is not of type object`). Create GET requests via MCP; fill
  POST/PUT bodies manually in the UI, and verify mutations live via curl.
- **Yaak's cookie jar re-sends session cookies** — anonymous-401 checks must use curl with a
  clean jar, not Yaak.
- **Token rows are keyed by SHA-256, but user_id stays a UUID** — the phase 5 token stores
  (onetimeaccess, emailverification) upsert into `auth_tokens` by `(user_id, purpose)`. Two
  live failures came from this boundary: binding a typeid string into the UUID `user_id`
  column (fix: `userUUID()` at the store edge) and comparing a scanned UUID against
  `userID.String()` (typeid form) in the owner check — compare `userID.UUID()` instead. The
  second bug looks like "tokens vanish" because verify consumes the row, then 404s.
- **`signup/setup` creates the first admin** (upstream parity): it succeeds while no admin
  exists and 409s afterwards — it is not a general signup variant.
- **Admin password is not seeded by migrations** — migration 00004 inserts the admin user row
  but leaves `user_passwords` empty; first sign-in fails with 401 until a password hash is
  written (dev: seed via the password service or a one-off insert).

## Endpoint Gap Register (swagger.yaml vs tango, post-phase-4)

Audit of all 82 upstream paths against the live :3080 server (2026-09-13). Verdicts:
`missing-backend` (not implemented — scheduled by phase), `deviation` (implemented under a
different path/shape), `ok` (implemented, parity).

| Upstream path(s)                                                                                                             | Verdict         | Phase / note                                                                                                            |
| ---------------------------------------------------------------------------------------------------------------------------- | --------------- | ----------------------------------------------------------------------------------------------------------------------- |
| `/healthz`, `/api/version/current`, `/api/version/latest`                                                                    | ok              | `/healthz` serves 204 bare; the release check runs as a phase-7 recurring job and `/api/version/latest` serves the cached tag (deployed build as fallback) |
| `/api/users` GET/POST, `/api/users/{id}` GET/PUT/DELETE, `/api/users/{id}/groups`                                            | ok              | phase 1–2                                                                                                               |
| `/api/users/me` GET/PUT (+ profile-picture)                                                                                  | deviation       | implemented (session auth, profile fields only — email stays admin-gated); picture lands in phase 8                     |
| `/api/users/{id}/user-groups` PUT                                                                                            | ok              | completion pass (usergroup store: ReplaceGroupsForUser)                                                                 |
| `/api/users/{id}/profile-picture*`                                                                                           | missing-backend | phase 8 (storage)                                                                                                       |
| `/api/users/{id}/webauthn-credentials*`, `/api/users/me/send-email-verification`, `/api/users/me/verify-email`               | ok              | phase 5 (credentials admin CRUD + rename; verification self-service)                                                    |
| `/api/users/{id}/one-time-access-email` + `/api/users/{id}/one-time-access-token`                                            | ok              | phase 5 (mint returns raw token once; email send deferred to phase 7 queue)                                             |
| `/api/one-time-access-email`, `/api/one-time-access-token/{token}`                                                           | deviation       | phase 5 (anonymous email request 204-without-mint until appconfig policy, phase 8; exchange live)                       |
| `/api/signup`, `/api/signup/setup`, `/api/signup-tokens*`                                                                    | ok              | phase 5 (setup = first-admin bootstrap; token CRUD + group grants; invitations land later)                              |
| `/api/user-groups*`, `/api/user-groups/{id}/users` PUT                                                                       | ok              | phase 2                                                                                                                 |
| `/api/user-groups/{id}/allowed-oidc-clients` PUT                                                                             | ok              | phase 9A completion (junction `user_groups_allowed_oidc_clients`, migration 00028; snake_case `oidc_client_ids`)         |
| `/api/custom-claims/suggestions`, `/api/custom-claims/user` + `/user-group` CRUD                                             | ok              | completion pass: list-replace PUT + single-claim POST/PUT/DELETE both live                                              |
| `/api/audit-logs` (self), `/api/audit-logs/all` (admin), `/filters/users` + `/filters/client-names`                          | ok              | completion pass: self listing (session auth) + `/all` (admin) split                                                     |
| `/.well-known/jwks.json`, `/.well-known/openid-configuration`                                                                | ok              | phase 3                                                                                                                 |
| `/.well-known/oauth-authorization-server`                                                                                    | ok              | completion pass (RFC 8414 mirror)                                                                                       |
| `/api/oidc/clients` GET/POST, `/api/oidc/clients/{id}` GET/PUT/DELETE                                                        | ok              | phase 4 (fields may lag: logo, credentials list)                                                                        |
| `/api/oidc/clients/{id}/allowed-user-groups` PUT                                                                             | ok              | completion pass (list-replace userGroupIds)                                                                             |
| `/api/oidc/clients/{id}/secrets*` (list/create/delete multi-secret)                                                          | ok              | completion pass (credentials JSONB; legacy column mirrored)                                                             |
| `/api/oidc/clients/{id}/logo*`                                                                               | ok              | phase 9C (logo columns 00030, shared blob store)                                                                          |
| `/api/oidc/clients/{id}/refresh` (CIMD)                                                                      | ok              | phase 9D (CIMD-lite: admin-registered metadata-URL clients, allowlist default-deny, no fosite resolver — see deviations)  |
| `/api/oidc/token`, `/api/oidc/userinfo`                                                                                      | ok              | phase 4                                                                                                                 |
| `/api/oidc/introspect`                                                                                                       | ok              | completion pass (RFC 7662; client-scoped)                                                                               |
| `/api/oidc/users/me/clients`, `/api/oidc/users/me/authorized-clients*`, `/api/oidc/users/{id}/authorized-clients`            | ok              | completion pass (revocation cascades to active tokens)                                                                  |
| `/authorize` (authorize code + PKCE), `/api/oidc/end-session`                                                                | ok              | completion pass: end-session clears the cookie + logout-callback redirect; family revocation lands phase 5              |
| `/api/api-keys*`, `/api/apis*`, `/api/api-access/{clientId}/*`                                                               | ok              | phase 6 (live-tested; `X-API-KEY` machine guard on the users CRUD surface)                                              |
| `/api/device-login/*` (requests, exchange, verification, decision)                                                           | ok              | phase 5 (own table `device_login_requests`; upstream uses the francis actor framework — noted deviation)                |
| `/api/application-configuration*`, `/api/application-images/*`, `/api/scim/service-provider*` | partial         | phase 9C (images), phase 9D closing (settings CRUD + `test-email` live, phase 9B / 7, plus the phase 9 stretch: `smtp_*`/`ldap_*` admin-editable with env defaults); `sqlite-warning` **won't port** (Postgres-only, upstream-specific) |

Yaak folder coverage after phase 5: Users (webauthn-credentials, one-time access, email
verification), User Groups, Custom Claims, Audit Logs, Well Known (incl.
oauth-authorization-server), OIDC (incl. introspect), OAuth, Version, Device Login, Signup,
and Tango Extensions (auth) hold the implemented requests; missing _requests_ inside existing
folders mirror the `missing-backend` rows above and get created as each phase lands.

All 16 upstream parity gaps are closed (phase 9: 9A OIDC client context → 9B appconfig →
9C pictures/logos → 9D CIMD-lite refresh); final parity is 112/113 implemented + 1 recorded
non-goal (`sqlite-warning`, Postgres-only) — see `llms/phase-09-parity-gap.md`. Tango-only
features and every structural deviation from upstream are catalogued in
`llms/tango-deviations.md` — read it before porting upstream code so upstream shapes do not
leak into handlers.
