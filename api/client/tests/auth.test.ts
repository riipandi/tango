import { describe, expect, it } from 'vitest'
import { createApiClient } from '../index'
import { envelope, errorEnvelope, expectCall, mockFetch } from './helpers'

const BASE_URL = 'http://localhost:3080'

const user = {
  id: 'user_01j',
  username: 'abbey',
  email: 'abbey@tango.local',
  display_name: 'Abbey',
  is_admin: true,
  disabled: false,
  created_at: '2026-01-01T00:00:00Z'
}

describe('auth module', () => {
  it('signs in with password and returns the session result', async () => {
    const { fetchMock, calls } = mockFetch([
      envelope({ user, session_id: 'st_a', provider: 'password' })
    ])
    const client = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock })

    const result = await client.auth.signInWithPassword({ identity: 'abbey', secret: 's3cret' })

    expect(result.provider).toBe('password')
    const call = expectCall(calls)
    expect(call.method).toBe('POST')
    expect(call.path).toBe('/api/auth/sign-in')
    expect(call.body).toBe(JSON.stringify({ identity: 'abbey', secret: 's3cret' }))
  })

  it('reads the current session', async () => {
    const { fetchMock, calls } = mockFetch([
      envelope({ user, session_id: 'st_a', provider: 'password' })
    ])
    const client = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock })

    const result = await client.auth.getSession()

    expect(result.user.id).toBe('user_01j')
    expect(expectCall(calls).method).toBe('GET')
    expect(expectCall(calls).path).toBe('/api/auth/session')
  })

  it('signs out with POST', async () => {
    const { fetchMock, calls } = mockFetch([envelope(null)])
    const client = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock })

    await expect(client.auth.signOut()).resolves.toBeUndefined()
    expect(expectCall(calls).path).toBe('/api/auth/sign-out')
  })

  it('requests a recovery email and resets the password', async () => {
    const { fetchMock, calls } = mockFetch([envelope(null), envelope(null)])
    const client = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock })

    await client.auth.forgotPassword({ identity: 'abbey@tango.local' })
    await client.auth.resetPassword({ token: 'rt_1', new_password: 'n3wS3cret' })

    const forgot = expectCall(calls, 0)
    expect(forgot.path).toBe('/api/auth/forgot-password')
    expect(forgot.body).toBe(JSON.stringify({ identity: 'abbey@tango.local' }))
    const reset = expectCall(calls, 1)
    expect(reset.path).toBe('/api/auth/reset-password')
    expect(reset.body).toBe(JSON.stringify({ token: 'rt_1', new_password: 'n3wS3cret' }))
  })

  it('signs up and bootstraps the first admin', async () => {
    const { fetchMock, calls } = mockFetch([envelope(user, {}, 201), envelope(user, {}, 201)])
    const client = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock })
    const params = { username: 'abbey', email: 'abbey@tango.local' }

    await expect(client.auth.signUp(params)).resolves.toMatchObject({ username: 'abbey' })
    await expect(client.auth.setupAccount(params)).resolves.toMatchObject({ username: 'abbey' })

    expect(expectCall(calls, 0).path).toBe('/api/signup')
    expect(expectCall(calls, 1).path).toBe('/api/signup/setup')
  })

  it('maps setup availability from 204 and 404', async () => {
    const { fetchMock, calls } = mockFetch([
      { status: 204 },
      errorEnvelope(404, 'setup not available')
    ])
    const client = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock })

    await expect(client.auth.setupAvailable()).resolves.toBe(true)
    await expect(client.auth.setupAvailable()).resolves.toBe(false)
    expect(expectCall(calls, 0).path).toBe('/api/signup/setup')
    expect(expectCall(calls, 1).path).toBe('/api/signup/setup')
  })
})

describe('auth.mfa.totp module', () => {
  it('verifies the pending code', async () => {
    const { fetchMock, calls } = mockFetch([envelope(user)])
    const client = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock })

    await expect(client.auth.mfa.totp.verify({ code: '123456' })).resolves.toMatchObject({
      username: 'abbey'
    })
    expect(expectCall(calls).path).toBe('/api/mfa/totp/verify')
    expect(expectCall(calls).body).toBe(JSON.stringify({ code: '123456' }))
  })

  it('enrolls, confirms, and rotates recovery codes', async () => {
    const { fetchMock, calls } = mockFetch([
      envelope({ secret: 'JBSW', provisioning_uri: 'otpauth://x' }, {}, 201),
      envelope({ recovery_codes: ['rc1', 'rc2'] }),
      envelope({ recovery_codes: ['rc3'] })
    ])
    const client = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock })

    const enroll = await client.auth.mfa.totp.enroll()
    expect(enroll.secret).toBe('JBSW')

    const confirm = await client.auth.mfa.totp.confirm({ code: '123456' })
    expect(confirm.recovery_codes).toEqual(['rc1', 'rc2'])

    const rotated = await client.auth.mfa.totp.rotateRecoveryCodes()
    expect(rotated.recovery_codes).toEqual(['rc3'])

    expect(expectCall(calls, 0).path).toBe('/api/mfa/totp/enroll')
    expect(expectCall(calls, 1).path).toBe('/api/mfa/totp/confirm')
    expect(expectCall(calls, 2).path).toBe('/api/mfa/totp/recovery-codes')
  })

  it('reads status and disables', async () => {
    const { fetchMock, calls } = mockFetch([
      envelope({ confirmed: true, recovery_codes_remaining: 8 }),
      envelope(null, {}, 204)
    ])
    const client = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock })

    await expect(client.auth.mfa.totp.status()).resolves.toMatchObject({ confirmed: true })
    await expect(client.auth.mfa.totp.disable()).resolves.toBeUndefined()
    expect(expectCall(calls, 0).method).toBe('GET')
    expect(expectCall(calls, 1).method).toBe('DELETE')
    expect(expectCall(calls, 1).path).toBe('/api/mfa/totp')
  })
})

describe('auth.webauthn module', () => {
  it('returns the bare begin payload and posts the assertion with session query', async () => {
    const begin = { publicKey: { challenge: 'abc' }, session_id: 'ws_1' }
    const { fetchMock, calls } = mockFetch([{ status: 200, body: begin }, envelope(user)])
    const client = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock })

    const beginResult = await client.auth.webauthn.beginLogin()
    expect(beginResult).toEqual(begin)

    await expect(client.auth.webauthn.finishLogin({ id: 'cred' }, 'ws_1')).resolves.toMatchObject({
      username: 'abbey'
    })

    const finish = expectCall(calls, 1)
    expect(finish.path).toBe('/api/webauthn/login/finish')
    expect(finish.query.get('session_id')).toBe('ws_1')
    expect(finish.body).toBe(JSON.stringify({ id: 'cred' }))
  })

  it('registers a passkey with the ceremony session', async () => {
    const begin = { publicKey: { challenge: 'xyz' }, session_id: 'ws_9' }
    const credential = { id: 'cred_1', credential_id: 'aaaa', name: 'laptop' }
    const { fetchMock, calls } = mockFetch([
      { status: 200, body: begin },
      envelope(credential, {}, 201)
    ])
    const client = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock })

    const beginResult = await client.auth.webauthn.beginRegistration()
    expect(beginResult.session_id).toBe('ws_9')

    await expect(
      client.auth.webauthn.finishRegistration({ id: 'att' }, 'ws_9')
    ).resolves.toMatchObject({ id: 'cred_1' })

    const finish = expectCall(calls, 1)
    expect(finish.path).toBe('/api/webauthn/register/finish')
    expect(finish.query.get('session_id')).toBe('ws_9')
  })
})
