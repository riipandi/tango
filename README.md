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
6. Run the database migrations: `go run -tags debug ./cmd migrate:up --env-file=.env.local`
7. Start the development servers: `task dev`

Vite serves the frontend on `:3000` and proxies `/api`, `/rpc`, `/.well-known`, and `/static` to Go on `:3080`.
Go files are watched and rebuilt automatically.

The env file currently carries only `HOST` and `PORT`; the remaining configuration keys are being rebuilt alongside
`internal/config`. The `db:migrate` and `secrets:generate` tasks still call the previous CLI shape and fail against
the current `cmd/` subcommands — use `go run -tags debug ./cmd migrate:up` and `go run -tags debug ./cmd key:generate`
until they are updated.

## Available Tasks

Run `task` to list every target.

| Command             | Description                                       |
| ------------------- | ------------------------------------------------- |
| `task dev`          | Vite dev server (:3000) + Go API server (:3080)   |
| `task run`          | Run the Go server directly (debug build)          |
| `task build`        | Build the frontend and the Go binary (single file)|
| `task start`        | Run the production binary                         |
| `task test`         | Run the frontend and backend tests                |
| `task typecheck`    | Run TypeScript type checking                      |
| `task lint`         | Run all linters (Go and JS)                       |
| `task check`        | Run `go vet` and the formatting check             |
| `task format`       | Format all files (Go and JS)                      |
| `task rpc:generate` | Generate Go and TypeScript from the proto contracts|
| `task rpc:stale`    | Fail when generated code is out of date           |
| `task compose:up`   | Start the docker compose services                 |
| `task compose:down` | Stop the docker compose services                  |

## Local Certificates

The nginx service in `compose.yaml` expects certificates in `storage/certs`:

```sh
mkdir -p storage/certs && mkcert \
  -key-file storage/certs/localhost_key.pem \
  -cert-file storage/certs/localhost_crt.pem \
  localhost 127.0.0.1 ::1 host.docker.internal \
  "*.localhost.test"

# Install the local CA in the system trust store.
mkcert -install
```

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
