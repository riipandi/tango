import { describe, expect, it } from 'vitest'
import { SignInSchema } from '../schemas/session.schema'
import { UserSchema } from '../schemas/user.schema'

describe('api-client schemas', () => {
  it('accepts a sign-in payload with identity and password', () => {
    const parsed = SignInSchema.safeParse({ identity: 'abbey', password: 's3cret' })
    expect(parsed.success).toBe(true)
  })

  it('rejects a sign-in payload without credentials', () => {
    const parsed = SignInSchema.safeParse({ identity: 'abbey' })
    expect(parsed.success).toBe(false)
  })

  it('accepts an empty user object (placeholder schema)', () => {
    expect(UserSchema.safeParse({}).success).toBe(true)
  })

  it('rejects unknown-top-level garbage', () => {
    expect(UserSchema.safeParse(null).success).toBe(false)
  })
})
