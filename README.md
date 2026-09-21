[![Go](https://img.shields.io/badge/Go-1.27-blue.svg?logo=Go&logoColor=white)](https://go.dev)
[![TypeScript](https://img.shields.io/badge/TypeScript-7.0-blue.svg?logo=typescript&logoColor=blue)](https://www.typescriptlang.org)
[![React](https://img.shields.io/badge/React-19-blue.svg?logo=react)](https://react.dev)
[![Postgres](https://img.shields.io/badge/Postgres-18-blue.svg?logo=postgresql)](https://www.postgresql.org)

Starter project template built with [Go][golang], [chi][go-chi] (HTTP router), [ConnectRPC][connectrpc] (RPC),
[Postgres][postgres] (database), [React][react], and [TanStack][tanstack] (Router, Query, Store). It exists so you
can start building without repeating the initial setup.

---

> [!NOTE]
> This project is a template I use for my personal use, so you may encounter bugs.
> Please review the release notes thoroughly before updating, as breaking changes can occur!

## Quick Start

You will need [`Go >= 1.27`][golang], [`Node.js >= 24.21`][nodejs], [`PNPM >= 12.5`][pnpm], and
[`Docker >= 20.10`][docker] installed on your machine.

```bash
pnpm dlx tiged riipandi/tango myapp-name
```

1. Install the Go toolchain binaries: `task deps`
2. Find and replace `tango`, `Tango`, and `MyApplication` across the source files.
3. Install the frontend dependencies: `pnpm install`
4. Write a starter config file: `task config:generate`
5. Generate the secret keys into your env file: `task key:generate`
6. Start the local Postgres: `docker compose up -d pgsql`
7. Set `DATABASE_URL` in `.env.local` (see `.env.example`)
8. Run the database migrations: `task db:migrate`
9. Start the development servers: `task dev`

Vite serves the frontend on `:3000` and proxies `/api`, `/rpc`, `/.well-known`, `/metrics`, and `/static` to Go on `:3080`.
Go files are watched and rebuilt automatically.

### Configuration

`app.config.json` is the single source of truth. `internal/config` merges three layers, lowest to
highest: the built-in defaults, the config file, then the command-line flags. A flag set for one run
wins over the file, which is what makes `serve --port=9000` work without editing anything.

The file is required, so `config:generate` is the first command a fresh checkout runs. It writes
every key with its default and never writes a secret literally: each secret is an `env:` directive
naming the variable that holds it, so the file is safe to keep.

```bash
# Write app.config.json, or a file somewhere else.
task config:generate
go run -tags debug ./cmd config:generate --output=/tmp/app.config.json

# Check the file and the variables it references. Nothing is written.
task config:validate
go run -tags debug ./cmd config:validate --config-file=/tmp/app.config.json
```

The environment is not a layer. A variable reaches a config key only where the file references it,
with `env:NAME` as a whole value or `${NAME}` inline:

```json
{
  "database": { "url": "env:MY_DSN" },
  "server": { "base_url": "http://${MY_HOST}:3080" }
}
```

Name the variable whatever you like; the file decides which key it fills. A variable nobody
referenced cannot change a value, so an unrelated export in a shell can never alter a run.

A key that holds a length of time is written as a plain number of seconds, so the file says `900`
rather than `"15m0s"`. A duration string is still accepted, which is what a hand-written file or a
`${...}` directive may carry.

A directive naming a variable that is not set does not stop a command that does not read that key:
the key keeps its default and only the command that needs it reports the problem. `config:validate`
reports every unresolved variable at once.

The variable names the generated file uses are the conventional ones (`DATABASE_URL`,
`AUTH_SECRET_KEY`), which is also how `key:generate` writes them and how `.env.example` lists them.
A deployment variable is written the same way: `app.mode` asks for `APP_MODE`, `server.base_url` for
`PUBLIC_BASE_URL`, and each `mailer.smtp_*` key for the matching `MAILER_SMTP_*`.

The `mailer` section is optional. With no `mailer.smtp_host` the application runs without sending
mail, so a local checkout needs no mail server; set the `MAILER_SMTP_*` variables to enable it.
`mailer.smtp_secure` selects implicit TLS on connect instead of STARTTLS.
`--env-file` adds a dotenv file to the table the directives resolve from, and wins over the system
environment for a name both set.

### Database

Migrations are embedded in the binary and tracked in the `app_migration` table. `migrate:up` and
`migrate:down` ask for confirmation when they run in a terminal and proceed immediately when piped,
so `task db:migrate` works unattended.

```bash
# Apply everything pending.
task db:migrate

# List what would be applied.
go run -tags debug ./cmd migrate:up --env-file=.env.local --dry-run

# Stop at a version.
go run -tags debug ./cmd migrate:up --env-file=.env.local --to=3

# Roll back the last migration, or several.
task db:rollback
task db:rollback -- --count=3

# Rebuild the schema from scratch. On a database with nothing applied, --up
# applies the migrations, so this also initializes a fresh database.
task db:reset -- --up

# Inspect the current state.
task db:status
task db:version
task db:validate

# Start a new migration. The name is normalized and refused when another
# migration already uses it.
task db:create -- add_widgets

# Seed the default records. Every seeder is idempotent, so a second run
# changes nothing; --dry-run reports without writing.
task db:seed
```

`migrate:create` writes the next free version into `database/migrations` and refuses a name that
another migration already uses, whatever its version. The skeleton has empty Up and Down blocks, so
`migrate:up` reports it as `empty` until statements are added. Migrations are embedded in the binary,
so a new file only reaches `migrate:up` after a rebuild — `go run` and `task db:migrate` rebuild on
every call.

Every command that touches the database opens with the database it resolved, so a run against the
wrong server is visible before anything changes. Credentials are never printed:

```text
$ task db:migrate
database: localhost:5432/tango

  00001 applied 2026-09-21 01:15:32 initialize_schema (23.412 ms)
  00002 applied 2026-09-21 01:15:32 create_identity_tables (35.029 ms)
  ...

status: 9 migrations applied in 146.256 ms
```

A run reports each migration as it finishes, not in one block at the end, so a slow migration leaves
the ones before it visible. The line shape is the same one `migrate:status` prints — version, state,
time, name, and duration — so applying, rolling back, and listing all read alike:

```text
  00009 rolled back 2026-09-21 01:15:32 add_session_remember (7.349 ms)
  00008 rolled back 2026-09-21 01:15:32 create_queue_tables (5.626 ms)
```

The name is shortened because the version already has its own column and `.sql` says nothing, so
`00009_add_session_remember.sql` prints as `add_session_remember`.

Every outcome line carries a `status:` label, so one pattern finds the result of any command:

```bash
go run -tags debug ./cmd migrate:reset --env-file=.env.local --force --up | grep '^status:'
```

```text
status: 9 migrations rolled back in 176.226 ms
status: 9 migrations applied in 131.927 ms
```

`migrate:status` omits the duration, because the recorded time says when a migration ran, not how
long it took, and that command runs nothing to find out. A `--dry-run` lists rows too, with the time
column empty (`-`) and no duration, because nothing has run. Counts are pluralized (`1 migration`,
`9 migrations`) and durations are humanized. `migrate:version` is the exception: it prints the bare
number, because scripts read it directly.

`migrate:seed` creates the default records a fresh database needs. It refuses to run until every
migration is applied — a seeder writes columns the schema must already have — and reports the
pending count with the command to fix it. Seeding writes data, so it asks for confirmation unless
`--force` is passed or stdin is not a terminal, and the whole run is one transaction: a seeder that
fails leaves nothing behind. `--dry-run` reports what would be created without writing anything.

The default user is `admin@example.com` / `@dmin123` (`admin`, display name `Admin Sistem`,
administrator). Every seeder is idempotent, so running `migrate:seed` twice reports the existing
record as skipped instead of creating a second one. The credentials live in
`database/seeders/user_factory.go`; change the password at first login on any deployment.

`migrate:validate` checks the embedded files without a database: unparsable names, duplicate or
non-consecutive versions, malformed annotations, and missing Down blocks. It runs as part of
`task check`.

`migrate:status` lists every migration with the time it last ran, and closes with the most recent
run. Times come from the `tstamp` column goose writes when it records a migration, so they show when
a migration last ran, not when its file changed. Times are UTC.

Concurrent runs are safe: the migrator holds a Postgres session advisory lock, so a second process
waits instead of applying the same migration twice.

### Export and import

`db:export` writes a plain SQL dump of the application schemas — the DDL first, then the rows as one
`COPY` block per table. The file is text, so it can be read, diffed, and reviewed. `db:import` loads
it back.

```bash
# Write to storage/backup/tango-<YYYYMMDD_hhmm>.sql.
task db:export

# Write somewhere else, or take only one half of the dump.
task db:export -- --output=/tmp/dump.sql
task db:export -- --schema-only
task db:export -- --data-only

# Compress the dump. The suffix is added to the name: .sql.gz, .sql.zz, .sql.zip.
task db:export -- --compression=gzip

# Load a dump. The file is a required argument.
task db:import -- storage/backup/tango-20260921_0018.sql.gz

# See what a restore would load, without touching the database.
task db:import -- --dry-run storage/backup/tango-20260921_0018.sql.gz

# Replace the contents of the tables in the dump instead of colliding with them.
task db:import -- --truncate --force storage/backup/tango-20260921_0018.sql
```

The default output is `storage/backup/tango-<YYYYMMDD_hhmm>.sql`, in UTC, and the directory is created
when it is missing. The name is readable to the minute, so a second export inside the same minute
lands on the same path; rather than lose the first dump, the run stops and says so unless
`--overwrite` is passed. An `--output` path you type is used as it is, and its directory is not
created, so a typo stays visible.

`--compression` picks the container: `none` (the default), `gzip`, `zlib`, or `zip`. The dump is
written plain first and compressed once it is complete, so a failure during the dump cannot leave a
half-written archive that still looks valid. The compressed file replaces the plain one, and the
report names the file it wrote and its size.

`db:import` detects the container from the file's own bytes, never from the name, so a renamed dump
still loads and a name that claims a format the bytes do not carry is not believed. A format this
tool does not write, such as `bzip2` or `zstd`, is refused by name. A `--dry-run` reports the tables
and rows a restore would load and changes nothing; it does not open a database at all.

Rows travel through the PostgreSQL `COPY` protocol, so the server does the escaping and a value
holding a tab, a quote, a newline, or the `\.` terminator survives the round trip. Each table is
ordered by its primary key, so two dumps of the same state produce the same file apart from the
header timestamp. Compression is reproducible too: the same dump compresses to the same bytes.

A schema dump is idempotent: applying it twice is safe. `CREATE TABLE`, `CREATE INDEX`,
`CREATE SEQUENCE`, `CREATE SCHEMA`, and `CREATE EXTENSION` carry `IF NOT EXISTS`, and triggers use
`CREATE OR REPLACE TRIGGER`. Postgres has no such form for `CREATE TYPE` or
`ALTER TABLE ... ADD CONSTRAINT`, so those two are wrapped in a `DO` block that checks the catalog
first. That makes the dump safe to replay over an existing schema.

`db:import` loads the `COPY` blocks and ignores the DDL, because the target database gets its schema
from `migrate:up`. That keeps the migrations the single source of truth: a dump cannot quietly
disagree with them. Run `migrate:up` first. The whole load is one transaction, and the blocks are
ordered by foreign key rather than by the order they appear in the file, so a table is never loaded
before the table it references. A failure half way leaves the database untouched.

`--truncate` empties the tables in the dump before loading them, which is what a restore into a
populated database needs. A restore is destructive either way — it replaces rows in the tables the
dump carries — so it asks for confirmation unless `--force` is passed. A piped or non-interactive run
proceeds without a prompt, so `task` and CI never block.

`public.app_migration` is never dumped: it is the migrator's bookkeeping, derived from the migration
files by `migrate:up`, so carrying it would claim a schema state the target does not have.

### Health check

`health` probes the application dependencies and reports one aggregated status. The same result is
published by the REST handler, so the CLI and the API never disagree.

```bash
# Human-readable report (default).
task health

# One word, for a script or a container probe.
task health -- --short

# Machine-readable result, the same shape the REST handler sends.
task health -- --json
```

Text output is a flat list, one fact per line, so it greps and pipes without column padding to
strip. Durations go through `go-humanize`, so a reader sees `235 µs` instead of nanoseconds:

```text
name: Tango
uptime: <1 minute
version: 0.0.0
status: healthy
duration: 521.083 µs
checks: 2 up, 0 down
postgres: up (localhost:5432/postgres)
storage: up (/srv/tango/storage)
```

Colour is added when the output is a terminal, and is dropped when it is redirected, so a log stays
plain text. `NO_COLOR` is honoured. The meaning is fixed across every command: dim for labels and
secondary detail, green for work that succeeded, red for a failure, yellow for a state that is
neither. `--json` is never coloured.

Every check line is `name: status[ optional][ (target)][: error]`, so `grep ': down'` finds every
problem. The target is a full path for storage and a password-free `host:port/database` for Postgres.
Per-check durations and timestamps are absent from the text output — they are per-run numbers a
reader does not act on — and stay in `--json` for a machine that measures them. The JSON body is
byte-identical for the same state, so it diffs cleanly.

Two checks run by default: **postgres** (the pool answers) and **storage** (the application data
directory exists, is writable, and is not world-writable). The data directory is `storage` relative
to the working directory unless the configuration sets `storage.local_path`; the report shows it resolved
to an absolute path.

The vocabulary is deliberate: a component is `up` or `down`, the system is `healthy` or `unhealthy`.
A component marked optional is reported but does not affect the aggregate, so a missing optional
backend is not an outage. Checks run concurrently under a global timeout (`--timeout`, default 5s)
with a short result cache; `--no-cache` runs every check now.

`duration` is how long this one check run took, not process uptime — `uptime` reports that
separately.

Exit codes: `0` healthy, `3` unhealthy, `1` on a usage error such as a missing `DATABASE_URL`. A
database or directory that cannot be used is reported as `unhealthy`, not as a command failure.

### Secret keys

`key:generate` always writes all four variables: `APP_SECRET_KEY` (AES-256, 64 hex characters),
`AUTH_PRIVATE_KEY` and `AUTH_PUBLIC_KEY` (base64-encoded JWK JSON), and `AUTH_SECRET_KEY` (HMAC).

Without `--algorithm` the key pair uses `ES256` and `AUTH_SECRET_KEY` uses `HS256`. Passing an
asymmetric algorithm (`ES384`, `ES512`, `EdDSA`, `RS*`, `PS*`) replaces the key pair; passing an `HS*`
algorithm replaces the HMAC secret. The other role keeps its default.

```bash
# Print the keys to the terminal. Nothing is written.
go run -tags debug ./cmd key:generate

# Create the env file when it is missing.
go run -tags debug ./cmd key:generate --env-file=.env.local

# Update an existing env file: prompts first, or skips the prompt with --overwrite.
go run -tags debug ./cmd key:generate --env-file=.env.local --overwrite

# Pick the algorithms explicitly.
go run -tags debug ./cmd key:generate --algorithm=ES384 --env-file=.env.local

# Same, through the Taskfile.
task key:generate
```

An existing env file is never rewritten without consent: the command asks `replace its key values? [y/N]`
and leaves the file untouched on anything other than `y`/`yes`. Updating keeps the comments, blank lines,
and order, replaces the existing key values, and appends the ones that are missing.

Changing `APP_SECRET_KEY` makes data encrypted with the previous key unreadable, and replacing a signing
key invalidates the tokens signed with it. `key:rotate` (not implemented yet) is intended to re-encrypt
stored data during rotation.

## Available Tasks

Run `task` to list every target. Most targets live in `tasks/`, one file per group; the root
`Taskfile.yml` declares the shared variables, includes them, and keeps the `compose:*` targets
itself, next to the `compose.yaml` they drive.

```text
Taskfile.yml       shared variables, includes, default, compose:*
tasks/
  build.yml        build, start, release, publish
  cert.yml         cert:generate, cert:trust
  cleanup.yml      cleanup, cleanup:dist, cleanup:deps
  database.yml     db:migrate, db:rollback, db:status, db:version, db:validate, db:reset,
                   db:create, db:seed, db:export, db:import, health, key:generate
  deps.yml         deps, update-deps
  dev.yml          dev, run, email:dev, email:build
  docker.yml       docker:build, docker:run, docker:shell, docker:push, docker:prune,
                   docker:images, docker:check
  lint.yml         format, check, lint, typecheck
  rpc.yml          rpc:generate, rpc:lint, rpc:breaking, rpc:stamp, rpc:stale
  test.yml         test, test:go, test:go:debug, test:ui, test:sdk, coverage
```

Each include is flattened, so a task keeps the name it had before the split: `task db:migrate`, not
`task database:db:migrate`. Flattening also keeps one flat namespace, so a task in one file can
depend on a task in another by its plain name.

| Command             | Description                                         |
| ------------------- | --------------------------------------------------- |
| `task dev`          | Vite dev server (:3000) + Go API server (:3080)     |
| `task run`          | Run the Go server directly (debug build)            |
| `task build`        | Build the frontend and the Go binary (single file)  |
| `task start`        | Run the production binary                           |
| `task test`         | Run the frontend and backend tests                  |
| `task typecheck`    | Run TypeScript type checking                        |
| `task lint`         | Run all linters (Go and JS)                         |
| `task check`        | Run `go vet` and the formatting check               |
| `task format`       | Format all files (Go and JS)                        |
| `task key:generate` | Generate the application secret keys                |
| `task cert:generate`| Generate local HTTPS certificates into `storage/certs`|
| `task cert:trust`   | Trust the local CA in the system trust store        |
| `task rpc:generate` | Generate Go and TypeScript from the proto contracts |
| `task rpc:stale`    | Fail when generated code is out of date             |
| `task compose:up`   | Start the docker compose services                   |
| `task compose:down` | Stop the docker compose services                    |

## Local HTTPS

The nginx service in `compose.yaml` reads certificates from `storage/certs`. Generate them with
[`mkcert`](https://github.com/FiloSottile/mkcert), falling back to a self-signed `openssl` certificate when
`mkcert` is unavailable:

```sh
# Writes storage/certs/localhost_key.pem and storage/certs/localhost_crt.pem.
task cert:generate

# Install the mkcert local CA in the system trust store.
task cert:trust
```

`task cert:generate` covers `localhost`, `127.0.0.1`, `::1`, `host.docker.internal`, and
`*.localhost.test`. `storage/` is gitignored, so the certificates stay local.

## Deployment

Build the image with `task docker:build`. Read the [Deployment Guidelines](./docs/deployment.md) for the full procedure.

## License

Licensed under either of [Apache License 2.0][license-apache] or [MIT license][license-mit] at your option.

> Unless you explicitly state otherwise, any contribution intentionally submitted for inclusion in this project by you,
> as defined in the Apache-2.0 license, shall be dual licensed as above, without any additional terms or conditions.

Copyrights in this project are retained by their contributors. See the [LICENSE-APACHE](./LICENSE-APACHE) and
[LICENSE-MIT](./LICENSE-MIT) files for more information.

---

<sub>If you like my work, you can support me via [GitHub sponsors](https://github.com/sponsors/riipandi).</sub>

[![Creator Badge](https://badgen.net/badge/icon/by%20Aris%20Ripandi?label&color=black&labelColor=black)][riipandi-x]

[connectrpc]: https://connectrpc.com/
[docker]: https://docs.docker.com/engine/install/
[go-chi]: https://github.com/go-chi/chi
[golang]: https://go.dev/doc/install
[license-apache]: https://choosealicense.com/licenses/apache-2.0/
[license-mit]: https://choosealicense.com/licenses/mit/
[nodejs]: https://nodejs.org/en/download
[pnpm]: https://pnpm.io/installation
[postgres]: https://www.postgresql.org/
[react]: https://react.dev/
[riipandi-x]: https://twitter.com/intent/follow?screen_name=riipandi
[tanstack]: https://tanstack.com/
