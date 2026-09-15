# AGENTS.md

Go + React monolith (tango): one binary serving an OIDC provider API (`:3080`), SPA assets, and a CLI.

## Project Overview

- Port of upstream Pocket ID (`~/Developer/github.com/pocket-id/pocket-id`, tag `v2.14.0` — read it locally, never fetch from the web) onto a Go/Postgres stack, deliberately diverging where the porting plan says so.
- Read `llms/porting-plan/README.md` before porting or changing API features: active scope, excluded-feature cleanup, atomic tasks, endpoint contract checks, and validation gates are defined there. Use `llms/endpoint-reference.md`, `llms/database-reference.sql`, and `llms/tango-deviations.md` as contract references; `llms/archived/` is historical context only.
- The upstream port is complete (parity 112/113, one recorded non-goal); new work should check the deviations doc first so upstream shapes do not leak into handlers.

## Tech Stack & Tooling

- Go 1.27, Node >= 24.21, pnpm >= 12.4, Docker.
- Task runner: `task` (`Taskfile.yml`). Dev stack: `docker compose up -d` (Postgres, Mailpit, upstream parity instance).
- Frontend: React 19 + TanStack Router/Query/Store + Vite. Email templates in `email/` compile via `plugins/plugin-email.ts` into `web/email` (embedded in the binary).

## Build / Test / Lint

- `task test` — full gate: Go release-tag suite + debug-tag suite + frontend. Run one package with `task test:go -- ./modules/identity/...`.
- `task test:go:debug` covers `cmd/...` + `database/...`; the release-tag suite is `go test -tags release ./...`. The full gate is all three suites.
- `task lint` — golangci-lint + oxlint. `task check` — go vet + format check. `task format` — gofmt + oxfmt. `task typecheck` — `tsc -b --noEmit`.
- Integration tests use testcontainers (Postgres 18, Mailpit) and fail fast without a Docker daemon.

## Architecture

- `cmd/launcher` remains the CLI and server entrypoint. `internal/registry` is the explicit composition root. Keep `internal/` flat and avoid adding `internal/app`, `internal/platform`, or a generic plugin registry. Application boundaries are `modules/identity`, `modules/federation`, `modules/admin`, and `modules/webhook`.
- `modules/<area>/<feature>/{schema,service,store,handler}.go` — exactly one store file named `store.go`, Postgres-backed via `internal/datastore`. No memory-store implementations.
- `modules/identity` — accounts core + auth features (session, password, webauthn, signup, apikey, apiaccess, ...). `modules/federation` — provider surface (oidc, jwks, discovery, scimsync). Authn/authz features never leave `modules/identity`.
- Admin-editable settings live in `app_config` via `modules/appconfig`; defaults fold catalog < env < DB. Cross-module consumers read through the appconfig surface (`MergedValues`), not raw env. Sensitive values redact in the admin view but resolve for their owning consumers.
- Schema is owned by `database/migrations/` (goose). Never embed or auto-create schema. Migration DDL is verbatim: editing an applied migration does not re-run it; reset via `tango db migrate:down --force --count N` then `migrate:up`.
- Typed IDs per module via `go.jetify.com/typeid`; the prefix lives in the module's `schema.go`. Only URL-facing/cross-module IDs carry TypeID; token/code rows use SHA-256 keys plus DB `uuidv7()`.
- Background work runs on the built-in queue `internal/queue` (tango-owned; based on backlite): Postgres-backed, in-process dispatcher, schema in migrations. Consumers register queue processors and enqueue typed tasks; the engine reads/writes only through `internal/datastore` (`Executor`/`WithTx`). Recurring maintenance lives in `internal/jobs` (`Job` registry, fixed-delay self-rescheduling).
- Recoverable encrypted values use `pkg/crypto.Cipher` and are written with the exact `enc:` prefix (`enc:<ciphertext>`). Passwords, reset/session tokens, API keys, and recovery codes remain hashes when verification is sufficient. Do not add another encryption format.
- This is a fresh target implementation: do not add legacy code, backward-compatibility branches, fallback readers, compatibility views, dual writes, transitional columns, or adapters for removed behavior. Delete obsolete paths instead.
- `llms/database-reference.sql` — upstream schema dump for parity checks.

## Conventions

- Go 1.27 idioms: stdlib `uuid`, `encoding/json/v2` (`omitzero`), `for range n`, `t.Context()` in tests, `errors.Join`, `min`/`max`/`slices`/`maps`.
- Request validation via `pkg/validate` (ozzo v4, code-first `Validate()` methods); handlers respond 422 through `pkg/responder` — no ad-hoc field guards.
- SQL via `github.com/huandu/go-sqlbuilder` (PostgreSQL dialect). Gotchas: raw conditions (`IS NOT NULL`) pass as plain strings; pgx surfaces statement errors through `rows.Err()` after iteration, not the `Query` return.
- Responses use the envelope `{status, message?, data, error?, metadata, links}` in snake_case, except declared bare-document endpoints (jwks, images, `.well-known/*`).
- Tests use `pkg/testutils.StartPostgres` (throwaway containers; isolate data with unique names, never absolute-count assertions).
- Comments: write only concise context that the code cannot show. Explain a non-obvious invariant, security rule, protocol requirement, compatibility constraint, or side effect; do not narrate control flow or restate names and expressions.
- Exported declarations need a short Go doc comment when their contract is not obvious; begin it with the declaration name. Add comments to unexported functions only when their behavior or constraint needs explanation.
- Do not add history, porting progress, phase/task/plan markers, TODO-style notes, personal context, or upstream-reference commentary. Preserve a reference only when it explains a live protocol or compatibility requirement, and describe the behavior rather than its origin.
- Keep test comments limited to setup, invariants, security properties, or non-obvious fixtures. Remove comments that merely label the next assertion or restate the test name.
- Do not use section-separator comments. When existing comments violate these rules, delete or rewrite them while preserving behavior.

## Common Tasks

- Add an endpoint: follow the matching `llms/phase-*.md` task list; create the request in Yaak (via MCP) and send it against the running server before ticking a checkbox. Exported request specs land in `api/specs/*.yaml`.
- Add a config key: catalog entry in `modules/appconfig/config.go` + env layer in `modules/appconfig/env.go` (`EnvDefaults`); `.env.example` documents the env name.
- Frontend asset images live in `public/images/` → copied to `web/output/images` by the Vite build; never embed them in Go.
- LDAP is an excluded upstream feature: no LDAP configuration, services, clients, or schema
  columns. Do not reintroduce any of them.
- Migrations: `tango db migrate:create`, `migrate:up`, `migrate:status` (see `tango db --help`); bump the version/count assertions in the migrator tests (`database/migrator_test.go`, `cmd/launcher/db_migrate*_test.go`) with every new migration.

## Gotchas / Anti-patterns

- Check `gofmt -l` before committing; lefthook `format-go` and `format-js` block dirty trees. Committing without a JS file staged can still fail `format-js` — retry or use `--no-verify` after confirming `format-go` is clean.
- `env.example` and `internal/config` are kept in sync by a test — new config keys must be documented in `.env.example`.
- Tests and Yaak requests must fail fast with explicit timeouts. Prefer focused commands with `-failfast` where supported; stop and report a hung test, unavailable container, or stalled request instead of waiting for a long default timeout.
- When local Pocket ID behavior, a database field, or an API contract is ambiguous, do not guess. Record the evidence and ask the project owner for confirmation before changing dependent code, endpoint matrices, or Yaak requests.
- Route params with IDs only accept TypeID form (`user_...`, `oidc_client_...`); raw UUIDs 404.
- `compose.yaml` `pocketid` service runs upstream Pocket ID for parity testing; its DB holds real schema state — do not wipe it casually.
- Commits are local only; never push without being asked.
- Never commit on your own — even when the work is done and the gate is green. Recommend a commit message and let the user run the commit (the only exception is an explicit "commit" instruction in that turn).

## Related Agent Instructions

- None found. `AGENTS.md` is the single instruction source for all agents (Codex, Elph, Copilot, Cursor, Gemini CLI).
