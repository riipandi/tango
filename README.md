<img src="https://i.imgur.com/vJfIiId.png" alt="banner" align="left" height="240" />

Starter project template with [Go][golang], [Kong][kong] (CLI), [Koanf][koanf] (config), [chi][go-chi],
[React][react], and [TanStack][tanstack] (Router, Query, Store). This aims to make you able to quickly
create awesome app without having to bother with the initial setup.

[![Go](https://img.shields.io/badge/Go-1.27-blue.svg?logo=Go&logoColor=white)](https://go.dev)
[![TypeScript](https://img.shields.io/badge/TypeScript-7.0-blue.svg?logo=typescript&logoColor=blue)](https://www.typescriptlang.org)
[![React](https://img.shields.io/badge/React-19-blue.svg?logo=react)](https://react.dev)
[![Go Report Card](https://goreportcard.com/badge/github.com/riipandi/tango)](https://goreportcard.com/report/github.com/riipandi/tango)
[![Contributions](https://img.shields.io/badge/Contributions-welcome-blue.svg?color=gray)](https://github.com/riipandi/tango/graphs/contributors)
<!-- [![Release](https://img.shields.io/github/v/release/riipandi/tango?logo=docker&logoColor=white)](https://github.com/riipandi/tango/releases) -->
<!-- [![CI Test](https://github.com/riipandi/tango/actions/workflows/test.yml/badge.svg)](https://github.com/riipandi/tango/actions/workflows/test.yml) -->
<!-- [![CI Release](https://github.com/riipandi/tango/actions/workflows/release.yml/badge.svg)](https://github.com/riipandi/tango/actions/workflows/release.yml) -->

---

```bash
pnpm dlx tiged riipandi/tango myapp-name
```

> [!NOTE]
> This project is a template I use for my personal use, so you may encounter bugs.
> Please review the release notes thoroughly before updating, as breaking changes can occur!

## 🏁 Quick Start

You will need [`Go >= 1.27`][golang], [`Node.js >= 24.21`][nodejs], [`PNPM >= 12.3`][pnpm],
and [`Docker >= 20.10`][docker] installed on your machine.

1. Install Go toolchain binaries (lint, migrate, release): `task deps`
2. Find and replace `tango`, `Tango`, and `MyApplication` strings in the source files.
3. Install application dependencies: `pnpm install`
4. Create env file for development: `cp .env.example .env.local`
5. Start the local Postgres: `docker compose up -d pgsql`
6. Generate application secrets: `task secrets:generate -- --apply`
7. Run database migrations: `task db:migrate`
8. Run the project in development mode: `task dev`

Vite serves the frontend on `:3000` and proxies `/api/*` to Go on `:3080`.
Go files are watched and auto-rebuilt.

### Available tasks

| Command           | Description                                     |
| ----------------- | ----------------------------------------------- |
| `task dev`        | Vite dev server (:3000) + Go API server (:3080) |
| `task run`        | Run the Go server directly (debug build)        |
| `task build`      | Build frontend + Go binary (single file)        |
| `task start`      | Run the production binary                       |
| `task db:migrate` | Run database migrations                         |
| `task test`       | Run tests (frontend and backend)                |
| `task lint`       | Run all linters (Go + JS)                       |

### Generate Certificates

```sh
# Generate local development certificates
mkdir -p storage/certs && mkcert
  -key-file storage/certs/localhost_key.pem \
  -cert-file storage/certs/localhost_crt.pem \
  localhost 127.0.0.1 ::1 host.docker.internal \
  "*.localhost.test"

# Install the local CA in the system trust store.
mkcert -install
```

## 🏗 Architecture

A modular monolith: one binary, features are self-contained modules.

| Path                                | Purpose                                                             |
| ----------------------------------- | ------------------------------------------------------------------- |
| `cmd/launcher`                      | CLI entry (`serve`, `db migrate`, `secrets`) via Kong               |
| `internal/kernel`                   | Module contract + registry (routes, middleware, start/stop)         |
| `internal/registry`                 | Wires modules and shared dependencies                               |
| `internal/config`                   | Koanf layering: defaults ← env file ← system env ← flags            |
| `internal/datastore`                | Postgres pool, health, transactions                                 |
| `internal/transport`                | HTTP server, middleware, SPA serving                                |
| `internal/logger`                   | LogLayer logging incl. the task queue adapter                       |
| `modules/*`                         | Feature modules, each owning its schema and store                   |
| `pkg/antree`                        | Postgres-backed in-process task queue (see below)                   |
| `pkg/*{crypto,jwtutils,responder}*` | Shared building blocks: secret crypto, JWT (jwx), response envelope |
| `pkg/testutils`                     | Test helpers: shared testcontainers Postgres                        |
| `database/migrations`               | Goose SQL migrations, the single source of schema truth             |

Typed identifiers (`user_*, audit_*`) come from `go.jetify.com/typeid`.

### Task queue

Background jobs run on `pkg/antree`, a port of [backlite][backlite]
(MIT): a type-safe, Postgres-backed queue that executes inside the app
process — no broker needed. Queues are registered in the registry, tasks
are plain Go types encoded with `encoding/json/v2`, and the schema lives
in `database/migrations/00010_create_queue_tables.sql` (`queue_tasks`,
`queue_tasks_completed`).

Configuration via env (see `.env.example`):

| Key                      | Default | Description                                    |
| ------------------------ | ------- | ---------------------------------------------- |
| `QUEUE_WORKERS`          | `4`     | Goroutines executing tasks concurrently        |
| `QUEUE_RELEASE_AFTER`    | `300`   | Seconds before a stuck claimed task is retried |
| `QUEUE_CLEANUP_INTERVAL` | `21600` | Seconds between expired-task cleanups          |

## 🚀 Deployment

Build the image with `docker build -f deploy/Dockerfile .` and read the
[Deployment Guidelines](./docs/deployment.md) for detailed documentation.

## 📚 References

- [Choosing the Right Go Web Framework](https://brunoscheufler.com/blog/2019-04-26-choosing-the-right-go-web-framework)
- [How To Structure A Golang Project](https://blog.boot.dev/golang/golang-project-structure)
- [What's the best way to do authentication in modern applications](https://neciudan.dev/most-secure-way-to-store-auth-token)

## 🪪 License

Licensed under either of [Apache License 2.0][license-apache] or [MIT license][license-mit] at your option.

> Unless you explicitly state otherwise, any contribution intentionally submitted for inclusion in this project by you,
> as defined in the Apache-2.0 license, shall be dual licensed as above, without any additional terms or conditions.

Copyrights in this project are retained by their contributors.

See the [LICENSE-APACHE](./LICENSE-APACHE) and [LICENSE-MIT](./LICENSE-MIT) files for more information.

---

<sub>🤫 Psst! If you like my work you can support me via [GitHub sponsors](https://github.com/sponsors/riipandi).</sub>

[![Creator Badge](https://badgen.net/badge/icon/by%20Aris%20Ripandi?label&color=black&labelColor=black)][riipandi-x]

[docker]: https://docs.docker.com/engine/install/
[go-chi]: https://github.com/go-chi/chi
[golang]: https://go.dev/doc/install
[kong]: https://github.com/alecthomas/kong
[koanf]: https://github.com/knadh/koanf
[backlite]: https://github.com/mikestefanello/backlite
[license-apache]: https://choosealicense.com/licenses/apache-2.0/
[license-mit]: https://choosealicense.com/licenses/mit/
[nodejs]: https://nodejs.org/en/download
[pnpm]: https://pnpm.io/installation
[react]: https://react.dev/
[riipandi-x]: https://twitter.com/intent/follow?screen_name=riipandi
[tanstack]: https://tanstack.com/
