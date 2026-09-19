# Tango API Client

TypeScript SDK for the retained REST surface of the tango API, built on `ofetch`. Supabase-style namespaces:

```ts
import { apiClient, ApiClientError } from '~/apiclient/index'

// Sign-in and session reads are ConnectRPC — the SPA gets them through
// the generated clients; the SDK covers the retained HTTP surface only.
await apiClient.auth.signOut()
await apiClient.auth.forgotPassword({ identity: 'abbey' })
await apiClient.auth.exchangeOneTimeToken('otat_...')

// Machine-to-machine: X-API-KEY rides every request.
const script = createApiClient({ baseUrl: 'https://tango.local', apiKey: 'k-123' })
await script.system.health()

try {
  await apiClient.raw('POST', '/users/me/verify-email')
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
- Escape hatch: `apiClient.raw(method, path, init)` — the path is relative to the `/api`
  prefix (`'/users/{id}'` calls `/api/users/{id}`).

Build a custom client with `createApiClient(options)`; import everything from the package
entry point. Types mirror the Go DTOs and are inferred from the zod schemas in `schemas/`.

## Scope: retained REST only

This SDK wraps the retained HTTP surface only — OAuth/OIDC and SCIM protocol endpoints,
WebAuthn ceremonies, device-login request/exchange, email-link exchanges
(one-time access token, verify-email), the auth-worker cookie bridge and sign-out
fallback, and health/discovery documents. It does not wrap ConnectRPC services:
first-party application calls use the generated Connect clients
(`app/generated/rpc/`, produced from `api/connect/*.proto` by `task rpc:generate`) through
the same-origin `/rpc` transport with bearer access tokens supplied by the auth worker
(`app/auth.worker.ts`). See `llms/connectrpc-plan/04-client.md` for the split.

Layout: `client.ts` composes namespaces, `http.ts` is the transport executor, `envelope.ts`
detects and projects the envelope, `error.ts` normalizes failures, `modules/` holds one file
per namespace, `tests/` runs under the `api-client` vitest project.
