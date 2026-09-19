---
status: planned
updated: 2026-09-19
owner: tango-connectrpc-remediation
---

# Phase 02 — Transport and Error Contract

Prerequisite: phase 01 done.

## Task 02.1 — Correct the HTTP method column for `/rpc` rows

**Finding F3 (P1).** `docs/api-endpoint.md` and `llms/endpoint-reference.md` publish `GET`, `PUT`,
and `DELETE` for 10 ConnectRPC procedures. Connect Protocol serves a unary procedure over `GET`
only when the proto declares `idempotency_level = NO_SIDE_EFFECTS`
(`connectrpc.com/connect@v1.21.0/protocol_connect.go:74-76`). No file in `api/connect/` declares
it, so every procedure is POST-only and a `GET` answers
`405` with `Allow: POST` (`internal/transport/route_table_test.go:252`).

Affected rows (identical in both documents):

| Procedure | Documented | Actual |
| --- | --- | --- |
| `AuthService/GetSession` | GET | POST |
| `AccountService/GetAccount` | GET | POST |
| `AccountService/ChangePassword` | PUT | POST |
| `AccountService/ListSessions` | GET | POST |
| `AccountService/RevokeSession` | DELETE | POST |
| `MfaService/GetTotpStatus` | GET | POST |
| `MfaService/DisableTotp` | DELETE | POST |
| `ApplicationConfigurationService/Get` | GET | POST |
| `ApplicationConfigurationService/GetAll` | GET | POST |
| `ApplicationConfigurationService/Update` | PUT | POST |

The Yaak collection already uses POST for all ten, so only the documents are wrong.

### Required final shape

The Method column reads `POST` for every `/rpc` row. The documents state once, in the transport
section, that ConnectRPC procedures are unary POST unless the proto declares otherwise.

### Steps

1. Replace the Method cell with `POST` for the ten rows in `docs/api-endpoint.md`.
2. Apply the same change to `llms/endpoint-reference.md`.
3. Add a sentence to the transport section of both documents: unary ConnectRPC procedures are
   called with `POST`; `GET` is reserved for procedures that declare `idempotency_level`.
4. Add a route-table assertion that pins the rule: a `GET` on a non-idempotent procedure answers
   `405` with `Allow: POST`. Extend the existing `TestRPCRejectsWrongMethods` instead of adding a
   parallel test.
5. Do not add `idempotency_level` to the protos. Changing the wire contract is a separate,
   owner-approved decision.

Validation: `task test:go -- ./internal/transport/...` passes; no `/rpc` row carries a non-POST
method.

Commit: `docs(rpc): correct the HTTP method for connect procedures`

## Task 02.2 — State the supported transports truthfully

**Finding F4 (P1).** `llms/connectrpc-plan/03-server.md:21` claims:

> Unsupported transports (native gRPC, gRPC-Web) are not mounted and answer not_found; unary
> Connect over HTTP/1.1 is the only claimed transport.

That is false. `connect.NewUnaryHandler` installs the Connect, gRPC, and gRPC-Web protocol handlers
by default (`connectrpc.com/connect@v1.21.0/handler.go:400-404`), and no handler option restricts
them. Probe through `newRPCRouter`:

| Request | Result |
| --- | --- |
| `Content-Type: application/grpc-web+proto` | 200, `application/grpc-web+proto`, `grpc-status: 0` |
| `Content-Type: application/grpc` + `TE: trailers` | 200, `application/grpc` |
| `Content-Type: application/json` | 200 (Connect JSON) |

Only reflection is genuinely absent (`TestRPCRejectsReflection`). The plan both overstates
isolation and understates capability.

### Required final shape

Pick one and encode it:

- **Option A (close the gap).** Mount a `connect.WithRequestGate` that rejects a request whose
  `Peer.Protocol` is not `connect.ProtocolConnect` with `rpcerr.NotFound`. This matches the plan
  claim and removes two unclaimed attack surfaces. `Peer.Protocol` is available in the gate
  (`connect.go:353`, `handler.go:341-352`).
- **Option B (keep the capability).** Keep gRPC and gRPC-Web mounted, and correct
  `03-server.md` to state that all three protocols are served, that the browser uses Connect
  Protocol, and that no test currently pins gRPC behavior.

**Option B is the smaller and safer change**: closing the transports is a behavior change for any
external gRPC client, and the plan explicitly marks gRPC as optional rather than forbidden. Choose
Option A only if the owner wants the narrower surface.

Either way, the false claim is removed and a test pins the chosen behavior.

### Steps

1. Record the decision in this file under "Decision".
2. Option A: add the gate to `newRPCRouter` in `internal/transport/rpc.go` and a test asserting a
   gRPC and a gRPC-Web request answer `404 not_found`. Option B: extend
   `internal/transport/route_table_test.go` with a test that sends a gRPC-Web request and asserts
   the served protocol, so the capability is pinned rather than assumed.
3. Rewrite the claim in `llms/connectrpc-plan/03-server.md` to match the verified behavior.
4. Update the transport section of `docs/api-endpoint.md` if Option A changed the surface.

Validation: the new transport test passes; no document claims a transport the code does not serve.

Commit: `fix(rpc): align the served transports with the documented contract`

## Task 02.3 — Close the three missing matrix rows and requests

**Finding F5 (P1).** `api/connect/*.proto` declares 120 procedures. `docs/api-endpoint.md` and
`llms/endpoint-reference.md` carry 117 rows, and the Yaak workspace carries 117 requests. Three
procedures are implemented and tested but undocumented and unrequested:

| Procedure | Implementation | Documented? |
| --- | --- | --- |
| `AccountService/UpdateAccount` | `modules/identity/account/handler_rpc.go:52` | no row in either document, no Yaak request |
| `AuthService/ForgotPassword` | `modules/identity/session/handler_rpc.go:110`, answers `unimplemented` | no row in either document |
| `AuthService/ResetPassword` | `modules/identity/session/handler_rpc.go:114`, answers `unimplemented` | no row in either document |

`AccountService/UpdateAccount` is a working procedure with a test
(`modules/identity/account/handler_rpc_test.go:121`) and no contract row. The two recovery
procedures are deliberate `unimplemented` stubs; they are declared in the proto, so they need a
row that says so, exactly as the plan matrix does
(`llms/connectrpc-plan/endpoint-reference.md:294-297`).

`llms/connectrpc-plan/endpoint-reference.md` also still records ambiguity A4 as open: whether
`AccountService.UpdateAccount` and `UserService.UpdateMe` should both exist. The refactor shipped
both, both call `UpdateProfile`, and the question was never answered. Answer it in this task.

### Required final shape

Every declared procedure has a row in `docs/api-endpoint.md` and `llms/endpoint-reference.md`, a
status, and a Yaak request. A4 is resolved in writing.

### Steps

1. Add the `AccountService/UpdateAccount` row to both documents with its test evidence.
2. Add the `AuthService/ForgotPassword` and `AuthService/ResetPassword` rows to both documents,
   status `done — declared for contract completeness; answers unimplemented; recovery is served by
   the retained REST routes`, with the existing recovery test evidence.
3. Create the three Yaak requests through the Yaak MCP integration under the folder that already
   holds their siblings: `AccountService/UpdateAccount` in `[Tango] Account` (`fl_UwXpNWCV47`),
   the two recovery procedures in `Session & Password` (`fl_vJemVfyfFL`). Name them
   `<METHOD> /rpc/<package>.<Service>/<Method>`. Send each one and record the observed status.
4. Resolve A4 in `llms/connectrpc-plan/endpoint-reference.md`: state which fields each procedure
   owns, or state that both are intentional and why.
5. Add a check that prevents a repeat: extend `internal/registry/rpc_inventory_test.go` with an
   assertion that the number of procedures in the generated service descriptors matches the
   documented matrix row count. If a static count is brittle, assert instead that every
   procedure constant in the generated `*v1connect` packages appears in the matrix.

Validation: the new inventory assertion passes; the three Yaak requests answer as recorded.

Commit: `docs(rpc): document the remaining connect procedures`

## Task 02.4 — Return a Connect error from the rate limiter

**Finding F6 (P1).** `internal/transport/middleware/ratelimit.go:220` calls
`responder.Fail(w, r, http.StatusTooManyRequests, ...)`. The `/rpc` mount runs the same limiter
(`internal/transport/http.go:74-78`), so a throttled RPC receives the REST envelope:

```
429 {"status":"error","message":"rate limit exceeded","metadata":{"status_code":429,...}}
```

The frozen mapping in `llms/connectrpc-plan/endpoint-reference.md:460-473` requires
`429 → resource_exhausted`. The TypeScript client still decodes a code (it falls back to
`codeFromHttpStatus`, and 429 maps to `unavailable`), so the caller sees the wrong code and a body
shape that belongs to the other transport. `rpcerr.ResourceExhausted` exists but has zero call
sites.

### Required final shape

A throttled `/rpc` request answers a Connect error document with code `resource_exhausted` and
HTTP 429, keeping `Retry-After`. A throttled REST request keeps the current envelope. One
implementation serves both.

### Steps

1. Branch on the request path in `writeRateLimited`. For `/rpc` prefixes, write the Connect error
   document through `connect.NewErrorWriter()` so the response format follows the negotiated
   protocol, and keep `Retry-After`. `ErrorWriter.IsSupported` plus `Write` is the supported path
   for `net/http` middleware (`connectrpc.com/connect@v1.21.0/error_writer.go:96-106`).
2. For non-`/rpc` paths, keep `responder.Fail`.
3. Add tests in `internal/transport/middleware/ratelimit_test.go`: a throttled `/rpc` request
   answers `resource_exhausted` with 429 and `Retry-After`; a throttled REST request keeps the
   envelope.
4. Confirm the mapping table stays accurate and note in the task evidence that
   `rpcerr.ResourceExhausted` is not used by the limiter because the limiter is middleware, not a
   handler. If the codebase prefers a single constructor, refactor `writeRateLimited` to build its
   error through `rpcerr.ResourceExhausted` and let the error writer render it.

Validation: both new limiter tests pass; `task test:go -- ./internal/transport/...` passes.

Commit: `fix(rpc): answer rate limits with a connect error`

## Task 02.5 — Allow the RPC headers in CORS

**Finding F7 (P1).** `internal/transport/middleware/cors.go:13-22` lists
`Connect-Protocol-Version`, `Connect-Request-Id`, `Connect-Timeout`, `Content-Encoding`,
`Content-Type`, `X-Connect-Protocol-Version`, `X-Grpc-Web`, and `X-User-Agent`. It omits
`Authorization`, `X-API-KEY`, and `Connect-Timeout-Ms`, and it lists `Connect-Timeout`, which is
not a Connect header. Probe of a preflight asking for `Authorization`:

```
OPTIONS /rpc/... + Access-Control-Request-Headers: Authorization
-> 200, Access-Control-Allow-Headers: "" (empty)
```

A cross-origin client therefore cannot send a bearer or a machine credential. Development and the
same-origin Vite proxy hide this; `compose.yaml` exposes the frontend on `:3443` and the Go
server on `:8443` as separate origins, which is the configuration that fails.

Task 06.4 covers the same defect in the nginx layer (`compose.yaml:209`), which answers the
preflight before this middleware sees it. Both layers must change; do them as one commit.

### Required final shape

The preflight allowlist covers every header a first-party RPC client sends: `Authorization`,
`X-API-KEY`, `Connect-Protocol-Version`, `Connect-Timeout-Ms`, `Connect-Accept-Encoding`,
`Connect-Content-Encoding`, `Content-Type`, `X-Grpc-Web`, `X-User-Agent`. `Connect-Timeout` and
`Connect-Request-Id` are removed unless something sends them.

### Steps

1. Verify the real header set: `@connectrpc/connect` sends `Connect-Protocol-Version`,
   `Connect-Timeout-Ms`, `Connect-Accept-Encoding`, and `Connect-Content-Encoding`; the
   gRPC-Web transport sends `X-Grpc-Web` and `X-User-Agent`. Record the source in the task
   evidence.
2. Update `AllowedHeaders` accordingly.
3. Decide the origin policy in the same task and record it. `AllowedOrigins: ["*"]` with
   `AllowCredentials: false` is consistent with the bearer model, because RPCs do not need cookie
   credentials. If a deployment serves the SPA from a different origin, that origin must be
   configurable rather than hardcoded.
4. Extend `internal/transport/middleware/middleware_test.go` `TestCORSPreflight` with a case that
   requests `Authorization` and `X-API-KEY` and asserts both appear in
   `Access-Control-Allow-Headers`.

Validation: the extended CORS test passes; `task test:go -- ./internal/transport/...` passes.

Commit: `fix(rpc): allow the rpc credential headers in cors`

## Decision (Task 02.2)

Not yet taken. Option B (keep gRPC and gRPC-Web mounted, correct the document) is the recommended
default.
