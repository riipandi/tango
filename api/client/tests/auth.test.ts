import { describe, expect, it } from 'vitest'
import { createApiClient } from '../index'
import { envelope, expectCall, mockFetch } from './helpers'

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
