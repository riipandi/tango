---
status: planned
updated: 2026-09-18
---

# Phase 01: Transport and Vite Proxy Foundation

## Outcome

Run REST and ConnectRPC side by side in the same Tango process, with predictable development and
production paths.

## Tasks

1. Add an explicit ConnectRPC mount below `/connect/` in `internal/transport`.
2. Keep root protocol routes, `/api`, `/.well-known/*`, `/healthz`, and static asset behavior
   unchanged until their owning phase changes them.
3. Update `vite.config.ts` so `/connect` is proxied to the Go server with the same origin and
   cookie behavior as `/api`.
4. Make the proxy work in both Vite development and preview modes; keep Storybook behavior explicit.
5. Define CORS, credentials, CSRF, request-ID, panic recovery, timeout, and content negotiation
   behavior for ConnectRPC before adding RPC handlers.
6. Add route-table tests proving the Connect prefix is mounted once and does not fall through to
   the SPA handler.
7. Add a transport smoke RPC such as `HealthService.Check` only if it does not conflict with the
   existing health contract; the public health endpoints remain REST.
8. Use Yaak MCP to create and send a smoke gRPC request against the generated Connect service.
   Confirm the selected request protocol, service path, metadata, and response decoding instead
   of manually constructing or editing a Yaak export.
9. Validate the same request through the proxied HTTPS path provided by the `nginx` service in
   `compose.yaml`. The current compose ports are `3443` for the Vite frontend proxy and `8443`
   for the Go backend proxy; use the applicable path for the test being performed.

## Vite proxy target

```ts
const viteProxy = {
  '/api': { target: 'http://127.0.0.1:3080', changeOrigin: true },
  '/connect': { target: 'http://127.0.0.1:3080', changeOrigin: true },
}
```

The exact configuration must preserve the existing proxy options and should not introduce a
second backend origin for browser clients.

## Gate

The SPA can call a local Connect endpoint through Vite, cookies are sent correctly, existing REST
and static-serving tests remain green, and a Yaak MCP gRPC smoke request succeeds against the
intended transport both directly and through the compose-provided HTTPS proxy where applicable.
