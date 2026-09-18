---
status: done
updated: 2026-09-17
---

# 02 — Modules (method → endpoint map)

All paths are relative to `/api`. Shapes come from the Go handlers; timestamps are ISO strings.

## auth (`auth.mod.ts`)

| Method | Request | Endpoint | Returns |
| --- | --- | --- | --- |
| `auth.signInWithPassword(params)` | `{ identity, secret }` | `POST /auth/sign-in` | `SignInResult` `{ user, session_id, provider, expires_at? }` |
| `auth.getSession()` | — | `GET /auth/session` | `SignInResult` (no `expires_at`) |
| `auth.signOut()` | — | `POST /auth/sign-out` | `void` |
| `auth.forgotPassword(params)` | `{ identity }` | `POST /auth/forgot-password` | `void` |
| `auth.resetPassword(params)` | `{ token, new_password }` | `POST /auth/reset-password` | `void` |
| `auth.signUp(params)` | `{ username, email, first_name?, last_name?, token? }` | `POST /signup` | `User` (201) |
| `auth.setupAvailable()` | — | `GET /signup/setup` | `boolean` (204 → true, 404 → false, swallow only 404) |
| `auth.setupAccount(params)` | same as sign-up | `POST /signup/setup` | `User` (201, 409 once users exist) |

`auth.mfa.totp`:

| Method | Request | Endpoint | Returns |
| --- | --- | --- | --- |
| `verify(params)` | `{ code }` | `POST /mfa/totp/verify` | `SignInResult` (rotates session cookie) |
| `enroll()` | — | `POST /mfa/totp/enroll` | `{ secret, provisioning_uri }` (201) |
| `confirm(params)` | `{ code }` | `POST /mfa/totp/confirm` | `{ recovery_codes: string[] }` |
| `status()` | — | `GET /mfa/totp/status` | `{ confirmed, recovery_codes_remaining }` |
| `rotateRecoveryCodes()` | — | `POST /mfa/totp/recovery-codes` | `{ recovery_codes }` |
| `disable()` | — | `DELETE /mfa/totp` | `void` |

`auth.webauthn` (begin payloads are bare documents — no envelope):

| Method | Request | Endpoint | Returns |
| --- | --- | --- | --- |
| `beginLogin()` | — | `POST /webauthn/login/begin` | `{ publicKey, session_id }` |
| `finishLogin(assertion, sessionId)` | assertion body | `POST /webauthn/login/finish?session_id=` | `User` |
| `beginRegistration()` | — | `POST /webauthn/register/begin` | `{ publicKey, session_id }` |
| `finishRegistration(attestation, sessionId)` | attestation body | `POST /webauthn/register/finish?session_id=` | credential view (201) |

## account (`account.mod.ts`)

| Method | Request | Endpoint | Returns |
| --- | --- | --- | --- |
| `account.getProfile()` | — | `GET /account` | `User` |
| `account.updateProfile(patch)` | `{ first_name?, last_name?, display_name?, avatar_url?, locale? }` | `PATCH /account` | `User` |
| `account.changePassword(params)` | `{ current_password, new_password }` | `PUT /account/password` | `void` |
| `account.listSessions()` | — | `GET /account/sessions` | `SessionView[]` |
| `account.revokeSession(id)` | — | `DELETE /account/sessions/{id}` | `void` |

## users (`user.mod.ts`)

| Method | Request | Endpoint | Returns |
| --- | --- | --- | --- |
| `users.list(params?)` | `?query&page&limit` | `GET /users` | `Paginated<User>` |
| `users.get(id)` | — | `GET /users/{id}` | `User` |
| `users.create(params)` | `{ username, email, first_name?, last_name?, display_name?, is_admin }` | `POST /users` | `User` (201) |
| `users.update(id, patch)` | partial `{ email?, first_name?, last_name?, display_name?, is_admin?, disabled? }` | `PUT /users/{id}` | `User` |
| `users.remove(id)` | — | `DELETE /users/{id}` | `void` |
| `users.listGroups(id)` | — | `GET /users/{id}/groups` | `UserGroup[]` |
| `users.setGroups(id, groupIds)` | `string[]` | `PUT /users/{id}/user-groups` | `void` |
| `users.updateProfilePicture(id, file)` | multipart field `file` | `PUT /users/{id}/profile-picture` | `void` (204) |
| `users.removeProfilePicture(id)` | — | `DELETE /users/{id}/profile-picture` | `void` (204) |
| `users.profilePictureUrl(id)` | — | — | URL string (no request) |

`updateProfilePicture` accepts `File | Blob` and builds `FormData` with field `file`.

## userGroups (`usergroup.mod.ts`)

| Method | Request | Endpoint | Returns |
| --- | --- | --- | --- |
| `userGroups.list(params?)` | `?query&page&limit` | `GET /user-groups` | `Paginated<UserGroup>` |
| `userGroups.get(id)` | — | `GET /user-groups/{id}` | `UserGroup` |
| `userGroups.create(params)` | `{ name, display_name }` | `POST /user-groups` | `UserGroup` (201) |
| `userGroups.update(id, patch)` | partial `{ name?, display_name? }` | `PUT /user-groups/{id}` | `UserGroup` |
| `userGroups.remove(id)` | — | `DELETE /user-groups/{id}` | `void` |
| `userGroups.listMembers(id)` | — | `GET /user-groups/{id}/users` | `string[]` |
| `userGroups.setMembers(id, userIds)` | `string[]` | `PUT /user-groups/{id}/users` | `void` |
| `userGroups.setAllowedClients(id, clientIds)` | `string[]` | `PUT /user-groups/{id}/allowed-oidc-clients` | `void` |

## appConfig (`appconfig.mod.ts`)

| Method | Request | Endpoint | Returns |
| --- | --- | --- | --- |
| `appConfig.listPublic()` | — | `GET /application-configuration` | `ConfigVariable[]` `{ key, type, value }` |
| `appConfig.listAll()` | — | `GET /application-configuration/all` | `ConfigVariable[]` (+ `is_public`) |
| `appConfig.update(values)` | `Record<string, string>` | `PUT /application-configuration` | `ConfigVariable[]` |
| `appConfig.sendTestEmail(email?)` | `{ email? }` | `POST /application-configuration/test-email` | `void` (202) |

## Conventions

- Path params are interpolated client-side; IDs arrive as TypeID strings (`user_...`,
  `user_group_...`, `session` IDs are plain UUIDs).
- Query params: `undefined`/omitted values are dropped (ofetch/ufo behavior).
- Method names use the server's response semantics: `void` only when the endpoint returns no
  data (204 or empty success).
