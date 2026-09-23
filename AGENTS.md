# AGENTS.md

Modular-monolith Go boilerplate (`tango`): one binary serving an HTTP/ConnectRPC API, an embedded SPA, and a CLI.

Full design rationale per package: **`.llms/architecture.md`** — read the section for the package you are changing before assuming behavior. This file holds the rules; that file holds the reasoning.

## Project Overview

- Built from `cmd/`. Commands: `serve`, `migrate:up|down|status|version` (plus `migrate:create|reset|seed|validate` in debug builds), `db:export`/`db:import`, `key:generate`/`key:rotate`, `health`, `logger:smoke`, `otel:smoke`, `mailer:smoke` (framework: `urfave/cli/v3`). Implemented: `key:generate`, `health`, every `migrate:*`, `db:export`/`db:import`, `mailer:smoke`, and `serve` (the basic surface: `/api` landing endpoint, `/api/healthz`, the SPA mount, the metrics exposition, and the request id, logger, recoverer, CORS, timeout, and rate limit middleware; ConnectRPC routes and the auth middleware still wait for their pieces). The rest print `not yet implemented`.
- The SPA (React 19 + TanStack + Vite) builds into `web/output/` and embeds into the same binary. The Vite dev server proxies `/api`, `/rpc`, `/.well-known`, `/static` to the Go server on `:3080`.
- Mid-rebuild. Implemented: `database`, `internal/{config,datastore,health,logger,cache,fetcher,mailer,queue,jobs,scheduler,storage}`, `pkg/{crypto,envfile,jwtutils,responder,validate,testutils,printext}`, `api/connect`, `email/templates`, `web`, `modules/identity/jwks`. Scaffolds only: most of `internal/**`, the rest of `modules/**` are single-line packages with no API.
- Never add, remove, or rename a top-level package or directory without an explicit request. Extend an existing package instead.
- Porting a plan or doc into code means implementing it, not copying it. Several docs describe a larger surface than the code has.

## Tech Stack & Tooling

- Go 1.27.1, Node >= 24.21, pnpm 12.5.1, Docker (testcontainers), `task`.
- Target stack: `go-chi/chi`, Postgres via pgx, optional valkey, `samber/do` DI, goose as a library, koanf JSON config, LogLayer Go with `log/slog` as the adapter, OpenTelemetry, S3 + local file storage.
- Optional backends are opt-in, never required: cache defaults to in-memory; session store and rate limit default to Postgres (`public.rate_limits` + `fn_check_rate_limit` under advisory locks). Valkey replaces them only when configured.
- LogLayer (`go.loglayer.dev/v3`, per-transport `go.loglayer.dev/transports/*/v3`): CLI pretty, structured file via lumberjack, OTel. Application code logs through `log/slog`; wire LogLayer as the slog handler (`integrations/sloghandler`), never call LogLayer directly in feature code.
- DI is explicit: constructors registered with `samber/do`, composed in `internal/registry`. No global singletons, no package-level `init`. Use skill `golang-samber-do`.

## Build / Test / Lint

- `task dev` — Vite dev server (:3000) + Go API (:3080). `task run` — Go CLI only, debug build with `--env-file=.env.local`. `task build` — one Vite pass that compiles the SPA bundle once and emits both Go binaries (debug and release targets; the email templates compile once and embed into both). `task start` — run the release binary.
- `task test` — frontend + Go unit tests. `task test:go -- ./modules/...` for one package. `task test:go:debug` covers the `debug`-tagged packages (`./cmd/... ./database/...`).
- `task lint` — golangci-lint + oxlint. `task check` — `go vet` + `migrate:validate` + format check. `task format` — gofmt + oxfmt. `task typecheck` — `tsc -b --noEmit`.
- `task rpc:generate` regenerates Go and TypeScript from `api/connect/*.proto`; `task rpc:stale` fails when the contracts changed without regenerating.
- Task targets wrap the CLI 1:1 (`db:migrate` → `migrate:up`, `db:rollback` → `migrate:down`, `db:reset` → `migrate:reset`, etc.), so a renamed command silently breaks the task. Pass extra flags after `--`.
- `task cert:generate` writes `storage/certs/localhost_{key,crt}.pem` via `mkcert`, falling back to a self-signed `openssl` certificate; `task cert:trust` installs the mkcert CA. The nginx service in `docker/compose.yaml` reads those paths.
- Integration tests use `pkg/testutils.StartPostgres` / `StartMailpit` / `StartMinIO` / `StartValkey` / `StartVictoriaLogs` (testcontainers). They need a Docker daemon: call `testutils.SkipWithoutDocker(t)` so a run without one skips rather than fails (macOS CI has none). Never widen a timeout to work around a hung container.

## Architecture

Implemented today — treat as the contract. One line each here; the reasoning, invariants, and traps per package live in `.llms/architecture.md`.

- `database/migrations/` — goose SQL migrations, the single source of schema truth. Never embed DDL or create tables at runtime.
- `database/migrator.go` — engine over goose v3; returns plain structs so `cmd/` never imports goose; runs on one pinned connection, never the pool.
- `database/validate.go` — checks the embedded migration files without a database (`migrate:validate` needs no DSN).
- `database/create.go` — writes the next migration skeleton; refuses a name already in use at any version.
- `database/dump.go`, `backup.go`, `schema_dump.go`, `restore.go` — the dump/restore behind `db:export`/`db:import` (COPY in text format, PK-ordered, idempotent DDL replay).
- `database/compress.go` — `--compression` containers (`none|gzip|zlib|zip`, klauspost/compress) and byte-level format detection.
- `database/seeders/` — idempotent default records behind `migrate:seed`; the caller owns the transaction.
- `modules/<area>/module.go` — the area module the registry loads: it holds its feature list and mounts each feature on one router, so the composition root names one module per area. Features are built from the area's `Deps` struct (the resolved services the registry passes in).
- `modules/<area>/<feature>/schema.go` — table constants and row structs; a table rename touches one line.
- `modules/identity/jwks` — the published key set at `GET /.well-known/jwks.json`: the configured key pair (`auth.private_key`/`auth.public_key`) plus the active signing rows of `public.jwks`, behind one `*jwks.Service` that satisfies `jwtutils.KeyProvider`. The document is a bare RFC 7517 JWK Set, not the API envelope; a `Source` failure degrades to the configured key, an unreadable configured key fails the run before the listener opens.
- `cmd/` — migration, dump/restore, debug, health, config/key, logger/observer lifecycle, smoke commands. Shared rendering in `cmd/helper.go`.
- `pkg/printext` — colour and formatting for human output; `Style` is meaning, `Palette` is colour; pad before colouring.
- `pkg/crypto` — AES-256-GCM `Cipher` (`enc:` prefix), PHC `PasswordHasher`, `KeyGenerator` emitting all four key variables. Do not add another format.
- `pkg/envfile` — dotenv reader/writer preserving comments and key order; owns the `DATABASE_URL` constant.
- `pkg/responder` / `pkg/validate` — API envelope + request validation; no hand-built envelopes. `pkg/jwtutils` — typed JWT claims. `pkg/testutils` — shared testcontainers.
- `internal/datastore` — the single `pgxpool`, `Querier`/`WithTx`, `OpenMigrationDB` for goose, optional `Valkey` client (opt-in via `kvstore.enable`).
- `internal/cache` — one `Cache` contract; Noop whenever caching does not run; in-memory driver (fastcache design, maphash, collision-safe) and Valkey driver (`tango:cache:` prefix).
- `internal/fetcher` — outbound HTTP client on Resty (`resty.dev/v3`); one process-wide client from `config.Fetcher`; attempt timeout, jittered backoff, count-based circuit breaker; errors distinguish network, timeout, cancellation, retry exhaustion, an open circuit, and HTTP status; bodies and credentials are not logged.
- `internal/health` — check engine; check names are generic (`database`, `kvstore`, `storage`), never the product; JSON wire form in `helper.go`: `took_ms`, details ordered, info flattened to top level.
- `internal/logger` — LogLayer behind `*slog.Logger`; sinks `console|file|otlp`; `Slog()` is the only frontend; async zero-drop console/file sinks; `echoSink` fallback on stderr.
- `internal/mailer` — SMTP submission (`emersion/go-smtp` + `go-sasl`) plus the compiled React Email templates, behind one `*mailer.Service`; optional (an empty `mailer.smtp_host` builds a mailer that refuses to send); STARTTLS or implicit TLS, and a credential is refused on an unencrypted connection that leaves the machine (`ErrInsecureAuth`, loopback exempt, `mailer.smtp_allow_plaintext_auth` to opt in); the body is streamed into the session's `DATA` command, never held as a string; the error classes are `ErrNotConfigured`, `ErrNetwork`, `ErrTimeout`, `ErrCanceled`, `ErrAuth`, `ErrInsecureAuth`, `ErrRejected`, `ErrTemporary`.
- `internal/observer` — traces and metrics; explicit exporter options (no env fallback); Prometheus bridge at `otel.metrics.prometheus_path`.
- `internal/queue` — durable Postgres task queue (SKIP LOCKED claim, priority, retries, replayable dead letters, optional payload encryption).
- `internal/jobs` — concrete jobs on the queue; recurring jobs re-enqueue their own next instance.
- `internal/scheduler` — durable cron scheduler; Postgres row lock claims a tick, the queue executes it.
- `internal/storage` — chunked file engine over local FS or S3; content-addressed chunks, manifest in Postgres, staging watcher, upload on the durable queue; flexible multi-purpose keys (`storage.Key`), per-file JSONB metadata, checkpointed resumable uploads, progress counters (endpoint stubbed, see architecture.md TODO(notification)); `BeforeSyncHook`/`AfterSyncHook` extension points (nil default, idempotence contract).
- `internal/config` (implemented) / `internal/kernel`, `internal/transport`, `internal/registry` — see `.llms/architecture.md` for the full contract of each.
- `api/connect/*.proto` — ConnectRPC contracts. `email/templates` — React Email sources. `web` — SPA embed and static serving.

## Library Documentation

- Before writing code against a third-party package, look it up instead of guessing: Context7 MCP (`mcp_context7__resolve_library_id`, then `mcp_context7__query_docs`) for API usage, DeepWiki MCP (`mcp_deepwiki__ask_question` with `owner/repo`) for design intent. Use when the answer depends on a version, the README is thin, or two candidates are compared. Do not use for this repo's own code — read the source.
- The pinned source of truth is the module cache (`go env GOMODCACHE`). When docs and code disagree, the code wins; say so and follow the code.
- Settled library comparisons (cache, kvstore client, schedulers, River, LogLayer/OTel pins) are recorded in `.llms/architecture.md` → "Library decision records", so they do not get re-run. Record a new outcome there when a comparison settles a decision.
- The OTel `otel/log` pin (v0.19.0, three modules held for the `otellog` transport) is fragile: after any `go get`, re-check the pinned versions and `go mod edit -require` them back.

## Conventions

- Config precedence, lowest to highest: built-in defaults → JSON config file → CLI args. `internal/config` is the only place this order is implemented — a command reads the resolved value from the context (`configFrom` / `fullConfigFrom` in `cmd/config.go`), never `os.Getenv` or a flag directly. The environment is a **value table the file references**, not a layer.
- A flag reaches the configuration only through `flagBindings` in `internal/config/flags.go`, and only `serve` has one (`--host`, `--port`, `--base-url`). A flag that changes what a command does (`--dry-run`, `--force`) must never be listed. `storage.local_path` has no flag and no variable of its own.
- Interpolation is how a secret stays out of the file: `${NAME}` inline, `env:NAME` as a whole value. Only the file is interpolated — a variable's own value is data.
- Every query against application tables is built with `github.com/huandu/go-sqlbuilder` (PostgreSQL flavor — the `internal/queue/store.go` idiom: builder → `Build()` → the shared `Querier`). Raw SQL strings are accepted only in tests (assertions and fixtures) and in `database/`'s maintenance tooling (migrator, dump/restore, catalog introspection), where the statements are the server's own or the migrator's bookkeeping. A `SELECT` of a database function counts as a query: build it with `sb.Var(...)`, never a hand-written `$1, $2, $3`.
- Every config key needs a default in `config.Default`, a rule in `config.Validate`, and — when it is a secret — a line in `secretKeys`. `.env.example` carries the secrets plus the deployment variables; any other non-secret default belongs in the config file.
- `internal/config` file layout: one file per stage of a Config's life (`types.go`, `defaults.go`, `keys.go`, `file.go`, `env.go`, `flags.go`, `validate.go`, `config.go`). Do not add a file for a single function.
- Go 1.27 idioms: `encoding/json/v2` (with `omitzero`) — production code imports `encoding/json/v2`, never `encoding/json`; the old package is a test-file concern only — plus `for range n`, `t.Context()` in tests, `errors.Join`, `slices`/`maps`, `min`/`max`, stdlib `uuid`. When migrating a call site, check the behavior deltas in <https://go.dev/doc/jsonv2-migration> — nil slice/map marshals as `[]`/`{}` (not `null`), duplicate object names and invalid UTF-8 error on decode — and add a test for the delta if the input can carry it. `DefaultOptionsV1` exists for a call that genuinely needs v1 behavior; say why in a comment.
- Shut down through context cancellation: servers, queue workers, background jobs stop in order and drain in-flight work. New long-running components must implement stop.
- Comments are for what the code cannot say: an invariant, security rule, protocol requirement, or side effect. Never a comment that restates the code, narrates control flow, or repeats a name. Never leave phase/task/plan markers or signatures such as `phase *`, `Step N`, `TODO(plan)`. Mark real rework with a `TODO`/`FIXME` saying what to change and why.
- Comments must be concise and straightforward. One point per comment, stated in as few words as the idea survives. No filler ("note that", "basically", "in order to", "it should be mentioned"), no preamble, no restating the sentence the code already tells, no narration of what the reader can see (`// increment i`). Say the rule or the why; skip the what. A comment that survives without losing meaning after trimming half its words was written too long.
- Do not add compatibility shims, fallback readers, or dual-write paths. Delete the obsolete path instead.
- Fix lint findings at the source. `//nolint` is not an accepted answer.
- Command output: pluralize through `printext.Plural`, humanize durations through `printext.Duration`, indent per-item lines and leave outcome lines at column zero under a `status:` label so `grep '^status:'` works, and print a `database:` target line before touching anything. A command a script parses prints only its value (`migrate:version`). Colour comes from `pkg/printext`, never a command's own helper; `--json` output is never coloured.

## Common Tasks

- Add a feature module: create `modules/<area>/<feature>/` with the standard file set (`schema.go`, `repository.go`, `service.go`, `handler_rpc.go`, plus `handler.go` for REST, `module.go`), implement the `internal/kernel` module contract, and list it in the area's `features()` in `modules/<area>/module.go` — that is the only registration an existing area needs. The registry loads one module per area: add the area's resolved services to its `Deps` struct when a feature needs one, and add the area itself to `transport.Options.Modules` only when it is new. Business logic lives in `service.go`; handlers do transport mapping only. Persistence goes through `internal/datastore`; never a second pool. Typed IDs come from `go.jetify.com/typeid`; token/code rows use hashes plus DB `uuidv7()`.
- Add an endpoint: write the proto in `api/connect/*.proto`, run `task rpc:generate`, implement the handler, mount it in the registry.
- Add a migration: `task db:create -- add_widgets`. Every migration needs `-- +goose Up` and `-- +goose Down`; version must be 5 digits, consecutive, higher than the last. goose skips a `00000_*.sql` silently — `migrate:validate` and the migrator tests catch it. Migrations are embedded: a new file runs only after a rebuild. Validate with `task db:validate`. Tests derive counts and versions from `database.EmbeddedMigrations()`, so a new file needs no test edits.
- Add a seeder: factory file in `database/seeders/`, append to `All()` in dependency order, name it `<Entity>Seeder`, guard every insert with `ON CONFLICT DO NOTHING`, report existing records as skipped. `Apply` receives a `datastore.Querier`; the command owns the transaction.
- Add a dump object kind: one writer to `dumpDDL`'s section list in `database/schema_dump.go`, placed in restore order; prefer the server's deparse helpers; skip `pg_depend.deptype = 'e'`; read whole lists before per-item queries (`conn busy`); add the round-trip case in `database/backup_test.go`.
- Add a schema-touching test: `testutils.StartPostgres(t).NewDatabase(t)` per test — the shared container DSN is one database for the whole binary; call `NewDatabase` again for a second database. A test that queries a migrated table runs the migrator first (`datastore.OpenMigrationDB` → `database.NewMigrator(...).Up` → close) and only then opens the pool; see `modules/identity/jwks/repository_test.go`. Call `testutils.SkipWithoutDocker(t)` first, so a run without a daemon skips instead of failing.
- Add a config key: define in `internal/config` with default + validation, add to `secretKeys` when secret, honor precedence. A flag only via `flagBindings`; never add a variable for it.
- Add a background job: task type, `QueueConfig`, processor in `internal/jobs/<name>_job.go`; register in `internal/jobs/register.go`. A recurring job re-enqueues its own next instance.
- Add a log sink: name in `LogTransport*`/`LogTransports()` (`internal/config/types.go`), default + `Validate` rule (read only when named), build in `internal/logger`, hold resources in a sink struct for `Shutdown`, add the case to `TestEveryTransportShipsTheSameEntry`.
- Prove logging end to end: `task metrics:up`, `LOG_TRANSPORT=console,file,otlp task metrics:smoke`, `task metrics:query -- 'marker:"tango-logger-smoke"'`. The store is shared — query the marker.
- Prove the mail path end to end: `docker compose -f docker/compose.yaml up -d mailpit`, `task mailer:smoke -- --to=you@example.com`, then read the message back at <http://localhost:8025>. `internal/mailer/integration_test.go` covers the same ground through `testutils.StartMailpit`.
- Send an email: render a template and submit it in one call through the `*mailer.Service` resolved from the registry; a new template needs a `.tsx` in `email/templates/` and a struct in `internal/mailer/data.go` whose field names match the `TemplateProps` the build writes.
- Prove tracing/metrics end to end: `task metrics:up`, `OTEL_TRACING_ENABLE=true OTEL_METRICS_ENABLE=true task metrics:smoke:otel`, then `task metrics:traces` and the `tango_otel_smoke_total` query against VictoriaMetrics. `internal/observer/observer_test.go` covers the same ground without Docker.
- Add a signal setting: shared (address, service identity) in `otel`; signal-specific in its own subsection. Never a second collector address — a different route sets a path. `envKeys` when a deployment sets it.
- Local services: `docker compose -f docker/compose.yaml up -d pgsql`; the full stack plus observability is in the same file (`compose-metrics.yaml` included). `task metrics:up` starts only the observability half.

## Gotchas / Anti-patterns

- `codegen/` is gitignored build output. Never edit or commit it; regenerate with `task rpc:generate`.
- Task targets live in `tasks/*.yml`, one file per group, included with `flatten: true`: `task db:migrate`, never `task database:db:migrate`; flattened names are one flat namespace, so cross-group deps use plain names. Add a group by writing `tasks/<group>.yml` + a flattened include in the root `Taskfile.yml`; do not add tasks to the root file. The `compose:*` targets are the exception and stay in the root file, next to `docker/compose.yaml`.
- `task` (default) runs `--list` without `--sort none` on purpose: alphabetical order keeps each group together.
- Tool configs live in `.config/` — **except the two oxc configs**, which stay at the repository root: both resolve `ignorePatterns` relative to the config file's own directory, so from `.config/` every root-anchored pattern silently stops matching. `ncu` needs `--configFilePath .config --configFileName ncurc.json`. After moving a config, re-check its `$schema` path.
- The root `--env-file` flag adds a dotenv file to the table the config file's directives resolve from; it is not a config layer and never a write target. A subcommand that writes a file (`key:generate`) declares its own flag of that name.
- `app.config.json` is required and is the single source of truth, so resolution is **deferred**: `initConfig` stores the result *and* the failure on the context; only the command that reads it reports it. Never move resolution into a hard failure in `Before` — it would break `config:generate` and `key:generate`.
- Never print a literal secret. `Redacted`/`RedactDSN`/`RedactKVURL` cover every rendering path; all read the one `secretKeys` list, so a new secret is added there and nowhere else.
- Keep the README lean — setup steps, task table, certificates, deployment, license — and free of architecture detail. The "Implemented today" list here, `.llms/architecture.md`, and the code are the source of truth for what runs; update them when a scaffold package becomes real.
- Do not require valkey, S3, or SMTP for the default local path; Postgres plus local storage must be enough.
- Never rewrite a dotenv file the user owns without consent. Destructive overwrite needs an explicit flag.
- Do not run destructive database commands (`migrate:reset`, volume removal) against a database holding state you did not create.
- A command whose action prints `not yet implemented` is not implemented. Never document, test, or rely on it as working, and never report a stub path as validated.
- Health check reports are published by an endpoint: check names stay generic (`database`, `kvstore`, `storage`), no DSN, no URL, no storage path in the published form; the CLI report may show the storage path (`StorageCheckWithTarget`) because an operator owns the machine.

## Committing

- Stage explicit paths only; never `git add -A` / `git add .`. Verify with `git status` before committing.
- Message format: `{feat,fix,docs,refactor,chore}[(scope)]: <concise message>`.
- Never push. Commits are local unless the user asks for a push.
- Never commit on your own initiative — finish the work, recommend a message, and let the user commit. The exception is an explicit commit instruction in that turn.
- Always close a finished piece of work with a recommended commit title in chat, even when you did not commit. Use the format above, keep it one line, and list the changed paths under it.

Never run: `git reset --hard`, `git checkout .`, `git clean -fd`, `git stash`, `git commit --no-verify`, `git push --force`.

## Related Agent Instructions

- `.llms/architecture.md` — per-package design rationale and library decision records (agent-facing; `docs/` is for humans).
- `AGENTS.md` is the single instruction source for all agents (Codex, Elph, Copilot, Cursor, Gemini CLI).
