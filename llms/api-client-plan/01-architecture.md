---
status: done
updated: 2026-09-17
---

# 01 — Architecture

## File layout

```plaintext
api/client/
  index.ts          # public entry: createApiClient, types re-exports, default singleton
  client.ts         # createApiClient(options): composes namespaces over the executor
  http.ts           # transport: ofetch instance, /api prefix, envelope unwrap, executor
  envelope.ts       # envelope detection + wire→camelCase metadata projection
  error.ts          # ApiClientError + normalization (api_error/network_error/aborted/unknown)
  pagination.ts     # toPaginated: list results from envelope metadata
  types.ts          # types only: envelope, metadata, pagination, executor contract
  README.md         # usage quickstart
  modules/
    auth.mod.ts       # auth; composes mfa + webauthn; one-time access
    mfa.mod.ts        # auth.mfa.totp lifecycle
    webauthn.mod.ts   # auth.webauthn ceremonies (bare begin payloads)
    account.mod.ts    # self-service profile, password, sessions, email verification
    users.mod.ts      # admin user CRUD + membership + credentials + one-time access
    usergroups.mod.ts
    appconfig.mod.ts
    oidcclients.mod.ts # relying-party client registry
    consent.mod.ts    # authorized clients (me + admin)
    scim.mod.ts
    apis.mod.ts       # API resources, permissions, grants, CIMD
    apiaccess.mod.ts  # client-centric grant views
    apikeys.mod.ts    # X-API-KEY machine credentials
    customclaims.mod.ts
    auditlogs.mod.ts
    webhooks.mod.ts
    devicelogin.mod.ts
    system.mod.ts     # versions + readiness
  schemas/          # zod schemas (runtime-validated in tests only) + inferred types
  tests/            # vitest, node environment
```

Layering is acyclic: `types` ← `envelope` ← `error` ← `http` ← modules ← `client` ← `index`.
Module paths are resource paths (`/users/{id}`); the executor owns the `/api` prefix and the
absolute-URL helper (`exec.url`). Placeholder schema files for unbuilt namespaces were removed —
schemas land together with their namespace.

## Client core

`createApiClient(options?)` builds one `ofetch.create` instance and returns the namespaced SDK.

Options:

| Option | Default | Notes |
| --- | --- | --- |
| `baseUrl` | `''` (same-origin) | joined as `baseURL`; dev SPA hits `http://localhost:3080` |
| `headers` | `{}` | merged into every request (e.g. `X-API-KEY` for admin scripts) |
| `credentials` | `'include'` | the `tango_session` cookie must flow; SameSite=Lax |
| `timeout` | `undefined` | forwarded to ofetch |
| `fetch` | global | injectable for tests |

`retry: 0` — failures surface immediately; TanStack Query owns retry policy at the app layer.

### Request execution

All calls go through an internal executor using `ofetch.raw` so status and headers are available:

1. Send request (`method`, `path`, `body`, `query`, `signal`, merged `headers`).
2. Non-2xx → ofetch throws `FetchError` → normalize to `ApiClientError` and throw.
3. Parse body:
   - Envelope-shaped (`status` is `'success' | 'error'` and `metadata` is an object) → return
     `{ data, metadata }`.
   - Otherwise (bare document: WebAuthn begin `{publicKey, session_id}`, JWKS, discovery) →
     return `{ data: body, metadata: { statusCode, requestId from `X-Request-Id` } }`.
4. A 2xx envelope with `status: 'error'` throws `ApiClientError` built from the envelope itself.

### Return convention

- Singular operations return `data` directly: `await apiClient.users.get(id)` → `User`.
- Operations whose only answer is success return `void` (204/empty data).
- Paginated lists return `Paginated<T>`: `{ data: T[]; pagination?: { page, limit, totalPages,
  totalItems, firstItemIndex?, lastItemIndex? } }` — built from envelope metadata; `pagination`
  is absent when the server reports the "all" page (-1).
- Per-call overrides (`signal`, `headers`, `query` merge) pass via an optional last argument.

## Error model

`ApiClientError extends Error` (from `error.ts`):

| Field | Source |
| --- | --- |
| `status?: number` | HTTP status; `undefined` for network failures |
| `message` | envelope `message`, else status text, else `'network error'` |
| `requestId` / `traceId` | envelope metadata |
| `fieldErrors: FieldError[]` | envelope `error` when it is the 422 `{field, message}[]` form |
| `rateLimit?` | envelope metadata rate limit (`limit`, `remaining`, `reset`) |
| `envelope?` | the raw envelope for unreduced consumers |
| `code: 'api_error' \| 'network_error' \| 'aborted' \| 'unknown'` | classification; aborts are detected on the ofetch cause chain (timeouts and aborted signals) |

Normalizer duck-types ofetch `FetchError` (has `response` / `data`); anything else with no
response becomes `network_error`. No retry logic, no token refresh — sessions are cookie-backed.

## Escape hatch

`apiClient.raw` executes an arbitrary method/path and returns the full
`{ data, metadata }` (or bare data) result — used by future namespaces before they get typed
methods, and by SDK tests.
