# AGENTS.md

Modular-monolith Go boilerplate (`tango`): one binary serving an HTTP/ConnectRPC API, an embedded SPA, and a CLI.

## Project Overview

- The binary is built from `cmd/` and exposes `serve`, `migrate:up|down|status|version` (plus `migrate:create|reset|seed|validate` in debug builds), `db:export`/`db:import`, `key:generate`/`key:rotate`, and `health` (CLI framework: `urfave/cli/v3`). Implemented: `key:generate` and all four `migrate:*` release commands; the rest print `not yet implemented`.
- The SPA (React 19 + TanStack + Vite) builds into `web/output/` and embeds into the same binary; the Vite dev server proxies `/api`, `/rpc`, `/.well-known`, and `/static` to the Go server on `:3080`.
- The backend is mid-rebuild. Implemented today: `database` (migrations + engine), `internal/datastore`, `pkg/{crypto,envfile,jwtutils,responder,validate,testutils}`, `api/connect`, `email/templates`, and `web`. Scaffolds only: most of `internal/**` and all of `modules/**` are single-line packages with no API. Read the file before assuming behavior.
- Never add, remove, or rename a top-level package or directory without an explicit request. Extend an existing package instead of creating a sibling one.
- Porting a plan or a doc into code means implementing it, not copying it. Several docs describe a larger surface than the code has.

## Tech Stack & Tooling

- Go 1.27.1, Node >= 24.21, pnpm 12.5.1, Docker (testcontainers), `task` (`Taskfile.yml`).
- Target stack: `go-chi/chi` router, Postgres via pgx, optional valkey, `samber/do` DI, goose as a library, koanf JSON config, LogLayer Go with `log/slog` as the adapter, OpenTelemetry, S3 + local file storage.
- Optional backends are opt-in, never required: cache defaults to in-memory, session store and rate limit default to Postgres (`public.rate_limits` + `fn_check_rate_limit` under advisory locks). Valkey replaces them only when configured.
- LogLayer: `go.loglayer.dev/v3` with per-transport modules (`go.loglayer.dev/transports/*/v3`) — CLI pretty, structured file via lumberjack, and OTel. Application code logs through `log/slog`; wire LogLayer as the slog handler (`integrations/sloghandler`) instead of calling LogLayer directly in feature code.
- DI is explicit: constructors registered with `samber/do`, composed in `internal/registry`. No global singletons, no package-level `init` wiring. Use skill `golang-samber-do` when using `samber/do` DI.

## Build / Test / Lint

- `task dev` — Vite dev server (:3000) + Go API (:3080). `task run` — Go server only, debug build with `--env-file=.env.local`.
- `task build` — frontend bundle + Go binary. `task start` — run the release binary.
- `task test` — frontend + Go unit tests. `task test:go -- ./modules/...` for one package. `task test:go:debug` covers the `debug`-tagged packages (`./cmd/... ./database/...`).
- `task lint` — golangci-lint + oxlint. `task check` — `go vet` + format check. `task format` — gofmt + oxfmt. `task typecheck` — `tsc -b --noEmit`.
- `task rpc:generate` regenerates Go and TypeScript from `api/connect/*.proto`; `task rpc:stale` fails when the contracts changed without regenerating.
- `task key:generate` runs `go run -tags debug ./cmd key:generate --env-file=.env.local --overwrite`; `task db:migrate` runs `migrate:up`, `db:rollback` runs `migrate:down`, and `db:status`/`db:version` wrap the matching commands. Pass extra flags after `--`.
- `task cert:generate` writes `storage/certs/localhost_{key,crt}.pem` via `mkcert`, falling back to a self-signed `openssl` certificate; `task cert:trust` installs the mkcert CA. The nginx service in `compose.yaml` reads those paths.
- Integration tests use `pkg/testutils.StartPostgres` / `StartMailpit` / `StartMinIO` (testcontainers). They need a Docker daemon; call `testutils.SkipWithoutDocker(t)` when one may be absent (macOS CI has none).
- Check library documentation before writing code against a third-party package: use the Context7 MCP (`mcp_context7__resolve_library_id` then `mcp_context7__query_docs`) for usage and options, and the DeepWiki MCP (`mcp_deepwiki__ask_question`) for design intent and behavior questions about a GitHub repository. See "Library Documentation" below.

## Architecture

Implemented today — treat as the contract:

- `database/migrations/` — goose SQL migrations, the single source of schema truth (9 files, `00001`–`00009`). Numbering starts at 1: goose reserves version 0 as the sentinel row in `app_migration`, and silently skips any file whose numeric prefix is below 1. Never embed DDL or create tables at runtime. Editing an applied migration does not re-run it — roll back with `migrate:down` and re-apply.
- `database/` — the migration engine over goose v3. `NewMigrator` loads the migrations embedded in the binary (`go:embed migrations/*.sql`) and builds a `goose.Provider` with a Postgres session locker and `app_migration` as the version table. Migrations run on the single-connection handle from `datastore.OpenMigrationDB`, never the pool. `Up`, `UpTo`, `Down`, `Status`, `Version`, `Pending`, `Applied`, `HighestVersion` return plain structs so `cmd/` never imports goose. `Down(ctx, count)` rolls back one migration at a time, so goose picks the next in the order it recorded — a version comparison cannot express out-of-order rollback.
- `cmd/migration.go` — DSN resolution and the migration commands. The DSN comes from `envfile.DatabaseURL`, resolved from the root `--env-file` first and the process environment second; nothing else opens a connection. `migrate:up` and `migrate:down` ask for confirmation only when stdin is a real terminal (checked with `golang.org/x/term`, because `/dev/null` is also a character device) and proceed immediately when piped, so `task db:migrate` and CI never block. `up` takes `--to`, `down` takes `--count`; both take `--dry-run` and `--force`. `migrate:status` lists every embedded migration with its state, `migrate:version` prints the bare version number for scripts.
- `pkg/crypto` — `Cipher` seals recoverable values with AES-256-GCM and the exact `enc:` prefix; `PasswordHasher` produces PHC strings (scrypt default, Argon2id opt-in). Keys are 32 bytes and stored as 64 hex characters (`GenerateKeyHex`, `ParseKeyHex`, `NewCipherFromHex`). `KeyGenerator` always emits all four variables: `APP_SECRET_KEY`, `AUTH_PRIVATE_KEY`/`AUTH_PUBLIC_KEY` as base64 (raw, unpadded) JWK JSON with `alg` and a thumbprint `kid`, and `AUTH_SECRET_KEY`. The key pair and the HMAC secret are independent: defaults are `ES256` and `HS256`, and a passed algorithm replaces only the role it can fill (asymmetric → key pair, `HS*` → secret). Passwords are hashed, never encrypted. Do not add another encryption or hashing format.
- `pkg/envfile` — dotenv reader/writer that preserves comments, blank lines, and key order; new files are written `0600`. Owns the `DatabaseURL` key constant (`DATABASE_URL`) so the CLI and future config layer cannot drift. `key:generate` declares its own `--env-file` flag (the root flag only loads config) and owns the I/O: it only prints unless that flag is given, creates a missing file, and asks before replacing the values of an existing one (`--overwrite` skips the prompt).
- `pkg/responder` — the API envelope, pagination, and request IDs (envelope documented in `docs/api-response.md`). `pkg/validate` — request decoding and ozzo v4 code-first validation. Handlers use these; no hand-built envelopes or ad-hoc field guards.
- `pkg/jwtutils` — JWT signing/verification with typed private claims. `pkg/testutils` — shared testcontainers Postgres, Mailpit, MinIO.
- `internal/datastore` — the Postgres adapter. `NewPostgres` owns the single `pgxpool` of the process (sane pool defaults, connect-time ping, `Ping`/`Stats`/`Close`), `Querier` is the shared pool/transaction surface, and `WithTx` commits on a nil callback and rolls back otherwise (including on panic). Every pooled connection sets `search_path` and `timezone` through `RuntimeParams`, so a recycled connection keeps the same session defaults. `Acquire` hands out a connection for work that must stay on one backend (LISTEN, session advisory locks); callers must release it. `ErrNoRows` is re-exported so repositories do not import pgx. Migrations never use the pool: `OpenMigrationDB` / `(*Postgres).MigrationDB` return a `database/sql` handle pinned to one connection, because goose holds a session advisory lock and needs every statement of a run on the same backend. `internal/datastore/valkey.go` is still a stub.
- `api/connect/*.proto` — ConnectRPC contracts. `email/templates` — React Email sources compiled into `web/email`. `web` — SPA embed and static serving (debug/release variants).

Planned layout — the target shape of the scaffold packages:

- `internal/kernel` — module contract + registry (routes, middleware, start/stop). `internal/registry` — composition root wiring modules and shared dependencies.
- `internal/config` — koanf layering. `internal/transport` — HTTP server, middleware, SPA serving. `internal/logger` — LogLayer setup. `internal/queue` — Postgres-backed in-process task queue. `internal/cache`, `internal/storage`, `internal/mailer`, `internal/jobs`, `internal/scheduler`, `internal/observer` — infrastructure surfaces.
- `modules/<area>/<feature>/` owns one feature: `schema.go`, `repository.go`, `service.go`, `handler_rpc.go` (plus `handler.go` when a REST surface exists), `module.go`. Business logic lives in `service.go`; handlers do transport mapping only. Follow this layout when implementing; do not invent a different file set.
- Persistence goes through `internal/datastore`; never open a second pool or a raw driver connection inside a module.
- Typed IDs come from `go.jetify.com/typeid`; the prefix is declared in the module's `schema.go`. Token and code rows use hashes plus DB `uuidv7()` instead of TypeIDs.
- Background work runs on `internal/queue`: register typed queues, enqueue typed tasks, keep the schema in migrations (`00008_create_queue_tables.sql`). Recurring maintenance belongs in `internal/jobs`.

## Library Documentation

- Before writing code against a third-party package, look it up instead of guessing: Context7 MCP (`mcp_context7__resolve_library_id`, then `mcp_context7__query_docs`) for API usage and options, DeepWiki MCP (`mcp_deepwiki__ask_question` with `owner/repo`) for design intent and behavior.
- Use them when the answer depends on a version, when the README is thin, or when comparing two candidate libraries. Do not use them for this repo's own code — read the source.
- The pinned source of truth is the module cache (`go env GOMODCACHE`). When docs and code disagree, the code wins; say so and follow the code.
- Record the outcome of a library comparison in this file when it settles a decision, so the next agent does not re-run it.

## Conventions

- Config precedence, lowest to highest: built-in defaults → JSON config file → env file → system environment → CLI args. `--env-file` beats system environment; CLI args beat both.
- Config values support interpolation in the JSON source: `${NAME}` for plain substitution and `env:NAME` for environment-variable resolution. A missing referenced variable is an error, not an empty string.
- Every config key needs a default, a validation rule, and a line in `.env.example`. Add a test that keeps `.env.example` and `internal/config` in sync when the config package lands.
- Go 1.27 idioms: `encoding/json/v2` (with `omitzero`), `for range n`, `t.Context()` in tests, `errors.Join`, `slices`/`maps`, `min`/`max`, stdlib `uuid`.
- Shut down through context cancellation: servers, queue workers, and background jobs stop in order and drain in-flight work. New long-running components must implement stop.
- Comments are for what the code cannot say: an invariant, security rule, protocol requirement, or side effect. Write them short and functional. Never add a comment that only restates the code, narrates control flow, or repeats a name. Never leave phase/task/plan markers or signatures such as `phase *`, `Step N`, or `TODO(plan)`. Mark real future rework with a `TODO` or `FIXME` comment that says what to change and why.
- Do not add compatibility shims, fallback readers, or dual-write paths. Delete the obsolete path instead.
- Fix lint findings at the source. `//nolint` is not an accepted answer.

## Common Tasks

- Add a feature module: create `modules/<area>/<feature>/` with the standard file set, implement the `internal/kernel` module contract in `module.go`, and register it in `internal/registry`.
- Add an endpoint: write the proto in `api/connect/*.proto`, run `task rpc:generate`, implement the handler, then mount it in the registry.
- Add a migration: create `database/migrations/<NNNNN>_<name>.sql` with goose Up/Down blocks, then run `task db:migrate`. The version must be `>= 1` and higher than the last file; `00001` is the lowest. Migrations are embedded in the binary, so a new file only runs after a rebuild.
- Add a test that touches the schema: get a database per test with `testutils.StartPostgres(t).NewDatabase(t)`. The shared container DSN points at one database for the whole test binary, so migrations applied by one test leak into the next.
- Add a config key: define it in `internal/config` with a default and validation, document it in `.env.example`, and honor the precedence order above.
- Add a background job: define a task type plus queue config in `internal/queue`, register the queue, and enqueue from the owning module.
- Local services: `docker compose up -d pgsql` for the database; the full stack (pgsql, redis, mailpit, silo, nginx) is in `compose.yaml`.
- The migration engine is goose v3 as a library. Every migration must define both `-- +goose Up` and `-- +goose Down`. goose requires one `Up`; the CLI's `migrate:down` requires the `Down`.

## Gotchas / Anti-patterns

- `codegen/` is gitignored build output. Never edit or commit it; regenerate with `task rpc:generate`.
- Migration numbering is sequential and 5 digits (`00009` is the highest today) and starts at `00001`. goose treats version 0 as the sentinel row and silently skips a file numbered below 1, so a `00000_*.sql` file never runs and never errors. Bump the version assertions in the migrator tests when adding one.
- `internal/queue/README.md` documents an API, a `module.go`, and migration `00010` that do not exist — the engine was removed and only the doc remains. Trust the code and `database/migrations/` over that file.
- Keep `Taskfile.yml` targets aligned with the CLI (`key:generate`, `db:migrate` → `migrate:up`). A renamed command silently breaks the task.
- The root `--env-file` flag loads configuration for the server commands. A subcommand that writes a file must declare its own flag of that name; never treat the root flag as a write target, or `task run -- <cmd>` will rewrite `.env.local`.
- The README intentionally carries no architecture detail; the "Implemented today" list in this file and the code are the source of truth for what runs. When a scaffold package becomes real, update that list.
- Keep the README lean: setup steps, task table, certificates, deployment, license. Do not re-add architecture tables, emoji, or per-path inventories there.
- Integration tests without a Docker daemon must skip, not fail. Never widen a timeout to work around a hung container.
- Do not require valkey, S3, or SMTP for the default local path; Postgres plus local storage must be enough.
- Never rewrite a dotenv file the user owns without consent: print instead, or ask first. Destructive overwrite needs an explicit flag.
- Do not run destructive database commands (`migrate:reset`, volume removal) against a database holding state you did not create.
- A command whose action prints `not yet implemented` is not implemented. Never document, test, or rely on it as working, and never report a stub path as validated.

## Committing

- Stage explicit paths only; never `git add -A` / `git add .`. Verify with `git status` before committing.
- Message format: `{feat,fix,docs,refactor,chore}[(scope)]: <concise message>`.
- Never push. Commits are local unless the user asks for a push.
- Never commit on your own initiative — finish the work, recommend a message, and let the user commit. The exception is an explicit commit instruction in that turn.
- Always close a finished piece of work with a recommended commit title in chat, even when you did not commit. Use the format above, keep it one line, and list the changed paths under it.

Never run: `git reset --hard`, `git checkout .`, `git clean -fd`, `git stash`, `git commit --no-verify`, `git push --force`.

## Related Agent Instructions

- None. `AGENTS.md` is the single instruction source for all agents (Codex, Elph, Copilot, Cursor, Gemini CLI).
