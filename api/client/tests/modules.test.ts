import { describe, expect, it } from 'vitest'
import { createApiClient } from '../index'
import { envelope, expectCall, mockFetch } from './helpers'

const BASE_URL = 'http://localhost:3080'

describe('oidcClients module', () => {
  const view = {
    id: 'oidc_client_01j',
    name: 'app',
    callback_urls: ['https://app.test/cb'],
    has_secret: true,
    is_public: false,
    pkce_enabled: true
  }

  it('performs client CRUD', async () => {
    const created = { ...view, client_secret: 'raw-secret' }
    const { fetchMock, calls } = mockFetch([
      envelope([view]),
      envelope(view),
      envelope(created, {}, 201),
      envelope(view),
      envelope(null, {}, 204)
    ])
    const c = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock })

    await expect(c.oidcClients.list()).resolves.toHaveLength(1)
    await expect(c.oidcClients.get('oidc_client_01j')).resolves.toMatchObject({ name: 'app' })
    await expect(
      c.oidcClients.create({ name: 'app', callback_urls: ['https://app.test/cb'] })
    ).resolves.toMatchObject({ client_secret: 'raw-secret' })
    await expect(
      c.oidcClients.update('oidc_client_01j', { name: 'app', callback_urls: [] })
    ).resolves.toMatchObject({ name: 'app' })
    await expect(c.oidcClients.remove('oidc_client_01j')).resolves.toBeUndefined()

    expect(expectCall(calls, 1).path).toBe('/api/oidc/clients/oidc_client_01j')
    expect(expectCall(calls, 4).method).toBe('DELETE')
  })

  it('manages groups, meta, preview, secrets, logo, and refresh', async () => {
    const { fetchMock, calls } = mockFetch([
      envelope(view),
      envelope({ ...view, has_logo: false, has_dark_logo: false }),
      envelope({ id_token: 'i', access_token: 'a', user_info: {} }),
      envelope([{ id: 'legacy', created_at: 'x', is_active: true }]),
      envelope({ id: 'new', created_at: 'x', is_active: true, secret: 's3cret' }, {}, 201),
      envelope(null, {}, 204),
      envelope(view)
    ])
    const c = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock })

    await c.oidcClients.setAllowedGroups('oidc_client_01j', ['ug_1'])
    await c.oidcClients.meta('oidc_client_01j')
    await c.oidcClients.preview('oidc_client_01j', 'user_01j', 'openid profile')
    await c.oidcClients.listSecrets('oidc_client_01j')
    const secret = await c.oidcClients.createSecret('oidc_client_01j')
    await c.oidcClients.removeSecret('oidc_client_01j', 'new')
    await c.oidcClients.refresh('oidc_client_01j')

    expect(secret.secret).toBe('s3cret')
    expect(expectCall(calls, 0).path).toBe('/api/oidc/clients/oidc_client_01j/allowed-user-groups')
    expect(expectCall(calls, 0).body).toBe(JSON.stringify({ user_group_ids: ['ug_1'] }))
    const preview = expectCall(calls, 2)
    expect(preview.path).toBe('/api/oidc/clients/oidc_client_01j/preview/user_01j')
    expect(preview.query.get('scopes')).toBe('openid profile')
    expect(expectCall(calls, 6).method).toBe('POST')
  })

  it('uploads and removes the logo as multipart', async () => {
    const { fetchMock, calls } = mockFetch([{ status: 204 }, { status: 204 }])
    const c = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock })
    const file = new Blob(['png'], { type: 'image/png' })

    await c.oidcClients.updateLogo('oidc_client_01j', file, 'logo.png')
    await c.oidcClients.removeLogo('oidc_client_01j')

    expect(expectCall(calls, 0).formData?.get('file')).toBeInstanceOf(Blob)
    expect(expectCall(calls, 1).path).toBe('/api/oidc/clients/oidc_client_01j/logo')
  })
})

describe('consent module', () => {
  it('lists and revokes my consents plus admin views', async () => {
    const authorized = {
      user_id: 'user_01j',
      client_id: 'oidc_client_01j',
      scopes: ['openid'],
      last_used_at: 'x'
    }
    const { fetchMock, calls } = mockFetch([
      envelope([authorized]),
      envelope(null, {}, 204),
      envelope([]),
      envelope([authorized]),
      envelope([authorized])
    ])
    const c = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock })

    await expect(c.consent.listMine()).resolves.toHaveLength(1)
    await expect(c.consent.revokeMine('oidc_client_01j')).resolves.toBeUndefined()
    await expect(c.consent.listAccessible()).resolves.toEqual([])
    await expect(c.consent.listForUser('user_01j')).resolves.toHaveLength(1)
    await expect(c.consent.listAll()).resolves.toHaveLength(1)

    expect(expectCall(calls, 0).path).toBe('/api/oidc/users/me/authorized-clients')
    expect(expectCall(calls, 1).path).toBe('/api/oidc/users/me/authorized-clients/oidc_client_01j')
    expect(expectCall(calls, 2).path).toBe('/api/oidc/users/me/clients')
    expect(expectCall(calls, 4).path).toBe('/api/oidc/authorized-clients')
  })
})

describe('scim module', () => {
  it('manages service providers', async () => {
    const provider = {
      id: 'scim_1',
      endpoint: 'https://scim.test',
      oidc_client_id: 'c1',
      created_at: 'x'
    }
    const { fetchMock, calls } = mockFetch([
      envelope([provider]),
      envelope(provider, {}, 201),
      envelope(provider),
      envelope(null, {}, 204),
      envelope(null, {}, 204)
    ])
    const c = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock })

    await expect(c.scim.list()).resolves.toHaveLength(1)
    await expect(
      c.scim.create({ endpoint: 'https://scim.test', token: 't' })
    ).resolves.toMatchObject({
      id: 'scim_1'
    })
    await expect(
      c.scim.update('scim_1', { endpoint: 'https://scim2.test', token: 't' })
    ).resolves.toMatchObject({ id: 'scim_1' })
    await c.scim.sync('scim_1')
    await c.scim.remove('scim_1')

    expect(expectCall(calls, 0).path).toBe('/api/scim/service-provider')
    expect(expectCall(calls, 3).path).toBe('/api/scim/service-provider/scim_1/sync')
  })

  it('resolves the client provider to null on 404', async () => {
    const { fetchMock } = mockFetch([envelope(null, {}, 404)])
    const c = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock })

    await expect(c.scim.getByClient('oidc_client_01j')).resolves.toBeNull()
  })
})

describe('apiKeys module', () => {
  it('creates, lists, renews, and revokes keys', async () => {
    const key = { id: 'ak_1', name: 'ci', expires_at: '2027-01-01T00:00:00Z', created_at: 'x' }
    const secret = { api_key: key, token: 'plain' }
    const { fetchMock, calls } = mockFetch([
      envelope([key], { page: 1, limit: 20, total_pages: 1, total_items: 1 }),
      envelope(secret, {}, 201),
      envelope(secret),
      envelope(null, {}, 204)
    ])
    const c = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock })

    const list = await c.apiKeys.list()
    expect(list.pagination?.totalItems).toBe(1)
    await expect(
      c.apiKeys.create({ name: 'ci', expires_at: '2027-01-01T00:00:00Z' })
    ).resolves.toMatchObject({ token: 'plain' })
    await expect(
      c.apiKeys.renew('ak_1', { expires_at: '2027-06-01T00:00:00Z' })
    ).resolves.toMatchObject({ token: 'plain' })
    await expect(c.apiKeys.revoke('ak_1')).resolves.toBeUndefined()

    expect(expectCall(calls, 0).path).toBe('/api/api-keys')
    expect(expectCall(calls, 2).path).toBe('/api/api-keys/ak_1/renew')
  })
})

describe('webhooks module', () => {
  it('manages endpoints, test deliveries, and secrets', async () => {
    const hook = {
      id: 'wh_1',
      name: 'my-endpoint',
      endpoint: 'https://r.test/hook',
      method: 'POST',
      enabled: true,
      event_types: [],
      created_at: 'x'
    }
    const { fetchMock, calls } = mockFetch([
      envelope([hook], { page: 1, limit: 20, total_pages: 1, total_items: 1 }),
      envelope(hook),
      envelope({ ...hook, secret: 'whsec_1' }, {}, 201),
      envelope(hook),
      envelope(null, {}, 204),
      envelope({ delivery_id: 'wd_1' }, {}, 202),
      envelope({ ...hook, secret: 'whsec_2' })
    ])
    const c = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock })

    const list = await c.webhooks.list({ enabled: true, event: 'user.created' })
    expect(list.pagination?.totalItems).toBe(1)
    await expect(c.webhooks.get('wh_1')).resolves.toMatchObject({ id: 'wh_1' })
    const created = await c.webhooks.create({
      name: 'my-endpoint',
      endpoint: 'https://r.test/hook',
      event_types: ['user.created']
    })
    expect(created.secret).toBe('whsec_1')
    await c.webhooks.update('wh_1', { name: 'my-endpoint-renamed', enabled: true })
    await c.webhooks.remove('wh_1')
    await expect(c.webhooks.sendTest('wh_1')).resolves.toMatchObject({ delivery_id: 'wd_1' })
    await expect(c.webhooks.rotateSecret('wh_1')).resolves.toMatchObject({ secret: 'whsec_2' })

    expect(expectCall(calls, 0).url).toContain('/api/webhooks?enabled=true&event=user.created')
    expect(expectCall(calls, 5).path).toBe('/api/webhooks/wh_1/test')
    expect(expectCall(calls, 6).path).toBe('/api/webhooks/wh_1/rotate-secret')
  })

  it('lists deliveries globally and per endpoint', async () => {
    const delivery = { id: 'wd_1', attempts: 1, succeeded: true, created_at: 'x' }
    const { fetchMock, calls } = mockFetch([
      envelope([delivery], { page: 1, limit: 20, total_pages: 1, total_items: 1 }),
      envelope([])
    ])
    const c = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock })

    await expect(c.webhooks.listDeliveries({ page: 1, limit: 20 })).resolves.toMatchObject({
      data: [delivery]
    })
    await c.webhooks.listDeliveriesFor('wh_1')

    expect(expectCall(calls, 0).path).toBe('/api/webhook-deliveries')
    expect(expectCall(calls, 1).path).toBe('/api/webhooks/wh_1/deliveries')
  })
})

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
  it('reads versions and health', async () => {
    const { fetchMock, calls } = mockFetch([
      envelope({ current_version: '1.0.0' }),
      envelope({ latest_version: '1.1.0' }),
      { status: 200, body: { status: 'ok' } }
    ])
    const c = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock })

    await expect(c.system.versionCurrent()).resolves.toMatchObject({ current_version: '1.0.0' })
    await expect(c.system.versionLatest()).resolves.toMatchObject({ latest_version: '1.1.0' })
    await expect(c.system.health()).resolves.toMatchObject({ status: 'ok' })

    expect(expectCall(calls, 2).path).toBe('/api/healthz')
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

describe('oidcClients logo URL', () => {
  it('exposes the direct logo URL without requesting it', () => {
    const { fetchMock } = mockFetch([])
    const c = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock })

    expect(c.oidcClients.logoUrl('oidc_client_01j')).toBe(
      `${BASE_URL}/api/oidc/clients/oidc_client_01j/logo`
    )
  })
})

describe('apiKey auth handling', () => {
  it('sends the X-API-KEY header from options', async () => {
    const { fetchMock, calls } = mockFetch([envelope([])])
    const c = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock, apiKey: 'k-123' })

    await c.apiKeys.list()

    expect(expectCall(calls).headers.get('x-api-key')).toBe('k-123')
  })

  it('rotates and clears the key via setApiKey', async () => {
    const { fetchMock, calls } = mockFetch([envelope([]), envelope([])])
    const c = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock, apiKey: 'old' })

    c.setApiKey('new')
    await c.apiKeys.list()
    c.setApiKey(undefined)
    await c.apiKeys.list()

    expect(expectCall(calls, 0).headers.get('x-api-key')).toBe('new')
    expect(expectCall(calls, 1).headers.get('x-api-key')).toBeNull()
  })

  it('lets per-call headers override the API key', async () => {
    const { fetchMock, calls } = mockFetch([envelope([])])
    const c = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock, apiKey: 'default' })

    await c.apiKeys.list()

    expect(expectCall(calls).headers.get('x-api-key')).toBe('default')
  })
})
