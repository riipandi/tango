# Tango API Client

TypeScript SDK for the tango REST API, built on `ofetch`. Supabase-style namespaces:

```ts
import { apiClient, ApiClientError } from '~/apiclient/index'

const { user, session_id } = await apiClient.auth.signInWithPassword({
  identity: 'abbey',
  secret: 's3cret'
})

const users = await apiClient.users.list({ query: 'abb', page: 1, limit: 20 })
users.pagination?.totalItems

// Machine-to-machine: X-API-KEY rides every request.
const script = createApiClient({ baseUrl: 'https://tango.local', apiKey: 'k-123' })
await script.system.versionLatest()

try {
  await apiClient.account.changePassword({ current_password: 'old', new_password: 'n3wS3cret' })
} catch (error) {
  if (error instanceof ApiClientError) console.log(error.status, error.fieldErrors)
}
```

- Transport: one `ofetch` instance per client, `credentials: 'include'` by default so the
  `tango_session` cookie flows, no transport-level retries (TanStack Query owns retry policy).
  Machine auth: `apiKey` option (or `setApiKey`) sends `X-API-KEY` on every request.
- Envelope: `{status, message?, data, error?, metadata, links}` unwraps into `{data, metadata,
  links?}`; bare documents (WebAuthn begin payloads, JWKS, discovery) pass through untouched.
- Errors: every failure throws `ApiClientError` (`status`, `fieldErrors`, `rateLimit`,
  `requestId`, `code: 'api_error' | 'network_error' | 'aborted' | 'unknown'`).
- Lists: paginated endpoints return `Paginated<T>` with `pagination` folded from envelope
  metadata; absent when the server skipped paging.
- Escape hatch: `apiClient.raw(method, path, init)` — the path is relative to the `/api`
  prefix (`'/users/{id}'` calls `/api/users/{id}`).

Build a custom client with `createApiClient(options)`; import everything from the package
entry point. Types mirror the Go DTOs and are inferred from the zod schemas in `schemas/`.

## Scope: REST only

This SDK wraps the retained HTTP surface only — OAuth/OIDC and SCIM protocol endpoints,
WebAuthn, binary/health/discovery documents, and the `/api` routes not yet migrated. It does
not wrap ConnectRPC services: first-party application calls use the generated Connect clients
(`app/generated/rpc/`, produced from `api/connect/*.proto` by `task rpc:generate`) through the
same-origin `/rpc` transport with bearer access tokens supplied by the auth worker
(`app/auth.worker.ts`). See `llms/connectrpc-plan/04-client.md` for the split.

Layout: `client.ts` composes namespaces, `http.ts` is the transport executor, `envelope.ts`
detects and projects the envelope, `error.ts` normalizes failures, `modules/` holds one file
per namespace, `tests/` runs under the `api-client` vitest project.
