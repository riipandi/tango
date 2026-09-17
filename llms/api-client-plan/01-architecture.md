---
status: pending
updated: 2026-09-17
---

# 01 — Architecture

## File layout

```
api/client/
  index.ts          # public entry: createApiClient, types re-exports, default singleton
  client.ts         # createApiClient(options): namespaced SDK over one ofetch instance
  error.ts          # ApiClientError + normalization from ofetch FetchError
  types.ts          # envelope, metadata, pagination, request options
  modules/
    auth.mod.ts     # auth (+ auth.mfa, auth.webauthn)
    account.mod.ts  # self-service profile, password, sessions
    user.mod.ts     # admin user CRUD + membership + profile picture
    usergroup.mod.ts
    appconfig.mod.ts
  schemas/          # zod schemas (runtime-validated in tests only) + inferred types
  tests/            # vitest, node environment
```

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
| `code: 'api_error' \| 'network_error' \| 'unknown'` | classification |

Normalizer duck-types ofetch `FetchError` (has `response` / `data`); anything else with no
response becomes `network_error`. No retry logic, no token refresh — sessions are cookie-backed.

## Escape hatch

`apiClient.raw` executes an arbitrary method/path and returns the full
`{ data, metadata }` (or bare data) result — used by future namespaces before they get typed
methods, and by SDK tests.
