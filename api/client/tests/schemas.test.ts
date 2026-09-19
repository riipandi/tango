import { describe, expect, it } from 'vitest'
import {
  ForgotPasswordSchema,
  ResetPasswordSchema,
  SessionViewSchema,
  SignInResultSchema,
  SignInSchema
} from '../schemas/session.schema'
import {
  AdminUpdateUserSchema,
  ChangePasswordSchema,
  CreateUserSchema,
  UpdateProfileSchema,
  UserSchema
} from '../schemas/user.schema'
import { WebAuthnBeginSchema, WebAuthnCredentialSchema } from '../schemas/webauthn.schema'

const user = {
  id: 'user_01j',
  username: 'abbey',
  email: 'abbey@tango.local',
  display_name: 'Abbey',
  is_admin: true,
  disabled: false,
  created_at: '2026-01-01T00:00:00Z'
}

describe('schema contracts', () => {
  it('accepts a valid sign-in and rejects missing credentials', () => {
    expect(SignInSchema.safeParse({ identity: 'abbey', secret: 's3cret' }).success).toBe(true)
    expect(SignInSchema.safeParse({ identity: 'abbey' }).success).toBe(false)
  })

  it('accepts the user DTO and rejects a wrong boolean', () => {
    expect(UserSchema.safeParse(user).success).toBe(true)
    expect(UserSchema.safeParse({ ...user, is_admin: 'yes' }).success).toBe(false)
    expect(UserSchema.safeParse(null).success).toBe(false)
  })

  it('treats nullable profile fields as optional', () => {
    expect(UserSchema.safeParse({ ...user, locale: 'id' }).success).toBe(true)
    expect(UserSchema.safeParse({ ...user, locale: null }).success).toBe(true)
  })

  it('validates sign-in results and session views', () => {
    const result = { user, session_id: 'st_a', provider: 'password' }
    expect(SignInResultSchema.safeParse(result).success).toBe(true)
    expect(
      SessionViewSchema.safeParse({
        id: 'st_a',
        provider: 'password',
        created_at: '2026-01-01T00:00:00Z',
        expires_at: '2026-02-01T00:00:00Z'
      }).success
    ).toBe(true)
  })

  it('enforces the password reset minimum length', () => {
    expect(ResetPasswordSchema.safeParse({ token: 'rt', new_password: 'short' }).success).toBe(
      false
    )
    expect(ResetPasswordSchema.safeParse({ token: 'rt', new_password: 'n3wS3cret' }).success).toBe(
      true
    )
    expect(ForgotPasswordSchema.safeParse({ identity: 'abbey' }).success).toBe(true)
  })

  it('validates profile and password change payloads', () => {
    expect(UpdateProfileSchema.safeParse({ display_name: 'Abbey R.' }).success).toBe(true)
    expect(
      ChangePasswordSchema.safeParse({ current_password: 'x', new_password: 'short' }).success
    ).toBe(false)
    expect(CreateUserSchema.safeParse({ ...user, is_admin: undefined }).success).toBe(false)
    expect(AdminUpdateUserSchema.safeParse({ disabled: true }).success).toBe(true)
  })

  it('validates webauthn begin payloads', () => {
    expect(
      WebAuthnBeginSchema.safeParse({ publicKey: { challenge: 'c' }, session_id: 'ws_1' }).success
    ).toBe(true)
    expect(WebAuthnBeginSchema.safeParse({ publicKey: {} }).success).toBe(false)
    expect(WebAuthnCredentialSchema.safeParse({ id: 'c', credential_id: 'aa' }).success).toBe(true)
  })
})
