# AGENTS.md

Modular-monolith Go boilerplate (`tango`): one binary serving HTTP/ConnectRPC API, embedded SPA, and CLI.

Full design rationale per package: **`.llms/architecture.md`** — read the section for the package you're changing before assuming behavior. This file holds the rules; that file holds the reasoning.

## Project Overview

- Built from `cmd/` using `urfave/cli/v3`. Commands: `serve`, `migrate:*`, `db:export`/`db:import`, `key:generate`/`key:rotate`, `health`, smoke tests. Implemented: `key:generate`, `health`, all `migrate:*`, `db:export`/`db:import`, `mailer:smoke`, and `serve` (basic HTTP surface, SPA mount, metrics, middleware). The rest print `not yet implemented`.
- SPA (React 19 + TanStack + Vite) builds into `web/output/` and embeds into the binary. Vite dev server proxies `/api`, `/rpc`, `/.well-known`, `/static` to Go on `:3080`.
- Implemented: `database`, `internal/{config,datastore,health,logger,cache,fetcher,mailer,queue,jobs,scheduler,storage}`, `pkg/{crypto,envfile,jwtutils,responder,validate,testutils,printext}`, `api/connect`, `email/templates`, `web`, `modules/identity/{jwks,signin}`. Scaffolds only: most of `internal/**`, rest of `modules/**`.
- Never add, remove, or rename top-level packages/directories without explicit request. Extend existing packages instead.
- Porting a plan/doc into code means implementing it, not copying it. Docs may describe larger surface than the code has.

## Tech Stack & Tooling

- Go 1.27.1, Node >= 24.21, pnpm 12.5.1, Docker (testcontainers), `task`.
- Target: `go-chi/chi`, Postgres via pgx, optional valkey, `samber/do` DI, goose as library, koanf JSON config, LogLayer Go with `log/slog` adapter, OpenTelemetry, S3 + local storage.
- Optional backends are opt-in: cache defaults to in-memory; session store and rate limit default to Postgres (`public.rate_limits` + `fn_check_rate_limit` under advisory locks). Valkey replaces them only when configured.
- LogLayer (`go.loglayer.dev/v3`): CLI pretty, structured file via lumberjack, OTel. Application code logs through `log/slog`; wire LogLayer as slog handler (`integrations/sloghandler`), never call LogLayer directly in feature code.
- DI is explicit: constructors registered with `samber/do`, composed in `internal/registry` as `do.Package` values. The package is split so it stays composable — `infrastructure.go` names no module, `modules.go` holds only the `Area` seam (`{Name, Package, Mount}`) and never an area's internals, and each area owns its own wiring in `modules/<area>/module.go`. `registry.New(..., extra ...Area)` takes further areas, so a consumer serves its own without editing the registry. No global singletons, no package-level `init`. Use skill `golang-samber-do`.
- Service visibility follows the flat root scope: a service another area or the transport resolves registers in `infrastructure.go` (root), never in one area's `Package`; an area's `Package` holds only the services its own features consume. Two areas sharing a service is the signal to promote it to infrastructure — not to re-register it. `do.Scope` stays unused until an area must hide a service or own a private instance (see `.llms/architecture.md` → "internal/registry").
- A provider only constructs: no database reads or writes, no seeding, no network dial beyond the dependency it resolves. A step that touches state is its own service (e.g. `*jobs.Seeder`) or a runner, so the prewarm walk orders it and its failure fails the run explicitly.

## Build / Test / Lint

- `task dev` — Vite dev server (:3000) + Go API (:3080). `task run` — Go CLI only, debug build with `--env-file=.env.local`. `task build` — one Vite pass, compiles SPA once, emits both Go binaries (debug/release; email templates compile once and embed into both). `task start` — run release binary.
- `task test` — frontend + Go unit tests. `task test:go -- ./modules/...` for one package. `task test:go:debug` covers `debug`-tagged packages (`./cmd/... ./database/...`).
- `task lint` — golangci-lint + oxlint. `task check` — `go vet` + `migrate:validate` + format check. `task format` — gofmt + oxfmt. `task typecheck` — `tsc -b --noEmit`.
- `task rpc:generate` regenerates Go and TypeScript from `api/connect/*.proto`; `task rpc:stale` fails when contracts changed without regenerating.
- Task targets wrap CLI 1:1 (`db:migrate` → `migrate:up`, `db:rollback` → `migrate:down`, etc.), so a renamed command silently breaks the task. Pass extra flags after `--`.
- `task cert:generate` writes `storage/certs/localhost_{key,crt}.pem` via `mkcert`, falling back to self-signed `openssl` certificate; `task cert:trust` installs mkcert CA. nginx service in `docker/compose-dev.yaml` reads those paths.
- Integration tests use `pkg/testutils.StartPostgres` / `StartMailpit` / `StartMinIO` / `StartValkey` / `StartVictoriaLogs` (testcontainers). Need Docker daemon: call `testutils.SkipWithoutDocker(t)` so run without one skips rather than fails (macOS CI has none). Never widen timeout to work around hung container.

## Architecture

Implemented today — treat as the contract. Reasoning/invariants/traps per package live in `.llms/architecture.md`.

- `database/migrations/` — goose SQL migrations, single source of schema truth. Never embed DDL or create tables at runtime.
- `database/migrator.go` — engine over goose v3; returns plain structs so `cmd/` never imports goose; runs on one pinned connection, never the pool.
- `database/validate.go` — checks embedded migration files without a database (`migrate:validate` needs no DSN).
- `database/create.go` — writes next migration skeleton; refuses a name already in use at any version.
- `database/dump.go`, `backup.go`, `schema_dump.go`, `restore.go` — dump/restore behind `db:export`/`db:import` (COPY in text format, PK-ordered, idempotent DDL replay).
- `database/compress.go` — `--compression` containers (`none|gzip|zlib|zip`, klauspost/compress) and byte-level format detection.
- `database/seeders/` — idempotent default records behind `migrate:seed`; transaction is caller-managed.
- `modules/<area>/module.go` — area module the registry loads: holds feature list and mounts each feature on one router; composition root names one module per area. Features built from area's `Deps` struct (resolved services the registry passes in). The area also owns its own wiring — `Package` (services it registers) and `Mount` (resolves `Deps`, validates, returns the module) — so the registry never learns an area's internals.
- `modules/<area>/<feature>/schema.go` — table constants and row structs; table rename touches one line, and columns are named where their query is, never one constant each (use `sqlbuilder.NewStruct` when the feature needs full CRUD).
- `modules/identity/jwks` — published key set at `GET /.well-known/jwks.json`: configured key pair (`auth.private_key`/`auth.public_key`, read with `crypto.DecodeJWK`) plus active signing rows of `public.jwks`, behind one `*jwks.Service` satisfying `jwtutils.KeyProvider`. Signing is dual stack: the key pair and `auth.secret_key` (HMAC) both sign; optional `auth.jwt_algorithm` picks when both are configured and is omitted from the sample, since the material otherwise decides. Registry wraps it in `jwtutils.CachedKeyProvider` and hands interface to feature, so cache is invisible to endpoint. Document is bare RFC 7517 JWK Set, not API envelope; `Source` failure degrades to configured key, module with no provider fails closed, unreadable or mismatched configured key fails run before listener opens, and a symmetric key is never published since its "public" form is the secret.
- `cmd/` — migration, dump/restore, debug, health, config/key, logger/observer lifecycle, smoke commands. Shared rendering in `cmd/helper.go`.
- `pkg/printext` — colour and formatting for human output; `Style` is meaning, `Palette` is colour; pad before colouring.
- `pkg/crypto` — AES-256-GCM `Cipher` (`enc:` prefix), PHC `PasswordHasher`, `KeyGenerator` emitting all four key variables, `DecodeJWK` as reader of base64 JWK form the generator writes, and `ParseHMACKeyHex` for the 32/48/64-byte HMAC secret (distinct from `ParseKeyHex`, which is the AES-256 reader). Do not add another format.
- `pkg/envfile` — dotenv reader/writer preserving comments and key order; manages `DATABASE_URL` constant.
- `pkg/responder` / `pkg/validate` — API envelope + request validation; no hand-built envelopes. `pkg/jwtutils` — typed JWT claims. `pkg/testutils` — shared testcontainers.
- `internal/datastore` — single `pgxpool`, `Querier`/`WithTx`, `OpenMigrationDB` for goose, optional `Valkey` client (opt-in via `kvstore.enable`).
- `internal/cache` — one `Cache` contract; Noop whenever caching does not run; in-memory driver (fastcache design, maphash, collision-safe) and Valkey driver (`tango:cache:` prefix).
- `internal/fetcher` — outbound HTTP client on Resty (`resty.dev/v3`); one process-wide client from `config.Fetcher`; attempt timeout, jittered backoff, count-based circuit breaker; errors distinguish network, timeout, cancellation, retry exhaustion, open circuit, and HTTP status; bodies and credentials are not logged.
- `internal/health` — check engine; check names are generic (`database`, `kvstore`, `storage`), never the product; JSON wire form in `helper.go`: `took_ms`, details ordered, info flattened to top level.
- `internal/logger` — LogLayer behind `*slog.Logger`; sinks `console|file|otlp`; `Slog()` is the only frontend; async zero-drop console/file sinks; `echoSink` fallback on stderr.
- `internal/mailer` — SMTP submission (`emersion/go-smtp` + `go-sasl`) plus compiled React Email templates, behind one `*mailer.Service`; optional (empty `mailer.smtp_host` builds mailer that refuses to send); STARTTLS or implicit TLS, credential refused on unencrypted connection that leaves the machine (`ErrInsecureAuth`, loopback exempt, `mailer.smtp_allow_plaintext_auth` to opt in); body streamed into session's `DATA` command, never held as string; error classes: `ErrNotConfigured`, `ErrNetwork`, `ErrTimeout`, `ErrCanceled`, `ErrAuth`, `ErrInsecureAuth`, `ErrRejected`, `ErrTemporary`.
- `internal/observer` — traces and metrics; explicit exporter options (no env fallback); Prometheus bridge at `otel.metrics.prometheus_path`.
- `internal/queue` — durable Postgres task queue (SKIP LOCKED claim, priority, retries, replayable dead letters, optional payload encryption).
- `internal/jobs` — concrete jobs on the queue; recurring jobs re-enqueue the next instance.
- `internal/scheduler` — durable cron scheduler; Postgres row lock claims a tick, the queue executes it.
- `internal/storage` — chunked file engine over local FS or S3; content-addressed chunks, manifest in Postgres, staging watcher, upload on durable queue; flexible multi-purpose keys (`storage.Key`), per-file JSONB metadata, checkpointed resumable uploads, progress counters (endpoint stubbed, see architecture.md TODO(notification)); `BeforeSyncHook`/`AfterSyncHook` extension points (nil default, idempotence contract).
- `internal/transport/static` — serves `storage.local_path/uploads` at `/static`, outside the throttled group and before the SPA so a missing upload is a 404 rather than `index.html`. What a key is served from is a driver behind `Upload` (`Local` today), so an object-store driver can answer with a redirect without changing the route; the mount owns the headers and the not-found boundary, and refuses to list a directory. It is deliberately not the chunk store: the engine writes content-addressed chunks under `chunks/`, which is never served; a feature that wants a URL writes a whole copy under `uploads/`.
- `internal/config` (implemented) / `internal/kernel`, `internal/transport`, `internal/registry` — see `.llms/architecture.md` for full contract of each.
- `api/connect/*.proto` — ConnectRPC contracts. `email/templates` — React Email sources. `web` — SPA embed and static serving.

## Library Documentation

- When writing code or configs against a library, always look up the ACTUAL documentation first — via MCP Context7, MCP DeepWiki, or the official docs pages.
- Pinned source of truth is module cache (`go env GOMODCACHE`). When docs and code disagree, code wins; say so and follow code.
- Settled library comparisons (cache, kvstore client, schedulers, River, LogLayer/OTel pins) are recorded in `.llms/architecture.md` → "Library decision records". Record new outcome there when comparison settles a decision.
- OTel `otel/log` pin (v0.19.0, three modules held for `otellog` transport) is fragile: after any `go get`, re-check pinned versions and `go mod edit -require` them back.

## Conventions

- Config precedence, lowest to highest: built-in defaults → JSON config file → CLI args. `internal/config` is the only place this order is implemented — command reads resolved value from context (`configFrom` / `fullConfigFrom` in `cmd/config.go`), never `os.Getenv` or flag directly. Environment is a **value table the file references**, not a layer.
- Flag reaches configuration only through `flagBindings` in `internal/config/flags.go`; only `serve` has one (`--host`, `--port`, `--base-url`). Flag that changes what a command does (`--dry-run`, `--force`) must never be listed. `storage.local_path` has no flag and no variable.
- Interpolation keeps secrets out of file: `${NAME}` inline, `env:NAME` as whole value. Only file is interpolated — variable value is data.
- Every query against application tables is built with `github.com/huandu/go-sqlbuilder` (PostgreSQL flavor — `internal/queue/store.go` idiom: builder → `Build()` → shared `Querier`). Raw SQL strings accepted only in tests (assertions and fixtures) and in `database/` maintenance tooling (migrator, dump/restore, catalog introspection), where statements are internal to the server or migrator. A `SELECT` of a database function counts as a query: build it with `sb.Var(...)`, never hand-written `$1, $2, $3`.
- Every config key needs default in `config.Default`, rule in `config.Validate`, and — when secret — line in `secretKeys`. `.env.example` carries secrets plus deployment variables; any other non-secret default belongs in config file.
- Config file layout: one file per stage of Config's life (`types.go`, `values.go`, `defaults.go`, `keys.go`, `file.go`, `env.go`, `flags.go`, `validate.go`, `predicates.go`, `redact.go`, `config.go`). Do not add a file for a single function. The schema file holds types only; the accepted-value constants and the methods that read them live in `values.go`.
- Go 1.27 idioms: `encoding/json/v2` (with `omitzero`) — production code imports `encoding/json/v2`, never `encoding/json`; old package is test-file concern only — plus `for range n`, `t.Context()` in tests, `errors.Join`, `slices`/`maps`, `min`/`max`, stdlib `uuid`. When migrating call site, check behavior deltas in <https://go.dev/doc/jsonv2-migration> — nil slice/map marshals as `[]`/`{}` (not `null`), duplicate object names and invalid UTF-8 error on decode — and add test for delta if input can carry it. `DefaultOptionsV1` exists for call that genuinely needs v1 behavior; say why in a comment.
- Flattened struct literals (Go 1.27, proposal #9859): a field promoted from an anonymous embedded struct is a valid composite-literal key — `rpcMountingModule{name: "rpc", procedures: ...}` — never the nesting-doll form `mountingModule: mountingModule{...}`. Keep the explicit nested form only when promoted names are ambiguous (two embeddings carry the field at the same depth) or when a small group of fields needs the embedded type name as its domain label. Never mix promoted keys with the enclosing embedded field in one literal — the compiler refuses it. Migrate old literals with `go fix -embedlit ./...` (preview with `-diff`).
- Shut down through context cancellation: servers, queue workers, background jobs stop in order and drain in-flight work. New long-running components must implement stop.
- Do not add compatibility shims, fallback readers, or dual-write paths. Delete obsolete path instead.
- Fix lint findings at the source. `//nolint` is not an accepted answer.
- Command output: pluralize through `printext.Plural`, humanize durations through `printext.Duration`, indent per-item lines and leave outcome lines at column zero under `status:` label so `grep '^status:'` works, and print `database:` target line before touching anything. Command a script parses prints only its value (`migrate:version`). Colour comes from `pkg/printext`, never command-specific helper; `--json` output is never coloured.
- Find the right balance between file size and folder depth. Prioritize package cohesion over arbitrary size limits. Split files when they contain unrelated concerns or become unwieldy to navigate; create subdirectories when a folder naturally groups multiple distinct features. Avoid both monolithic files (thousands of lines) and excessive nesting without clear separation of concerns.

## Comment Style

- Comments are for what the code cannot say: an invariant, security rule, protocol requirement, or side effect. Never a comment that restates the code, narrates control flow, or repeats a name.
- Never leave phase/task/plan markers or signatures such as `phase *`, `Step N`, `TODO(plan)`. Mark real rework with a `TODO`/`FIXME` saying what to change and why.
- Never include ownership references like "owned by" — structure may change; describe the invariant or contract instead.
- One point per comment, stated in as few words as the idea survives. No filler ("note that", "basically", "in order to", "it should be mentioned"), no preamble, no restating the sentence the code already tells, no narration of what the reader can see (`// increment i`). Say the rule or the why; skip the what.
- A comment that survives without losing meaning after trimming half its words was written too long.

## Common Tasks

- Add a feature module: create `modules/<area>/<feature>/` with standard file set (`schema.go`, `repository.go`, `service.go`, `handler_rpc.go`, plus `handler.go` for REST, `module.go`), implement `internal/kernel` module contract, list it in area's `features()` in `modules/<area>/module.go` — that is the only registration an existing area needs. Registry loads one module per area: add area's resolved services to its `Deps` struct when feature needs one (never to `internal/registry`). A **new** area also exports `Package` + `Mount` and adds one entry to `registry.Areas()`. Business logic lives in `service.go`; handlers do transport mapping only. Persistence goes through `internal/datastore`; never a second pool. Typed IDs from `go.jetify.com/typeid`; token/code rows use hashes plus DB `uuidv7()`.
- Ship or change an endpoint: update its Yaak request in the same change (see the `api/specs/` rule in Gotchas) — drop the `(unimplemented)` suffix when the endpoint starts serving, and keep the description aligned with `.llms/pocket-id-api-reference.md`.
- Add an endpoint: write proto in `api/connect/*.proto`, run `task rpc:generate`, implement handler, mount in registry.
- Add a migration: `task db:create -- add_widgets`. Every migration needs `-- +goose Up` and `-- +goose Down`; version must be 5 digits, consecutive, higher than last. goose skips `00000_*.sql` silently — `migrate:validate` and migrator tests catch it. Migrations are embedded: new file runs only after rebuild. Validate with `task db:validate`. Tests derive counts and versions from `database.EmbeddedMigrations()`, so new file needs no test edits.
- Add a seeder: factory file in `database/seeders/`, append to `All()` in dependency order, name it `<Entity>Seeder`, guard every insert with `ON CONFLICT DO NOTHING`, report existing records as skipped. `Apply` receives `datastore.Querier`; transaction is command-managed.
- Add a dump object kind: one writer to `dumpDDL`'s section list in `database/schema_dump.go`, placed in restore order; prefer server's deparse helpers; skip `pg_depend.deptype = 'e'`; read whole lists before per-item queries (`conn busy`); add round-trip case in `database/backup_test.go`.
- Add a schema-touching test: `testutils.StartPostgres(t).NewDatabase(t)` per test — shared container DSN is one database for whole binary; call `NewDatabase` again for second database. Test that queries migrated table runs migrator first (`datastore.OpenMigrationDB` → `database.NewMigrator(...).Up` → close) and only then opens pool; see `modules/identity/jwks/repository_test.go`. Call `testutils.SkipWithoutDocker(t)` first, so run without daemon skips instead of failing.
- Add a config key: define in `internal/config` with default + validation, add to `secretKeys` when secret, honor precedence. Flag only via `flagBindings`; never add variable for it.
- Add a background job: task type, `QueueConfig`, processor in `internal/jobs/<name>_job.go`; register in `internal/jobs/register.go`. Recurring job re-enqueues the next instance.
- Add a log sink: name in `LogTransport*`/`LogTransports()` (`internal/config/types.go`), default + `Validate` rule (read only when named), build in `internal/logger`, hold resources in sink struct for `Shutdown`, add case to `TestEveryTransportShipsTheSameEntry`.
- Prove logging end to end: `task metrics:up`, `LOG_TRANSPORT=console,file,otlp task metrics:smoke`, `task metrics:query -- 'marker:"tango-logger-smoke"'`. Store is shared — query the marker.
- Prove mail path end to end: `docker compose -f docker/compose.yaml up -d mailpit`, `task mailer:smoke -- --to=you@example.com`, then read message back at <http://localhost:8025>. `internal/mailer/integration_test.go` covers same ground through `testutils.StartMailpit`.
- Send an email: render template and submit in one call through `*mailer.Service` resolved from registry; new template needs `.tsx` in `email/templates/` and struct in `internal/mailer/data.go` whose field names match `TemplateProps` the build writes.
- Prove tracing/metrics end to end: `task metrics:up`, `OTEL_TRACING_ENABLE=true OTEL_METRICS_ENABLE=true task metrics:smoke:otel`, then `task metrics:traces` and `tango_otel_smoke_total` query against VictoriaMetrics. `internal/observer/observer_test.go` covers same ground without Docker.
- Add a signal setting: shared (address, service identity) in `otel`; signal-specific in its own subsection. Never a second collector address — different route sets a path. `envKeys` when deployment sets it.
- Local services: `docker compose -f docker/compose.yaml up -d pgsql`; full stack plus observability is in same file (`compose-sre.yaml` included). `task metrics:up` starts only observability half.

## Gotchas / Anti-patterns

- `api/specs/` is the file-sync mirror of the Yaak workspace; the sync writes it — never edit its files by hand, change requests through the `yaak` CLI. The Yaak collection is a checklist, **not** the canonical reference: the source of truth is the code plus `.llms/pocket-id-api-reference.md` (the upstream swagger), and a ported endpoint may change shape on the way in — some REST operations become ConnectRPC procedures, so the Yaak request's method and path may legitimately differ from the upstream one. Names carry the audit vocabulary: `[Pocket ID]` (name = the upstream summary exactly) for ported operations, `[Tango]` for tango-only surfaces, and the suffix `(unimplemented)` on any request whose endpoint nothing serves yet — strip the suffix when the endpoint ships.
- `codegen/` is gitignored build output. Never edit or commit it; regenerate with `task rpc:generate`.
- Task targets live in `tasks/*.yml`, one file per group, included with `flatten: true`: `task db:migrate`, never `task database:db:migrate`; flattened names are one flat namespace, so cross-group deps use plain names. Add group by writing `tasks/<group>.yml` + flattened include in root `Taskfile.yml`; do not add tasks to root file. `compose:*` targets are exception and stay in root file, next to `docker/compose-dev.yaml`.
- `task` (default) runs `--list` without `--sort none` on purpose: alphabetical order keeps each group together.
- Tool configs live in `.config/` — **except the two oxc configs**, which stay at repository root: both resolve `ignorePatterns` relative to config file's own directory, so from `.config/` every root-anchored pattern silently stops matching. `ncu` needs `--configFilePath .config --configFileName ncurc.json`. After moving a config, re-check its `$schema` path.
- Root `--env-file` flag adds dotenv file to table the config file's directives resolve from; it is not a config layer and never a write target. Subcommand that writes a file (`key:generate`) declares the flag separately.
- `app.config.json` is required and is single source of truth, so resolution is **deferred**: `initConfig` stores result *and* failure on context; only command that reads it reports it. Never move resolution into hard failure in `Before` — it would break `config:generate` and `key:generate`.
- Never print a literal secret. `Redacted`/`RedactDSN`/`RedactKVURL` cover every rendering path; all read the one `secretKeys` list, so new secret is added there and nowhere else.
- Keep README lean — setup steps, task table, certificates, deployment, license — and free of architecture detail. "Implemented today" list here, `.llms/architecture.md`, and the code are source of truth for what runs; update them when scaffold package becomes real.
- Do not require valkey, S3, or SMTP for default local path; Postgres plus local storage must be enough.
- Never rewrite a dotenv file without user consent. Destructive overwrite needs explicit flag.
- Do not run destructive database commands (`migrate:reset`, volume removal) against database holding state you did not create.
- Command whose action prints `not yet implemented` is not implemented. Never document, test, or rely on it as working, and never report stub path as validated.
- Health check reports are published by an endpoint: check names stay generic (`database`, `kvstore`, `storage`), no DSN, no URL, no storage path in published form; CLI report may show storage path (`StorageCheckWithTarget`) for direct machine access.

## Committing

- Stage explicit paths only; never `git add -A` / `git add .`. Verify with `git status` before committing.
- Message format: `{feat,fix,docs,refactor,chore}[(scope)]: <concise message>`.
- Never push. Commits are local unless user asks for a push.
- Never commit without explicit instruction — finish work, recommend message, and let user commit. Exception is explicit commit instruction in that turn.
- Always close finished piece of work with recommended commit title in chat, even when you did not commit. Use format above, keep it one line, and list changed paths under it.

Never run: `git reset --hard`, `git checkout .`, `git clean -fd`, `git stash`, `git commit --no-verify`, `git push --force`.

## Related Agent Instructions

- `.llms/architecture.md` — per-package design rationale and library decision records (agent-facing; `docs/` is for humans).
- `AGENTS.md` is the single instruction source for all agents (Codex, Elph, Copilot, Cursor, Gemini CLI).
