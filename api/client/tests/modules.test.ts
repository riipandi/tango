import { describe, expect, it } from 'vitest'
import { createApiClient } from '../index'
import { envelope, expectCall, mockFetch } from './helpers'

const BASE_URL = 'http://localhost:3080'

describe('deviceLogin module', () => {
  it('creates, exchanges (pending and success), inspects, and decides', async () => {
    const request = {
      id: 'dl_1',
      user_code: 'EABCD12',
      expires_at: 'x',
      interval: 5,
      verification_uri: 'http://localhost:3080/device',
      verification_uri_complete: 'http://localhost:3080/device?code=EABCD12'
    }
    const user = {
      id: 'user_01j',
      username: 'abbey',
      email: 'a@t.local',
      display_name: 'A',
      is_admin: false,
      disabled: false,
      created_at: 'x'
    }
    const { fetchMock, calls } = mockFetch([
      envelope(request, {}, 201),
      envelope({ status: 'pending', interval: 5 }, {}, 202),
      envelope(user),
      envelope({
        user_code: 'EABCD12',
        device: 'CLI',
        ip_address: '1.2.3.4',
        city: '',
        country: '',
        expires_at: 'x'
      }),
      envelope(null, {}, 204)
    ])
    const c = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock })

    await expect(c.deviceLogin.create()).resolves.toMatchObject({ user_code: 'EABCD12' })
    const pending = await c.deviceLogin.exchange('dl_1')
    expect(pending).toMatchObject({ status: 'pending' })
    const done = await c.deviceLogin.exchange('dl_1')
    expect(done).toMatchObject({ id: 'user_01j' })
    await expect(c.deviceLogin.inspect('EABCD12')).resolves.toMatchObject({ device: 'CLI' })
    await c.deviceLogin.decide('EABCD12', 'approve')

    expect(expectCall(calls, 1).path).toBe('/api/device-login/requests/dl_1/exchange')
    expect(expectCall(calls, 3).body).toBe(JSON.stringify({ code: 'EABCD12' }))
    expect(expectCall(calls, 4).path).toBe('/api/device-login/verification/decision')
    expect(expectCall(calls, 4).body).toBe(JSON.stringify({ code: 'EABCD12', decision: 'approve' }))
  })
})

describe('system module', () => {
  it('probes health', async () => {
    const { fetchMock, calls } = mockFetch([{ status: 200, body: { status: 'ok' } }])
    const c = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock })

    await expect(c.system.health()).resolves.toMatchObject({ status: 'ok' })

    expect(expectCall(calls, 0).path).toBe('/api/healthz')
  })
})

describe('deviceApproval module', () => {
  it('reads consent info and posts the decision form-encoded', async () => {
    const { fetchMock, calls } = mockFetch([
      envelope({ client_id: 'oidc_client_01j', client_name: 'CLI', scope: 'openid profile' }),
      envelope(null, {}, 204)
    ])
    const c = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock })

    await expect(c.deviceApproval.info('EABCD12')).resolves.toMatchObject({ client_name: 'CLI' })
    await c.deviceApproval.verify('EABCD12', 'approve')

    expect(expectCall(calls, 0).query.get('code')).toBe('EABCD12')
    expect(expectCall(calls, 1).path).toBe('/api/oidc/device/verify')
    expect(expectCall(calls, 1).formBody).toBe('code=EABCD12&action=approve')
  })
})

describe('apiKey auth handling', () => {
  it('sends the X-API-KEY header from options', async () => {
    const { fetchMock, calls } = mockFetch([envelope([])])
    const c = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock, apiKey: 'k-123' })

    await c.raw('GET', '/healthz')

    expect(expectCall(calls).headers.get('x-api-key')).toBe('k-123')
  })

  it('rotates and clears the key via setApiKey', async () => {
    const { fetchMock, calls } = mockFetch([envelope([]), envelope([])])
    const c = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock, apiKey: 'old' })

    c.setApiKey('new')
    await c.raw('GET', '/healthz')
    c.setApiKey(undefined)
    await c.raw('GET', '/healthz')

    expect(expectCall(calls, 0).headers.get('x-api-key')).toBe('new')
    expect(expectCall(calls, 1).headers.get('x-api-key')).toBeNull()
  })

  it('lets per-call headers override the API key', async () => {
    const { fetchMock, calls } = mockFetch([envelope([])])
    const c = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock, apiKey: 'default' })

    await c.raw('GET', '/healthz', { headers: { 'X-API-KEY': 'override' } })

    expect(expectCall(calls).headers.get('x-api-key')).toBe('override')
  })
})
