[![Go](https://img.shields.io/badge/Go-1.27-blue.svg?logo=Go&logoColor=white)](https://go.dev)
[![TypeScript](https://img.shields.io/badge/TypeScript-7.0-blue.svg?logo=typescript&logoColor=blue)](https://www.typescriptlang.org)
[![React](https://img.shields.io/badge/React-19-blue.svg?logo=react)](https://react.dev)
[![Go Report Card](https://goreportcard.com/badge/github.com/riipandi/tango)](https://goreportcard.com/report/github.com/riipandi/tango)

Starter project template built with [Go][golang], [chi][go-chi] (HTTP router), [ConnectRPC][connectrpc] (RPC),
[Postgres][postgres] (database), [React][react], and [TanStack][tanstack] (Router, Query, Store). It exists so you
can start building without repeating the initial setup.

---

```bash
pnpm dlx tiged riipandi/tango myapp-name
```

> [!NOTE]
> This project is a template I use for my personal use, so you may encounter bugs.
> Please review the release notes thoroughly before updating, as breaking changes can occur!

## Quick Start

You will need [`Go >= 1.27`][golang], [`Node.js >= 24.21`][nodejs], [`PNPM >= 12.5`][pnpm], and
[`Docker >= 20.10`][docker] installed on your machine.

1. Install the Go toolchain binaries: `task deps`
2. Find and replace `tango`, `Tango`, and `MyApplication` across the source files.
3. Install the frontend dependencies: `pnpm install`
4. Create your development env file: `cp .env.example .env.local`
5. Start the local Postgres: `docker compose up -d pgsql`
6. Set `DATABASE_URL` in `.env.local` (see `.env.example`)
7. Run the database migrations: `task db:migrate`
8. Start the development servers: `task dev`

Vite serves the frontend on `:3000` and proxies `/api`, `/rpc`, `/.well-known`, and `/static` to Go on `:3080`.
Go files are watched and rebuilt automatically.

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

# Rebuild the schema from scratch.
task db:reset -- --up

# Inspect the current state.
task db:status
task db:version
task db:validate
```

`migrate:validate` checks the embedded files without a database: unparsable names, duplicate or
non-consecutive versions, malformed annotations, and missing Down blocks. It runs as part of
`task check`.

`migrate:status` lists every migration with the time it last ran, and closes with the most recent
run. Times come from the `tstamp` column goose writes when it records a migration, so they show when
a migration last ran, not when its file changed. Times are UTC.

Concurrent runs are safe: the migrator holds a Postgres session advisory lock, so a second process
waits instead of applying the same migration twice.

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

Run `task` to list every target.

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
