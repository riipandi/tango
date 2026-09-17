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

const sessionView = {
  id: 'st_a',
  provider: 'password',
  created_at: '2026-01-01T00:00:00Z',
  expires_at: '2026-02-01T00:00:00Z'
}

describe('account module', () => {
  it('reads and patches the profile', async () => {
    const { fetchMock, calls } = mockFetch([envelope(user), envelope(user)])
    const client = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock })

    await expect(client.account.getProfile()).resolves.toMatchObject({ username: 'abbey' })
    await expect(
      client.account.updateProfile({ display_name: 'Abbey R.', locale: 'id' })
    ).resolves.toMatchObject({ display_name: 'Abbey' })

    expect(expectCall(calls, 0).method).toBe('GET')
    expect(expectCall(calls, 0).path).toBe('/api/account')
    const patch = expectCall(calls, 1)
    expect(patch.method).toBe('PATCH')
    expect(patch.body).toBe(JSON.stringify({ display_name: 'Abbey R.', locale: 'id' }))
  })

  it('changes the password with PUT', async () => {
    const { fetchMock, calls } = mockFetch([envelope(null)])
    const client = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock })

    await expect(
      client.account.changePassword({ current_password: 'old', new_password: 'n3wS3cret' })
    ).resolves.toBeUndefined()

    const call = expectCall(calls)
    expect(call.method).toBe('PUT')
    expect(call.path).toBe('/api/account/password')
  })

  it('lists and revokes sessions', async () => {
    const { fetchMock, calls } = mockFetch([envelope([sessionView]), envelope(null)])
    const client = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock })

    const sessions = await client.account.listSessions()
    expect(sessions).toHaveLength(1)
    expect(sessions[0]?.provider).toBe('password')

    await expect(client.account.revokeSession('st_a')).resolves.toBeUndefined()

    const revoke = expectCall(calls, 1)
    expect(revoke.method).toBe('DELETE')
    expect(revoke.path).toBe('/api/account/sessions/st_a')
  })
})
