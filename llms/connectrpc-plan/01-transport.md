---
status: planned
updated: 2026-09-18
---

# Phase 01: Transport and Vite Proxy Foundation

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
9. Use Yaak MCP to create and send a smoke gRPC request against the generated Connect service.
   Confirm the selected request protocol, service path, metadata, and response decoding instead
   of manually constructing or editing a Yaak export.
10. Validate the same request through the proxied HTTPS path provided by the `nginx` service in
   `compose.yaml`. The current compose ports are `3443` for the Vite frontend proxy and `8443`
   for the Go backend proxy; use the applicable path for the test being performed.
11. Verify whether the Nginx path supports gRPC HTTP/2. If gRPC is required through HTTPS, configure
    and test the appropriate TLS HTTP/2 listener and gRPC upstream forwarding; ordinary
    `proxy_pass` over HTTP/1.1 is not sufficient evidence for gRPC.
12. Keep the Connect protocol and gRPC protocol test cases separate. A Connect JSON/HTTP/1.1 request
    may pass while a gRPC request fails due to HTTP/2 or proxy configuration.
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
and gRPC transports, both directly and through every HTTPS proxy path claimed as supported.

## Commit

Commit this phase as one atomic conventional commit containing the transport mount, Vite proxy,
Nginx/proxy changes if required, transport tests, and Yaak smoke evidence.
