# AGENTS.md

Modular-monolith Go boilerplate (`tango`): one binary serving an HTTP/ConnectRPC API, an embedded SPA, and a CLI.

## Project Overview

- The binary is built from `cmd/` and exposes `serve`, `migrate:up|down|status|version` (plus `migrate:create|reset|seed|validate` in debug builds), `db:export`/`db:import`, `key:generate`/`key:rotate`, and `health` (CLI framework: `urfave/cli/v3`). Every command action, `serve` included, currently prints `not yet implemented`.
- The SPA (React 19 + TanStack + Vite) builds into `web/output/` and embeds into the same binary; the Vite dev server proxies `/api`, `/rpc`, `/.well-known`, and `/static` to the Go server on `:3080`.
- The backend is mid-rebuild. Implemented today: `database/migrations`, `pkg/{crypto,jwtutils,responder,validate,testutils}`, `api/connect`, `email/templates`, and `web`. Scaffolds only: `cmd/`, `internal/**`, and `modules/**` are mostly single-line packages with no API. Read the file before assuming behavior.
- Porting a plan or a doc into code means implementing it, not copying it. Several docs describe a larger surface than the code has.

## Tech Stack & Tooling

- Go 1.27.1, Node >= 24.21, pnpm 12.5.1, Docker (testcontainers), `task` (`Taskfile.yml`).
- Target stack: `go-chi/chi` router, Postgres via pgx, optional valkey, `samber/do` DI, goose as a library, koanf JSON config, LogLayer Go with `log/slog` as the adapter, OpenTelemetry, S3 + local file storage.
- Optional backends are opt-in, never required: cache defaults to in-memory, session store and rate limit default to Postgres (`public.rate_limits` + `fn_check_rate_limit` under advisory locks). Valkey replaces them only when configured.
- LogLayer: `go.loglayer.dev/v3` with per-transport modules (`go.loglayer.dev/transports/*/v3`) — CLI pretty, structured file via lumberjack, and OTel. Application code logs through `log/slog`; wire LogLayer as the slog handler (`integrations/sloghandler`) instead of calling LogLayer directly in feature code.
- DI is explicit: constructors registered with `samber/do`, composed in `internal/registry`. No global singletons, no package-level `init` wiring.

## Build / Test / Lint

- `task dev` — Vite dev server (:3000) + Go API (:3080). `task run` — Go server only, debug build with `--env-file=.env.local`.
- `task build` — frontend bundle + Go binary. `task start` — run the release binary.
- `task test` — frontend + Go unit tests. `task test:go -- ./modules/...` for one package. `task test:go:debug` covers the `debug`-tagged packages (`./cmd/... ./database/...`).
- `task lint` — golangci-lint + oxlint. `task check` — `go vet` + format check. `task format` — gofmt + oxfmt. `task typecheck` — `tsc -b --noEmit`.
- `task rpc:generate` regenerates Go and TypeScript from `api/connect/*.proto`; `task rpc:stale` fails when the contracts changed without regenerating.
- `task db:migrate` and `task secrets:generate` still call the pre-reset CLI shape. Until the Taskfile is updated, use `go run -tags debug ./cmd migrate:up --env-file=.env.local` and `go run -tags debug ./cmd key:generate`.
- Integration tests use `pkg/testutils.StartPostgres` / `StartMailpit` / `StartMinIO` (testcontainers). They need a Docker daemon; call `testutils.SkipWithoutDocker(t)` when one may be absent (macOS CI has none).

## Architecture

Implemented today — treat as the contract:

- `database/migrations/` — goose SQL migrations, the single source of schema truth (9 files, `00000`–`00008`). Never embed DDL or create tables at runtime. Editing an applied migration does not re-run it — roll back with `migrate:down` and re-apply.
- `pkg/crypto` — `Cipher` seals recoverable values with AES-256-GCM and the exact `enc:` prefix; `PasswordHasher` produces PHC strings (scrypt default, Argon2id opt-in). Passwords are hashed, never encrypted. Do not add another encryption or hashing format.
- `pkg/responder` — the API envelope, pagination, and request IDs (envelope documented in `docs/api-response.md`). `pkg/validate` — request decoding and ozzo v4 code-first validation. Handlers use these; no hand-built envelopes or ad-hoc field guards.
- `pkg/jwtutils` — JWT signing/verification with typed private claims. `pkg/testutils` — shared testcontainers Postgres, Mailpit, MinIO.
- `api/connect/*.proto` — ConnectRPC contracts. `email/templates` — React Email sources compiled into `web/email`. `web` — SPA embed and static serving (debug/release variants).

Planned layout — the target shape of the scaffold packages:

- `internal/kernel` — module contract + registry (routes, middleware, start/stop). `internal/registry` — composition root wiring modules and shared dependencies.
- `internal/config` — koanf layering. `internal/datastore` — Postgres pool, transactions, health. `internal/transport` — HTTP server, middleware, SPA serving. `internal/logger` — LogLayer setup. `internal/queue` — Postgres-backed in-process task queue. `internal/cache`, `internal/storage`, `internal/mailer`, `internal/jobs`, `internal/scheduler`, `internal/observer` — infrastructure surfaces.
- `modules/<area>/<feature>/` owns one feature: `schema.go`, `repository.go`, `service.go`, `handler_rpc.go` (plus `handler.go` when a REST surface exists), `module.go`. Business logic lives in `service.go`; handlers do transport mapping only. Follow this layout when implementing; do not invent a different file set.
- Persistence goes through `internal/datastore`; never open a second pool or a raw driver connection inside a module.
- Typed IDs come from `go.jetify.com/typeid`; the prefix is declared in the module's `schema.go`. Token and code rows use hashes plus DB `uuidv7()` instead of TypeIDs.
- Background work runs on `internal/queue`: register typed queues, enqueue typed tasks, keep the schema in migrations (`00007_create_queue_tables.sql`). Recurring maintenance belongs in `internal/jobs`.

## Conventions

- Config precedence, lowest to highest: built-in defaults → JSON config file → env file → system environment → CLI args. `--env-file` beats system environment; CLI args beat both.
- Config values support interpolation in the JSON source: `${NAME}` for plain substitution and `env:NAME` for environment-variable resolution. A missing referenced variable is an error, not an empty string.
- Every config key needs a default, a validation rule, and a line in `.env.example`. Add a test that keeps `.env.example` and `internal/config` in sync when the config package lands.
- Go 1.27 idioms: `encoding/json/v2` (with `omitzero`), `for range n`, `t.Context()` in tests, `errors.Join`, `slices`/`maps`, `min`/`max`, stdlib `uuid`.
- Shut down through context cancellation: servers, queue workers, and background jobs stop in order and drain in-flight work. New long-running components must implement stop.
- Comments explain only what the code cannot: an invariant, security rule, protocol requirement, or side effect. Do not narrate control flow, restate names, or leave plan/task/phase markers.
- Do not add compatibility shims, fallback readers, or dual-write paths. Delete the obsolete path instead.
- Fix lint findings at the source. `//nolint` is not an accepted answer.

## Common Tasks

- Add a feature module: create `modules/<area>/<feature>/` with the standard file set, implement the `internal/kernel` module contract in `module.go`, and register it in `internal/registry`.
- Add an endpoint: write the proto in `api/connect/*.proto`, run `task rpc:generate`, implement the handler, then mount it in the registry.
- Add a migration: create `database/migrations/<NNNNN>_<name>.sql` with goose Up/Down blocks, then run `task db:migrate`.
- Add a config key: define it in `internal/config` with a default and validation, document it in `.env.example`, and honor the precedence order above.
- Add a background job: define a task type plus queue config in `internal/queue`, register the queue, and enqueue from the owning module.
- Local services: `docker compose up -d pgsql` for the database; the full stack (pgsql, redis, mailpit, silo, nginx) is in `compose.yaml`.

## Gotchas / Anti-patterns

- `codegen/` is gitignored build output. Never edit or commit it; regenerate with `task rpc:generate`.
- Migration numbering is sequential and 5 digits (`00007` is the highest today). Bump the version assertions in the migrator tests when adding one.
- The Taskfile's `db:migrate` and `secrets:generate` targets still call the pre-reset CLI shape and fail; run the `go run -tags debug ./cmd ...` forms instead.
- `internal/queue/README.md` documents an API, a `module.go`, and migration `00010` that do not exist — the engine was removed and only the doc remains. Trust the code and `database/migrations/` over that file.
- `Taskfile.yml` still calls the removed `secrets` command; `cmd/` implements `key:generate` and `key:rotate`.
- The README intentionally carries no architecture detail; the "Implemented today" list in this file and the code are the source of truth for what runs. When a scaffold package becomes real, update that list.
- Keep the README lean: setup steps, task table, certificates, deployment, license. Do not re-add architecture tables, emoji, or per-path inventories there.
- Integration tests without a Docker daemon must skip, not fail. Never widen a timeout to work around a hung container.
- Do not require valkey, S3, or SMTP for the default local path; Postgres plus local storage must be enough.
- Do not run destructive database commands (`migrate:reset`, volume removal) against a database holding state you did not create.
- A command whose action prints `not yet implemented` is not implemented. Never document, test, or rely on it as working, and never report a stub path as validated.

## Committing

- Stage explicit paths only; never `git add -A` / `git add .`. Verify with `git status` before committing.
- Message format: `{feat,fix,docs,refactor,chore}[(scope)]: <concise message>`.
- Never push. Commits are local unless the user asks for a push.
- Never commit on your own initiative — finish the work, recommend a message, and let the user commit. The exception is an explicit commit instruction in that turn.

Never run: `git reset --hard`, `git checkout .`, `git clean -fd`, `git stash`, `git commit --no-verify`, `git push --force`.

## Related Agent Instructions

- None. `AGENTS.md` is the single instruction source for all agents (Codex, Elph, Copilot, Cursor, Gemini CLI).
