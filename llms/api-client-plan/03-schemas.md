---
status: done
updated: 2026-09-17
---

# 03 — Schemas and Types

## Strategy

- One zod schema file per wire family under `api/client/schemas/`, named `*.schema.ts`
  (existing convention). Schemas are the single source of truth: TS types are `z.infer`.
- The SDK validates at the boundary only in tests — module methods do not parse every response
  (hot path stays allocation-free; trust the envelope's `data` typed by hand-written generics).
- Real schemas exist only for shipped namespaces (session, user, usergroup, appconfig,
  webauthn); there are no placeholder files for pending namespaces. Schema assertions live in
  `api/client/tests/`.
- Schemas that describe requests use the wire field names (snake_case, matching Go DTOs).

## Files

| File | Contents |
| --- | --- |
| `schemas/session.schema.ts` | `SignInSchema` (`identity`, `secret`), `SignInResultSchema` (`user`, `session_id`, `provider`, `expires_at?`), `ResetPasswordSchema` (`token`, `new_password`), `ForgotPasswordSchema` (`identity`), `SessionViewSchema` |
| `schemas/user.schema.ts` | `UserSchema` (full `User` DTO: `id`, `username`, `email`, nullable names, `display_name`, `is_admin`, `disabled`, timestamps), `CreateUserSchema`, `AdminUpdateUserSchema`, `UpdateProfileSchema`, `ChangePasswordSchema` |
| `schemas/usergroup.schema.ts` | `UserGroupSchema`, `CreateUserGroupSchema`, `UpdateUserGroupSchema` |
| `schemas/appconfig.schema.ts` | `ConfigVariableSchema` (`key`, `type`, `value`, `is_public?`) |
| `schemas/webauthn.schema.ts` | `WebAuthnBeginSchema` (`publicKey` unknown object, `session_id`), `WebAuthnCredentialSchema` — begin payloads are bare |

Nullable Go pointers → `z.string().nullable().optional()` (pointer *and* absent mean "keep");
`omitzero` JSON fields → optional. Booleans never optional in responses (`is_admin`, `disabled`,
`confirmed`).

## Derived TS types

Module methods use the inferred types: `User`, `SignInResult`, `SessionView`, `UserGroup`,
`ConfigVariable`, `Paginated<T>`, `FieldError`. `index.ts` re-exports every public type so SPA
code imports from one entry point (`~/apiclient/...` path alias already exists in tsconfig).
