<img src="https://i.imgur.com/vJfIiId.png" alt="banner" align="left" height="240" />

Starter project template with [Go][golang], [Cobra][cobra], [Viper][viper], [React][react], [TanStack][tanstack]
(Router, Query, Store), and whatever router library you want to use. This aims to make you able to quickly
create awesome app without having to bother with the initial setup.

[![Go](https://img.shields.io/badge/Go-1.26-blue.svg?logo=Go&logoColor=white)](https://go.dev)
[![TypeScript](https://img.shields.io/badge/TypeScript-6.0-blue.svg?logo=typescript&logoColor=blue)](https://www.typescriptlang.org)
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

In this repo I'm using [go-chi][go-chi] but you can change it with whatever library you want.

## 🏁 Quick Start

You will need [`Go >=1.26`][golang], [`Node.js >= 24.14`][nodejs], [`PNPM >= 10.33`][pnpm],
and [`Docker >= 20.10`][docker] installed on your machine.

Also, you need to install the following tools:

```sh
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
go install github.com/bufbuild/buf/cmd/buf@latest
go install github.com/swaggo/swag/cmd/swag@latest
go install github.com/pressly/goose/v3/cmd/goose@latest
go install github.com/goreleaser/goreleaser/v2@latest
go install github.com/anchore/grype/cmd/grype@latest
```

### Up and Running

1. Install the required toolchain & SDK.
2. Find and replace `tango`, `Tango`, and `MyApplication` strings in the source files.
3. Install the required application dependencies: `pnpm install`
4. Create env file for development: `cp .env.example .env.local`
5. Geneate application secret key: `pnpm generate:key --apply`
6. Run project in development mode: `pnpm dev`

Vite serves the frontend on `:3000` and proxies `/api/*` and `/rpc/*` to Go on `:3080`.

Go files are watched and auto-rebuilt.

### Available tasks for this project

| Command      | Description                                     |
| ------------ | ----------------------------------------------- |
| `pnpm dev`   | Vite dev server (:3000) + Go API server (:3080) |
| `pnpm build` | Build frontend + Go binary (single file)        |
| `pnpm start` | Run the production binary                       |
| `pnpm cmd`   | Run Go server directly (`go run -tags debug .`) |
| `pnpm test`  | Run tests (frontend and backend)                |

## 🐳 Publishing Docker Image

Sign in to container registry:

```sh
echo $REGISTRY_TOKEN | docker login REGISTRY_URL --username YOUR_USERNAME --password-stdin
```

Replace `REGISTRY_URL` with your container registry, ie: `ghcr.io` or `docker.io`

Push docker image:

```sh
pnpm docker:push
```

## 🚀 Deployment

Read the [Deployment Guidelines](./docs/deployment.md) for detailed documentation.

## 📚 References

- [Choosing the Right Go Web Framework](https://brunoscheufler.com/blog/2019-04-26-choosing-the-right-go-web-framework)
- [How To Structure A Golang Project](https://blog.boot.dev/golang/golang-project-structure)
- [How to Structure Your Project in Golang](https://medium.com/geekculture/how-to-structure-your-project-in-golang-the-backend-developers-guide-31be05c6fdd9)
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

[cobra]: https://cobra.dev/
[docker]: https://docs.docker.com/engine/install/
[go-chi]: https://github.com/go-chi/chi
[golang]: https://go.dev/doc/install
[license-apache]: https://choosealicense.com/licenses/apache-2.0/
[license-mit]: https://choosealicense.com/licenses/mit/
[nodejs]: https://nodejs.org/en/download
[pnpm]: https://pnpm.io/installation
[react]: https://react.dev/
[riipandi-x]: https://twitter.com/intent/follow?screen_name=riipandi
[tanstack]: https://tanstack.com/
[viper]: https://github.com/spf13/viper
