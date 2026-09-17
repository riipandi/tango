---
status: pending
updated: 2026-09-17
---

# 04 — Tests and Gates

## Test harness

- Location: `api/client/tests/*.test.ts` (matches the existing `api-client` vitest project
  include `./api/client/**/*.test.ts` — no vitest config change needed).
- Environment: `node`. HTTP is faked by stubbing `globalThis.fetch` to return `Response` objects;
  the client under test receives it through the `fetch` client option. Helpers:
  - `mockFetch()` — captures `Request` (url, method, headers, body) and queues responses.
  - `envelope(data, metadata?)` / `errorEnvelope(status, message, error?)` — envelope builders.
- Delete the old placeholder `api/client/schemas/schemas.test.ts` (moved into `tests/`).

## Test files

| File | Covers |
| --- | --- |
| `tests/client.test.ts` | Envelope unwrap (data + metadata), bare-document passthrough (WebAuthn begin shape), baseUrl join, default `credentials: 'include'`, custom headers merge, per-call `signal`, `raw()` escape hatch |
| `tests/error.test.ts` | 422 → `ApiClientError` with `fieldErrors`; 429 → `rateLimit`; 401 message passthrough; non-envelope error body; network failure → `code: 'network_error'`; 2xx envelope with `status: 'error'` throws |
| `tests/auth.test.ts` | `signInWithPassword` (POST `/api/auth/sign-in`, body `{identity, secret}`, result type), `getSession`, `signOut`, `forgotPassword`, `resetPassword`, `signUp`, `setupAccount`, `setupAvailable` (204 → true, 404 → false), mfa totp six methods, webauthn begin/finish incl. `session_id` query |
| `tests/account.test.ts` | profile get/update, change password, list/revoke sessions |
| `tests/users.test.ts` | list with `query/page/limit` query encoding, `Paginated` build, get/create/update/remove, groups get/set, multipart profile picture upload (`FormData` field `file`), `profilePictureUrl` |
| `tests/usergroups.test.ts` | CRUD + members + allowed clients mapping |
| `tests/appconfig.test.ts` | public/all variables, `update` map body, `sendTestEmail` |
| `tests/schemas.test.ts` | zod accept/reject for each schema (valid payload passes; missing required / wrong type fails) |

Every request-mapping assertion checks: method, full URL (`/api/...`), query string, and JSON
body. Response-shape assertions check what the SDK returns, not server internals.

## Gates

1. `pnpm vitest run --project api-client` — all green.
2. `pnpm typecheck` (`tsc -b --noEmit`) — clean, including strict flags
   (`noUncheckedIndexedAccess`, `verbatimModuleSyntax`).
3. `task lint` — oxlint clean on new files; `pnpm format:js` (oxfmt) if the hook demands.
4. Coverage: `api/client/**` is inside the vitest coverage `include` with global thresholds
   (statements 80, branches 70, functions 75, lines 80) — run vitest with coverage once before
   closing to confirm the SDK core + modules stay above thresholds.

## Acceptance

- `apiClient.auth.signInWithPassword({ identity, secret })` returns the typed sign-in result and
  works against the running server (manual curl-level check or Yaak parity later; SPA wiring is a
  follow-up).
- No references to React/TanStack inside `api/client/`.
- Old placeholder test removed; new tests live only in `api/client/tests/`.
