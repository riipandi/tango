---
status: done
updated: 2026-09-19
---

# Phase 01: Transport and Vite Proxy Foundation

> Completed 2026-09-19. Delivered: `/rpc/` mount in `internal/transport/http.go` (chi `Group` +
> `Mount("/rpc", http.StripPrefix("/rpc", rpcHandler()))` — chi's `Mount` shifts only its route
> context, never `r.URL.Path`, so the Connect mux needs an explicit `StripPrefix`), smoke RPC
> `tango.system.v1.HealthService/Check` (`api/connect/system.proto`, buf-generated Go in
> `gen/proto/go/`, committed), `middleware.RequestTimeout` (30s RPC deadline), and
> `middleware.BearerAuth` (Connect-shaped 401; wired to protected services in phase 03). Route-table
> tests pin the smoke call, Connect 404 fallthrough, and 405 on GET. Vite proxy: `/rpc` already in
> `viteProxy` — verified through dev (:3000) and preview (:4173); Storybook stays explicit
> (`server: undefined`). Yaak evidence: folder `[ConnectRPC] System (smoke)`, requests
> `POST /rpc/tango.system.v1.HealthService/Check` (`rq_PWa9yMokFq`, direct :3080) and
> `…(nginx HTTPS)` (`rq_yPfrPo7huW`, :8443 over HTTP/1.1) — both 200.
>
> RPC transport policy (decided before handlers are added): bearer-only authentication for
> protected RPCs — cookies never authorize an RPC, so CSRF does not apply below `/rpc/`; CORS and
> request-ID ride the global root middleware; panic recovery via `connect.WithRecover`; content
> negotiation is Connect JSON and proto over HTTP/1.1. Server reflection: none served — Connect
> Protocol needs no reflection and production reflection stays disabled; Yaak requests use explicit
> procedure paths. Nginx needed no changes: the catch-all `location /` on :8443 already forwards
> `/rpc` with `Authorization` in its allowlist.

## Outcome

Run REST and ConnectRPC side by side in the same Tango process, with predictable development and
production paths.

## Tasks

1. Add an explicit ConnectRPC mount below `/rpc/` in `internal/transport`; keep REST mounted below
   `/api/`.
2. Keep root protocol routes, `/api`, `/.well-known/*`, `/healthz`, and static asset behavior
   unchanged until their owning phase changes them.
3. Update `vite.config.ts` so `/rpc` is proxied to the Go server with the same origin and cookie
   behavior as `/api`.
4. Make the proxy work in both Vite development and preview modes; keep Storybook behavior explicit.
5. Define CORS, `Authorization` metadata, cookie credentials, CSRF, request-ID, panic recovery,
   timeout, and content negotiation behavior for ConnectRPC before adding RPC handlers.
6. Make the RPC authentication middleware require `Authorization: Bearer` for protected RPCs.
   Do not silently fall back to access-token cookies for RPC authorization.
7. Add route-table tests proving the `/rpc` prefix is mounted once and does not fall through to
   the SPA handler.
8. Add a transport smoke RPC such as `HealthService.Check` only if it does not conflict with the
   existing health contract; the public health endpoints remain REST.
9. Use Yaak MCP to create and send a smoke Connect Protocol HTTP request against the generated
   Connect service. Confirm the HTTP method, `/rpc` routing prefix, procedure path, metadata,
   content type, and response decoding instead
   of manually constructing or editing a Yaak export.
10. Validate the same request through the proxied HTTPS path provided by the `nginx` service in
   `compose.yaml`. The current compose ports are `3443` for the Vite frontend proxy and `8443`
   for the Go backend proxy; use the applicable path for the test being performed.
11. Verify that unary Connect Protocol requests work through the existing Nginx `proxy_pass` over
    HTTPS and HTTP/1.1. Configure HTTP/2 forwarding only if the proto declares streaming methods or
    native gRPC compatibility is explicitly added.
12. Keep Connect Protocol and optional native gRPC compatibility tests separate. A successful
    Connect JSON/HTTP/1.1 request is the required baseline; native gRPC is not the baseline.
13. Define how server reflection is exposed for Yaak development/test requests and ensure production
    reflection is disabled or protected by explicit authorization.

## Vite proxy target

```ts
const viteProxy = {
  '/api': { target: 'http://127.0.0.1:3080', changeOrigin: true },
  '/rpc': { target: 'http://127.0.0.1:3080', changeOrigin: true },
}
```

The exact configuration must preserve the existing proxy options and should not introduce a
second backend origin for browser clients.

## Gate

The SPA can call a local `/rpc` Connect endpoint through Vite, cookies are sent correctly, existing REST
and static-serving tests remain green. Yaak MCP smoke requests succeed for the intended Connect
and Connect Protocol transports, both directly and through every HTTPS proxy path claimed as
supported.

## Commit

Commit this phase as one atomic conventional commit containing the transport mount, Vite proxy,
Nginx/proxy changes if required, transport tests, and Yaak smoke evidence.
