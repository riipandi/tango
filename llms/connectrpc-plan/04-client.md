---
status: done
updated: 2026-09-19
---

# Phase 04: Authentication Transport, Frontend, REST SDK, and Connect Client

> Completed 2026-09-19 with the SPA skeleton still unmounted (no app entry yet — `index.html`
> keeps the loader only), so the deliverable is the auth transport plus the worker/client library
> the SPA will consume:
>
> - **Token model** (documented in endpoint-reference): session row = token family; opaque
>   rotating refresh token in `tango_session` (HttpOnly); 10-minute RS256 access JWT
>   (`tango:internal`/`tango:rpc`) mirrored in the `tango_access` cookie scoped to `/api/auth`;
>   bridge `POST /api/auth/token` (REST, retained) returns the bearer and rotates the refresh
>   token; revocation is exact because RPC verification re-checks the session by `sid`.
> - **Connect handlers**: `AuthService.SignIn/SignOut/GetSession` live in
>   `modules/identity/session/handler_rpc.go` with a per-procedure bearer guard (SignIn stays
>   public); cookies ride `Set-Cookie` on the Connect response; ForgotPassword/ResetPassword stay
>   `Unimplemented` until the password-flow cutover phase. `rpcerr.RecoverOption` now attaches to
>   every registered service so panics answer Connect internal errors, not plain text.
> - **Worker**: `comlink` + `vite-plugin-comlink` + `@connectrpc/connect-web` are pinned; the
>   comlink plugin registers first with `worker.plugins`; `app/auth.worker.ts` exposes
>   bootstrap/getAccessToken/refresh/signOut/dispose — bootstrap and refresh use same-origin
>   `fetch(..., { credentials: 'include' })`, only the access token is held (in memory), refresh
>   tokens never cross to the UI thread, and sign-out falls back to the cookie channel when the
>   bearer is already dead. `app/rpc/client.ts` builds the `/rpc` transport with per-request
>   bearer injection from the worker.
> - **Codegen**: connect-es v2 removed the separate TS plugin — `protoc-gen-es` emits the service
>   descriptors; `buf.gen.yaml` drops the connect-es plugin and `*_connect.ts` outputs are gone.
> - **Tests**: Go bridge/rotation/revocation integration tests (testcontainers) plus 9 vitest
>   cases (new `app` vitest project) covering bootstrap, silent refresh, typed `AuthError`,
>   sign-out fallback, header injection, and anonymous calls. Yaak evidence (the workspace was
>   reorganised after this phase; the smoke folder is gone): SignIn 200 + cookies, bridge 200 +
>   access token, GetSession (bearer) 200, SignOut 200 + cookie clears, and the revoked bearer
>   answers 401 afterwards.
> - **Deferred by design**: tasks 11–12 (REST SDK method removal) and 14–16 (TanStack call-site
>   migration) belong to the domain cutover phases — no SPA exists yet; `api/client` is documented
>   as REST-only (README) and stays untouched until cutover deletes each route.

## Outcome

Make the SPA, admin console, and internal tools use typed ConnectRPC clients while keeping
`api/client` as the REST-only SDK for retained HTTP and protocol endpoints.

## Tasks

1. Decide the internal access/refresh token format, issuer, audience, TTLs, scopes, rotation,
   revocation, logout behavior, and cookie attributes. Keep these tokens distinct from OIDC RP
   access and refresh tokens.
2. Add `comlink` and `vite-plugin-comlink` as frontend dependencies and pin both in the package
   lock. The plugin requires Vite 5 or newer, which is satisfied by the project toolchain.
3. Configure `vite-plugin-comlink` near the beginning of the Vite plugin list and include it in
   `worker.plugins`. Add the plugin's client type reference to `vite-env.d.ts`.
4. Define a typed `AuthWorkerApi` as exports from a dedicated worker module. Instantiate it from
   the UI with the plugin's typed `ComlinkWorker<typeof import('./auth.worker')>` API; do not call
   `Comlink.wrap()` or `Comlink.expose()` manually.
5. The public worker API should
   expose commands such as bootstrap, get access token, refresh, sign out, and dispose; it must
   not expose refresh-token values to the UI thread.
6. Define the web worker ownership model for token handling. A worker cannot read `HttpOnly`
   cookies; choose and document a secure token bridge before implementation. Prefer worker-owned
   same-origin `fetch(..., { credentials: 'include' })` for cookie-backed bootstrap/refresh, with
   only the short-lived access token held in worker memory.
7. Configure the worker's origin and lifecycle using the plugin-supported worker options. Do not
   rely on a wildcard origin for an auth worker; if the plugin does not expose an origin control,
   enforce same-origin worker URLs and document that limitation. Release the worker endpoint and
   terminate it on logout, auth reset, or application shutdown.
8. Decide whether production uses the plugin's bundled classic worker or an explicit module worker
   (`type: 'module'`). Verify the selected mode in Vite dev, Vite preview, and the embedded release
   build.
9. Add a Connect client transport generated from `api/connect/*.proto`, configured with the
   same-origin `/rpc` base URL and credentials.
10. Add `/rpc` to the Vite proxy and use a relative base URL in the browser.
11. Update `api/client` to retain only REST namespaces and methods for endpoints marked `REST` or
   REST portions of `REST + ConnectRPC`.
12. Remove `api/client` methods for routes that move exclusively to ConnectRPC, including their
   schemas, fixtures, exports, and tests.
13. Keep `raw()` or dedicated REST protocol methods for OAuth/OIDC, WebAuthn, binary, health,
   discovery, and other retained HTTP endpoints.
14. Update TanStack Query/Router call sites: internal application calls use generated Connect
   clients, while protocol and HTTP calls use `api/client`.
15. Mirror Connect errors in generated/typed RPC client errors without reintroducing the REST
   envelope; keep REST error handling in `api/client`.
16. Update vitest fixtures and tests for both transports, asserting RPC method paths/messages and
   REST method URLs/bodies/headers separately.
17. Update `api/client/README.md` to state clearly that the SDK is REST-only and document the
   generated Connect client as a separate integration.
18. Update Yaak requests through Yaak MCP whenever a REST request, Connect Protocol request, auth header,
    metadata field, message, expected status, or response shape changes. Do not edit Yaak export
    files manually.
19. Use same-origin Connect Protocol as the browser transport. Connect-Web or native gRPC are
    optional compatibility transports and require a separate explicit decision.
20. Test session cookies, API-key metadata, `Authorization`, CSRF behavior, and request IDs through
   both Vite's `/rpc` proxy and the Nginx HTTPS proxy.
21. Test the plugin-generated worker API and lifecycle: login, access-token injection, refresh rotation,
    concurrent refresh, expiry, logout, worker restart, multiple tabs, and revocation. Test that
    Comlink exceptions are converted into typed auth errors, no token is accidentally
    structured-cloned to the UI thread, and refresh tokens are never sent as bearer tokens to
    ordinary RPCs.

## Gate

The SPA and admin console have no calls to internal `/api` routes, `api/client` contains only
retained REST methods, generated Connect clients cover all internal RPC namespaces, and the
protocol client still passes OAuth/OIDC and WebAuthn tests. Yaak MCP REST and Connect Protocol
requests match
the active contracts.

## Commit

Commit each completed client migration as one atomic conventional commit containing generated
Connect client output, SPA/admin caller changes, REST SDK cleanup, vitest coverage, Vite usage, and
Yaak updates for the affected services.
