# AGENTS.md

Go + React monolith template (tango): Kong CLI, Koanf config, chi router, Postgres (pgx), Vite + TanStack frontend.

## Project Overview

- Single binary serves the API (`:3080`), SPA assets, and CLI (`cmd/launcher`).
- Domain modules live in `modules/`; shared infrastructure in `internal/`; reusable libraries in `pkg/` (zero `internal/` imports allowed there).
- Porting plan, conventions, and endpoint reference live in `llms/README.md` — read it before adding features. Per-phase plans: `llms/phase-*.md`.
- Upstream reference source is cloned at `~/Developer/github.com/pocket-id/pocket-id`
  (tag `v2.14.0`); read it locally instead of fetching from the web.

## Tech Stack & Tooling

- Go 1.27, Node >= 24.21, pnpm >= 12.4, Docker (compose stack in `compose.yaml`).
- Task runner: `task` (see `Taskfile.yml`). Dev stack: `docker compose up -d`.
- Frontend: React 19, TanStack Router/Query/Store, Vite; email templates in `email/` compiled by `plugins/plugin-email.ts` into `web/email` (embedded in the binary).

## Build / Test / Lint

- `task test` — Go + frontend tests. `task test:go -- ./modules/identity/...` for one package.
- `task test:go:debug` — debug-tag suite (`cmd/...` + `database/...`); `go test -tags release ./...` for the release-tag suite. The full gate is all three suites.
- `task lint` — golangci-lint + oxlint. `task check` — go vet + format check.
- `task format` — gofmt + oxfmt. `task typecheck` — `tsc -b --noEmit`.
- Integration tests need Docker (testcontainers: Postgres 18, Mailpit); they are skipped or fail fast without a Docker daemon.

## Architecture

- `internal/kernel` — module contract: `Name()` plus optional `APIRoutes`/`Routes`/
  `Startable` capabilities; `internal/registry` wires modules into the HTTP server.
- `modules/<area>/<feature>/{schema,service,store,handler}.go` — exactly one store file named `store.go`, Postgres-backed via `internal/datastore`. No memory-store implementations.
- `modules/identity` — accounts core + auth features (session, password, webauthn, signup, apikey, apiaccess, ldapsync, ...). `modules/federation` — provider surface (oidc, jwks, discovery, scimsync). Authn/authz features never leave `modules/identity`.
- Schema is owned by `database/migrations/` (goose). Never embed or auto-create schema.
- Typed IDs per module via `go.jetify.com/typeid`; prefix in the module's `schema.go`.
- `llms/database-reference.sql` — upstream Pocket ID schema dump (v2.14.0) for parity checks.

## Conventions

- Go 1.27 idioms: stdlib `uuid`, `encoding/json/v2` (`omitzero`), `for range n`, `t.Context()` in tests, `errors.Join`, `min`/`max`/`slices`/`maps`.
- Request validation via `pkg/validate` (ozzo v4, code-first `Validate()` methods);
  handlers respond 422 through `pkg/responder` — no ad-hoc field guards.
- SQL via `github.com/huandu/go-sqlbuilder` (PostgreSQL dialect). Gotchas: raw conditions (`IS NOT NULL`) pass as plain strings; pgx surfaces statement errors through `rows.Err()` after iteration, not the `Query` return.
- Tests use `pkg/testutils.StartPostgres` (shared container; isolate data with unique names, never absolute-count assertions).
- Only URL-facing/cross-module IDs carry TypeID; token/code rows use SHA-256 keys plus DB `uuidv7()`.
- Migration DDL is verbatim: editing an applied migration does not re-run it. Reset via `tango db migrate:down --force --count N` then `migrate:up`.
- Comments: concise, explain why, never restate the code. No section separators, no abbreviated folder names.

## Common Tasks

- Add an endpoint: follow the matching `llms/phase-*.md` task list; create the request in Yaak (via MCP) and send it against the running server before ticking a checkbox.
- Frontend asset images live in `public/images/` → copied to `web/output/images` by the Vite build; never embed them in Go.
- Dev LDAP server: `deploy/glauth/` (postgres plugin backend, seeded via `seed.sql`).

## Gotchas / Anti-patterns

- Committing without a JS file staged can fail lefthook `format-js`; retry or use `--no-verify` after confirming `format-go` is clean.
- `env.example` and `internal/config` are kept in sync by a test — new config keys must be documented in `.env.example`.
- Commits are local only; never push without being asked.
- `compose.yaml` `pocketid` service runs upstream Pocket ID for parity testing; its DB holds real schema state — do not wipe it casually.
