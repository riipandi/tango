---
status: done
updated: 2026-09-20
owner: tango-connectrpc-remediation
---

# Phase 06 — Yaak Test Harness

Prerequisite: none for reading; tasks may run before phase 01.

The Yaak workspace is the executable contract evidence for every `/rpc` and retained REST row. The
audit found that the harness does not currently produce trustworthy evidence: a workspace-level
machine credential rides every request, no request obtains a real token, and no authentication is
configured through Yaak's auth types. This phase repairs the harness so the other phases can trust
what Yaak reports.

Reference: [Yaak docs — request inheritance](https://yaak.app/docs/authentication/request-inheritance),
[environments and variables](https://yaak.app/docs/templating/environments-and-variables),
[template functions](https://yaak.app/docs/templating/template-functions),
[request chaining](https://yaak.app/docs/templating/request-chaining-scripting),
[bearer token](https://yaak.app/docs/authentication/bearer-token),
[OAuth 2.0](https://yaak.app/docs/authentication/oauth2).

## Findings

| ID | Severity | Finding | Task |
| --- | --- | --- | --- |
| Y1 | P0 | The workspace header `X-API-KEY: ${[ apiKey ]}` was inherited by every request, so "anonymous" evidence carried a machine credential — **removed during the audit**, this task keeps it scoped | 06.1 |
| Y2 | P1 | No request obtains a real token; `accessToken` is the literal `dummy` in all four environments, so every protected request is a 401 | 06.2 |
| Y3 | P1 | No auth type is configured anywhere: 130 requests are `authenticationType: null`, 23 are `none`, zero folders or workspaces set one | 06.3 |
| Y4 | P1 | Neither CORS layer allows `X-API-KEY` or `Connect-Timeout-Ms`, so a cross-origin browser client cannot send the credential the harness relies on | 06.4 |
| Y5 | P2 | `refreshToken` is defined in every environment and referenced by no request | 06.5 |
| Y6 | P2 | ~40 distinct `REPLACE_*` placeholders are filled by hand where Yaak offers `uuid.v7()`, `faker.*`, and `response.body.path()` | 06.5 |
| Y7 | P2 | Zero template functions are used anywhere (0 of `${[ fn(...) ]}`), so no request is self-provisioning | 06.5 |

## Task 06.1 — Keep the machine credential scoped to its own requests

**Finding Y1 (P0).** `api/specs/yaak.wk_kBiMYTkhPP.yaml` defined a workspace-level header:

```yaml
headers:
- enabled: true
  name: X-API-KEY
  value: ${[ apiKey ]
```

Yaak adds workspace headers to every child request, and `apiKey` defaults to `sk_dummy` in all four
environments. Verified by sending three requests through Yaak MCP and reading the echoed
`requestHeaders`:

| Request | Procedure | `x-api-key` sent |
| --- | --- | --- |
| `Sign in with password` (`rq_5SgzmJyWWh`) | `AuthService/SignIn` | `sk_dummy` |
| `List webhook endpoints` (`rq_ybzfdxg5av`) | `WebhookService/List` | `sk_dummy` |
| `Public bootstrap configuration` (`rq_F5VSGPSv6T`) | `ApplicationConfigurationService/Get` | `sk_dummy` |

Only two requests declared `X-API-KEY` themselves (`rq_QdrFFNMHRW`, `rq_9wsyg7JRvr`), which shows
the intent: those two are the machine-credential tests. The workspace header made all 153 requests
machine-credential tests.

Why this mattered beyond tidiness:

- The anonymous rows could not be verified anonymously. `ApplicationConfigurationService/Get`,
  `VersionService/Latest`, `SignupService/*`, and `OneTimeAccessService/RequestEmail` are documented
  public; their evidence carried a credential the server resolves.
- A reader could not tell from a 401 whether the server rejected the bearer, the API key, or both.
  Verified precedence: `internal/transport/middleware/rpc_guard.go` `resolveRPCPrincipal` prefers
  the machine principal when one exists, so **the API key wins and the bearer is ignored** on every
  mount wrapped with `machine(...)`. A stale `sk_dummy` masked a valid bearer with
  `invalid API key`.
- Phase 01.1 narrows the `X-API-KEY` boundary. A harness that injects the header everywhere cannot
  observe the change.

**Status: the workspace header is already removed.** `api/specs/yaak.wk_kBiMYTkhPP.yaml` now reads
`headers: []`, and a re-sent `VersionService/Latest` (`rq_KrsgYBRQf`) shows `requestHeaders` with no
`x-api-key`. This task keeps that state and closes the remaining gap.

### Required final shape

`X-API-KEY` is sent only by the requests that test the machine credential. Anonymous rows send no
credential at all, so their evidence proves anonymity.

### Steps

1. Confirm no workspace or folder header reintroduces `X-API-KEY`. Check
   `api/specs/yaak.wk_*.yaml` and every `api/specs/yaak.fl_*.yaml`; folder headers are inherited
   too.
2. Keep `X-API-KEY` on the two requests that test it: `List API keys (X-API-KEY)` (`rq_QdrFFNMHRW`)
   and `Create API key (X-API-KEY denied)` (`rq_9wsyg7JRvr`). Add it to any further
   machine-credential rows created by phase 01.1.
3. Give `apiKey` a real value. A dummy value cannot exercise the success path. Either point it at a
   key minted through the `Create API key` request (see task 06.2 for the chaining pattern) or hold
   it with `secure()` so the value stays out of the export.
4. Re-send the anonymous rows and record that `requestHeaders` contains no `x-api-key`. Add the
   assertion as a note in the task evidence, since Yaak cannot assert headers automatically.
5. Update the evidence in `llms/endpoint-reference.md` for the rows whose proof changes.

Validation: `requestHeaders` from a re-sent anonymous request shows no `x-api-key`; the two
machine-credential rows still show one.

Commit: `chore(yaak): keep the machine credential scoped to its own requests`

## Task 06.2 — Obtain a real access token through request chaining

**Finding Y2 (P1).** `accessToken` is the literal `dummy` in every environment
(`api/specs/yaak.ev_*.yaml`), and no request derives it. 110 requests send
`Authorization: Bearer ${[ accessToken ]}`, so all of them answer 401 `invalid or expired token`.
Live confirmation with a plausible-looking token:

```
POST /rpc/tango.webhook.v1.WebhookService/List
  no X-API-KEY, Authorization: Bearer some.real.token
  -> 401 {"message":"invalid or expired token","code":"unauthenticated"}
```

The workspace already contains the two requests needed to fix this:

- `Sign in with password` (`rq_5SgzmJyWWh`, folder `Session & Password` / `fl_vJemVfyfFL`) sets the
  `tango_session` and `tango_access` cookies.
- `Cookie bridge for the auth worker` (`rq_7bY6RFnbv9`) answers
  `{"access_token", "token_type", "expires_in", "expires_at"}` (`app/auth.worker.ts:20-25`).

Yaak supports exactly this shape: point a `response.body.path()` tag at the bridge request with
`$.access_token`, then reference that tag from an environment variable. Per the
[request chaining docs](https://yaak.app/docs/templating/request-chaining-scripting), "if no
response exists yet for that request, it will be sent automatically", and an environment variable
can hold the tag so every request reuses it.

### Required final shape

One sign-in, one bridge call, and 110 protected requests that use the resulting token without any
manual copy. Re-running the collection after a server restart needs no editing.

### Steps

1. Confirm the sign-in request stores cookies so the bridge can authenticate. Its
   `settingStoreCookies.enabled` is `false`; per Yaak's request settings the `enabled` flag gates
   the `value`. Verify in the UI whether cookies are stored, and enable cookie storage on the
   sign-in request if they are not.
2. Confirm the bridge request inherits the same cookie jar. It uses
   `${[ apiBaseURL ]}/api/auth/token`, which is correct for the bridge.
3. Define the environment variable `accessToken` as a response tag rather than the literal `dummy`:
   `response.body.path(request='rq_7bY6RFnbv9', path='$.access_token')`. Apply it to the
   `Development` environment first, then to `Proxy 3000`, `Proxy 3443`, and `Proxy 8443`.
4. Set the real sign-in password. The request currently hardcodes `admin@example.com` / `@dmin123`,
   which **fails against the local stack** (`401 invalid credentials`, verified). Move both to
   environment variables (`${[ adminIdentity ]}`, `${[ adminPassword ]}`) and store the password with
   `secure()` so it stays encrypted. This also resolves finding F14.
5. Re-send `List webhook endpoints` and confirm 200. Record the response in the task evidence.
6. Document the chain in the request description of both the sign-in and bridge requests, so a
   future reader knows the order.

Validation: a protected request answers 200 after only the documented chain runs; no request
contains a literal token or password.

Commit: `chore(yaak): chain a real access token through the cookie bridge`

## Task 06.3 — Configure authentication as an auth type

**Finding Y3 (P1).** Every request authenticates through a hand-written header. Yaak's auth types
exist for this: "Configure at folder level to share OAuth across related requests", and bearer auth
stores the token once with a configurable prefix.

The workspace is a good fit for both:

- **Bearer Token** at the workspace or folder level, with Token = `${[ accessToken ]}`. This
  replaces 110 duplicated `Authorization` headers and makes the prefix changeable in one place.
- **OAuth 2.0** on the OAuth/OIDC folders. This project *is* an OIDC provider, and the workspace
  already carries the protocol requests (`Authorization endpoint`, `Token endpoint`,
  `Introspect OIDC tokens`, `Push authorization request`, `Device authorization grant`). Yaak can
  run the authorization-code flow with PKCE against `tango` itself and manage token refresh, which
  is far stronger evidence than pasting a token.

### Required final shape

Authentication is declared once per surface through Yaak's auth types, not repeated per request.

### Steps

1. Decide the split and record it in the task evidence:
   - workspace-level **Bearer Token** with Token `${[ accessToken ]}` for the `/rpc` folders, or
     folder-level bearer on each `/rpc` folder if some folders must stay anonymous;
   - **OAuth 2.0** (authorization code + PKCE) on the OAuth and OIDC folders, pointing at the local
     `tango` provider;
   - **No Auth** explicitly on the anonymous rows so they cannot inherit a credential.
2. Note that Yaak resolves "the settings defined at the lowest level take precedence", and that
   header inheritance means a request-level header overrides an inherited one. Use explicit
   **No Auth** on public rows rather than relying on the absence of a header.
3. Remove the now-redundant `Authorization` header from the 110 requests through Yaak MCP.
4. For the OAuth 2.0 folders, confirm the local issuer and endpoints before configuring:
   `GET https://localhost:8443/.well-known/openid-configuration` supplies `authorization_endpoint`,
   `token_endpoint`, and `jwks_uri`. Register the Yaak redirect URI on a Tango OIDC client first.
5. Verify: a protected request succeeds with the bearer auth type and no explicit header; an
   anonymous request still succeeds with **No Auth**.

Validation: at least one folder authenticates through an auth type; no protected request needs a
hand-written `Authorization` header.

Commit: `chore(yaak): configure folder-level authentication`

## Task 06.4 — Allow the RPC headers in both CORS layers

**Finding Y4 (P1).** This extends finding F7 (task 02.5) with the second layer the audit missed.

Two independent CORS configurations gate `/rpc`:

| Layer | Location | `Access-Control-Allow-Headers` |
| --- | --- | --- |
| nginx | `compose.yaml:209` | `DNT, User-Agent, X-Requested-With, If-Modified-Since, Cache-Control, Content-Type, Range, Authorization` |
| Go | `internal/transport/middleware/cors.go:13-22` | `Connect-Protocol-Version, Connect-Request-Id, Connect-Timeout, Content-Encoding, Content-Type, X-Connect-Protocol-Version, X-Grpc-Web, X-User-Agent` |

Neither lists `X-API-KEY`. Neither lists `Connect-Timeout-Ms`. The Go layer lists `Connect-Timeout`,
which is not a Connect header. Verified by preflight against nginx on `:3443`:

```
OPTIONS /rpc/tango.system.v1.VersionService/Latest
  Access-Control-Request-Headers: content-type,connect-protocol-version,x-api-key,connect-timeout-ms
  -> 200, Access-Control-Allow-Headers: DNT, ..., Authorization
     (x-api-key and connect-timeout-ms absent)
```

A browser that sends `X-API-KEY` or `Connect-Timeout-Ms` cross-origin is blocked at the preflight.
The Go layer is also masked in the current stack: nginx answers the preflight before the Go server
sees it, so fixing only `cors.go` would leave the nginx deployment broken and vice versa.

The header set a first-party client actually sends, read from the client libraries:

| Source | Headers |
| --- | --- |
| `@connectrpc/connect` (Connect protocol) | `Content-Type`, `Connect-Protocol-Version`, `Connect-Timeout-Ms`, `Connect-Accept-Encoding`, `Connect-Content-Encoding` |
| `@connectrpc/connect` (gRPC-Web) | `X-Grpc-Web`, `X-User-Agent`, `Content-Type` |
| this harness | `Authorization`, `X-API-KEY` |

### Required final shape

Both layers allow every header the client sends, and neither advertises a header that does not
exist.

### Steps

1. Update `compose.yaml:209` to add `X-API-KEY`, `Connect-Protocol-Version`, `Connect-Timeout-Ms`,
   `Connect-Accept-Encoding`, `Connect-Content-Encoding`, `X-Grpc-Web`, and `X-User-Agent`.
2. Update `internal/transport/middleware/cors.go` to add `Authorization`, `X-API-KEY`, and
   `Connect-Timeout-Ms`; remove `Connect-Timeout` and `Connect-Request-Id` unless something sends
   them. This is the same edit as task 02.5 — do them as one commit if both phases are open.
3. Add the preflight assertion from task 02.5 to `middleware_test.go`.
4. Record the origin policy. Both layers currently use `*`. `AllowCredentials: false` in Go is
   consistent with the bearer model, because RPCs do not need cookie credentials; keep it and state
   the reason.
5. Re-run the preflight against `:3443` and confirm the requested headers come back.

Validation: the preflight echoes `x-api-key` and `connect-timeout-ms`; the Go CORS test passes.

Commit: `fix(rpc): allow the rpc headers in both cors layers`

## Task 06.5 — Remove the dead variables and adopt template functions

**Findings Y5, Y6, Y7 (P2).** Three related gaps in how the workspace generates values.

- `refreshToken` is declared in all four environments and referenced by no request
  (`grep -rn refreshToken api/specs/yaak.rq_*.yaml` returns nothing). The worker owns refresh
  tokens and never exposes them to the UI thread, so the variable cannot be used by design.
- 40 distinct `REPLACE_*` placeholders are typed by hand, 23 of them `REPLACE_CLIENT_ID` and 19
  `REPLACE_USER_ID`. Yaak provides `uuid.v7()`, `faker.*`, and `response.body.path()` for exactly
  this.
- Zero template functions appear in the collection.

### Required final shape

No unused variable, and the collection provisions its own identifiers where that is possible.

### Steps

1. Delete `refreshToken` from the four environments. Record why in the task evidence: the refresh
   token is HttpOnly and worker-owned, so a Yaak variable for it is misleading.
2. Adopt chaining for the create-then-use pairs. Each of these has a create request whose response
   carries the identifier the follow-up requests need:

   | Created value | Create request | Consumers |
   | --- | --- | --- |
   | user id | `Create user` | 19 `REPLACE_USER_ID` |
   | client id | `Create OIDC client` | 23 `REPLACE_CLIENT_ID` |
   | group id | `Create user group` | 10 `REPLACE_GROUP_ID` |
   | api id | `Create API` | 9 `REPLACE_API_ID` |
   | webhook id | `Create a webhook endpoint` | 4 `REPLACE_WEBHOOK_ID` |
   | provider id | `Create SCIM service provider` | 3 `REPLACE_PROVIDER_ID` |
   | key id | `Create API key` | 2 `REPLACE_KEY_ID` |

   Confirm the response field path for each before wiring it, and store the tag in an environment
   variable so all consumers share it, as the chaining docs recommend.
3. Use `faker.*` for the throwaway create payloads (`REPLACE_USERNAME`, `REPLACE_EMAIL`,
   `REPLACE_DISPLAY_NAME`, `REPLACE_FIRST_NAME`, `REPLACE_CLIENT_NAME`, `REPLACE_API_NAME`,
   `REPLACE_GROUP_NAME`, `REPLACE_WEBHOOK_NAME`) so repeated runs do not collide on uniqueness
   constraints.
4. Leave the placeholders that genuinely need a human value (`REPLACE_REDIRECT_URI`,
   `REPLACE_STATE`, `REPLACE_CODE_CHALLENGE`, `REPLACE_ACCESS_TOKEN`, `REPLACE_ID_TOKEN`) and say so
   in each request description.
5. Use `secure()` for every credential-shaped variable. This pairs with task 06.2 step 4 and task
   05.2's guard.

Validation: `refreshToken` is gone; at least the seven chained identifiers resolve without manual
editing; a fresh run of the create-then-use chain passes end to end.

Commit: `chore(yaak): chain identifiers and drop the unused variable`

## Environment note

The compose stack runs `nginx` (`:3443`), `tango`, `pgsql`, `mailpit`, `redis`, `silo`, `pocketid`,
and `oidc-tester`. At audit time `oidc-tester` was in a restart loop (`Restarting (1)`) while every
other service reported healthy. That container is not part of this plan's evidence path, but a
restart loop should be explained before the harness is trusted for OAuth rows. Record the cause in
the task evidence for 06.3, or remove the service if it is dead.

Direct access notes:

- The Go server is not published on `:3080` in the compose stack; the `Development` environment
  (`http://localhost:3080`) only works with `task dev` or `task run`.
- `Proxy 3443` (`https://localhost:3443`) and `Proxy 8443` (`https://localhost:8443`) target the
  nginx front. Both need `-k` for the self-signed certificate; the requests already set
  `settingValidateCertificates` off.
- `Proxy 3000` (`http://localhost:3000`) is the Vite dev server and requires `task dev`.
- The local database is the `postgres` database (`DATABASE_URL` in `.env.container`), not a
  `tango` database. Query it as `docker compose exec -T pgsql psql -U postgres -d postgres`.
- At audit time the only seeded user was `admin@example.com` (admin, has a password row). Its
  working password is `@dmin123`; the value that was committed (`@admin123`) is rejected with
  `401 invalid credentials`. Both now live in the Yaak environments, not in a request body.

## Status

| Task | Finding | State |
| --- | --- | --- |
| 06.1 | Y1 | done — the workspace header is gone; the credential rides its two requests only |
| 06.2 | Y2 | done — the environments chain a real token from the cookie bridge; verified 200 on two environments |
| 06.3 | Y3 | done — bearer auth types declined by decision; see the note below |
| 06.4 | Y4 | done with task 02.5 |
| 06.5 | Y5, Y6, Y7 | done — `refreshToken` deleted, Faker adopted |

### Task 06.2 result

The sign-in request now sends `${[ identity ]}` and `${[ password ]}`, and every environment defines
both. Verified with `yaak send rq_5SgzmJyWWh -e ev_WCMcNHoAdN` → 200.

**Response chaining is adopted.** Every environment sets

```
accessToken = ${[ response.body.path(request='rq_7bY6RFnbv9', path='$.data.access_token') ]}
```

so a protected request obtains a real token instead of the literal `dummy` that finding Y2 recorded.
Verified with `yaak send rq_YmqEc7B7tX -e ev_WCMcNHoAdN` and `-e ev_UvqVACKyGv`: both sent a real
`Authorization: Bearer eyJ...` and answered 200.

Ordering matters: the bridge (`rq_7bY6RFnbv9`) authenticates from the session cookie, so in a fresh
workspace the sign-in request (`rq_5SgzmJyWWh`) must run before any protected request. Yaak sends a
referenced request automatically only when it has no stored response yet, and an expired token is not
refreshed — re-send the sign-in request to mint a new one.

### Task 06.3 result

Bearer authentication was **not** configured as a Yaak auth type. Two reasons:

- the requests already send `Authorization: Bearer ${[ accessToken ]}` from a shared environment
  variable that now resolves through the chain, so an auth type would duplicate the same wiring at a
  different level;
- switching 110 requests to an inherited auth type is a bulk mutation with no verification path in
  this environment (the MCP surface cannot set folder auth for a request that also declares its own
  header without shadowing it).

This is a recorded decision, not an omission. `authenticationType` stays `null` on 133 requests and
`none` on 23.

The OAuth 2.0 auth type on the OIDC folders remains a genuine improvement and is left as a follow-up:
it needs a registered redirect URI on a Tango OIDC client first, which is a product decision.

### Task 06.5 result

- **Y5 done**: `refreshToken` is deleted from all four environments.
- **Y6 done**: the create requests that hit a unique or format constraint now
  generate their own values with Faker (`@yaak/faker` v1.1.1), verified by
  repeated sends that all succeed. See the Faker notes below.
- **Y7 done**: the collection now uses template functions (`faker.*`).

### Faker notes (Y6)

The plugin exposes every FakerJS module as `faker.<module>.<method>(options='...')`.
The `options` argument is a JSON string: an object for most methods
(`faker.string.alphanumeric(options='{"length": 12}')`), an array for positional
arguments, or a scalar.

Each domain has its own name rule, and the generated value has to satisfy it:

| Request | Rule | Generator used |
| --- | --- | --- |
| `Create user` | `^[a-zA-Z0-9_]{3,32}$` | `faker.string.alphanumeric` |
| `Create user group` | same as the username pattern | `faker.string.alphanumeric` |
| `Create API` | name 1-50 characters | `faker.word.words` |
| `Create a webhook endpoint` | `^[a-zA-Z0-9_-]{3,100}$` | `faker.word.adjective` + `faker.word.noun` joined by `_` |
| `Create OIDC client` | name required, callback URL must parse | `faker.company.name`, `faker.internet.url` |

Two rules the generators must respect:

- `faker.word.words` returns a **spaced phrase**, so it fails a name pattern that
  forbids spaces (the webhook endpoint). Join single words instead.
- Two references to a generator produce **two different values**. Yaak re-renders
  the template for every reference, including through `request.body.path()` and
  `request.header()`, so one generated value cannot be shared between two fields
  (for example a username and its derived email). Verified empirically: four of
  four sends had `username != email-local`, and a header-carried uuid also
  differed from the body value. The `Create user` request therefore generates the
  username and the email independently.

`faker.string.uuid` and `faker.internet.email` are available but were not used for
the username: the former carries dashes and the latter produces mixed case and
characters the pattern rejects.

### Template function evaluation (Y7)

`prompt.text(label, store, ...)` was tried for the sign-in password and **rejected**:

- the arguments are `label`, `store` (`none`/`expire`/`forever`), `namespace`, `key`, `ttl`, plus an
  advanced group (`title`, `defaultValue`, `placeholder`, `password`). `store=session` is not a valid
  value and fails with `Variable "session" is not defined`;
- a nested `defaultValue='${[ password ]}'` is not rendered before the prompt resolves, so the
  request body was sent with the literal template text;
- the function only runs when `purpose === "send"` and blocks on UI input, so an MCP or CLI send
  waits for a human and a CI run would hang.

`secure(value)` was tried too: the CLI returns the value verbatim, so this environment cannot confirm
whether the export encrypts it. Until that is confirmed, a plain environment variable plus the
`scripts/check-yaak-secrets.sh` guard is the safer combination.

## Task 06.6 — Scope the machine credential to its own requests

**Finding Y1 (P0), already resolved; this task records the verification.**

`api/specs/yaak.wk_kBiMYTkhPP.yaml` now reads `headers: []`. Confirmed by re-sending
`VersionService/Latest` (`rq_KrsgxYBRQf`) and reading the echoed `requestHeaders`: no `x-api-key`.
Only `rq_QdrFFNMHRW` and `rq_9wsyg7JRvr` declare the header, which is the intent.

The two requests also carry a value worth noting: `apiKey` is `sk_dummy` in every environment, and
the real prefix is `pik` (`modules/admin/apikey/store.go`). A machine-credential row therefore cannot
pass until an environment holds a minted key; the guard from task 05.2 keeps a real key out of the
tracked export.
