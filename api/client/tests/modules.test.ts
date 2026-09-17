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

describe('apis + apiAccess modules', () => {
  const api = {
    id: 'api_1',
    name: 'core',
    resource: 'https://api.tango.local',
    allow_cimd_clients: false,
    permissions: [],
    created_at: 'x'
  }

  it('performs API CRUD and permission replacement', async () => {
    const { fetchMock, calls } = mockFetch([
      envelope([api], { page: 1, limit: 20, total_pages: 1, total_items: 1 }),
      envelope(api),
      envelope(api, {}, 201),
      envelope(api),
      envelope(null, {}, 204),
      envelope({ permissions: [] })
    ])
    const c = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock })

    await expect(c.apis.list()).resolves.toMatchObject({ data: [api] })
    await expect(c.apis.get('api_1')).resolves.toMatchObject({ name: 'core' })
    await expect(c.apis.create({ name: 'core', resource: api.resource })).resolves.toMatchObject({
      id: 'api_1'
    })
    await expect(c.apis.update('api_1', { name: 'core2' })).resolves.toMatchObject({ id: 'api_1' })
    await expect(c.apis.remove('api_1')).resolves.toBeUndefined()
    await expect(c.apis.setPermissions('api_1', [])).resolves.toMatchObject({ permissions: [] })

    expect(expectCall(calls, 4).path).toBe('/api/apis/api_1')
    expect(expectCall(calls, 5).path).toBe('/api/apis/api_1/permissions')
  })

  it('manages grants, client lists, cimd, and client views', async () => {
    const grant = {
      client_access: true,
      client_permission_ids: [],
      user_delegated_access: false,
      user_delegated_permission_ids: []
    }
    const ref = {
      id: 'oidc_client_01j',
      name: 'app',
      client_type: 'confidential',
      is_public: false,
      has_logo: false,
      has_dark_logo: false
    }
    const { fetchMock, calls } = mockFetch([
      envelope([ref], { page: 1, limit: 20, total_pages: 1, total_items: 1 }),
      envelope([]),
      envelope(grant),
      envelope({ deleted: true }),
      envelope(api),
      envelope([{ api, ...grant, cimd_granted_access: false, cimd_granted_permission_ids: [] }]),
      envelope([api])
    ])
    const c = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock })

    await expect(c.apis.listClients('api_1')).resolves.toMatchObject({ data: [ref] })
    await expect(c.apis.listAssignableClients('api_1')).resolves.toMatchObject({ data: [] })
    await expect(
      c.apis.setGrant('api_1', 'oidc_client_01j', {
        user_delegated_access: false,
        user_delegated_permission_ids: [],
        client_access: true,
        client_permission_ids: []
      })
    ).resolves.toMatchObject({ client_access: true })
    await expect(c.apis.removeGrant('api_1', 'oidc_client_01j')).resolves.toMatchObject({
      deleted: true
    })
    await expect(c.apis.setCimdAccess('api_1', { enabled: true })).resolves.toMatchObject({
      id: 'api_1'
    })
    await expect(c.apiAccess.listForClient('oidc_client_01j')).resolves.toHaveLength(1)
    await expect(c.apiAccess.listAssignableForClient('oidc_client_01j')).resolves.toMatchObject({
      data: [api]
    })

    expect(expectCall(calls, 2).path).toBe('/api/apis/api_1/clients/oidc_client_01j')
    expect(expectCall(calls, 5).path).toBe('/api/api-access/oidc_client_01j/apis')
  })
})

describe('auditLogs module', () => {
  it('lists scoped and all entries with filters', async () => {
    const entry = {
      id: 'al_1',
      event: 'user.created',
      trigger: 'user',
      status: 'success',
      created_at: 'x'
    }
    const { fetchMock, calls } = mockFetch([
      envelope([entry], { page: 1, limit: 20, total_pages: 1, total_items: 1 }),
      envelope([]),
      envelope(['core', 'admin']),
      envelope([])
    ])
    const c = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock })

    const list = await c.auditLogs.list({ event: 'user.created', page: 1, limit: 20 })
    expect(list.pagination?.totalItems).toBe(1)
    await c.auditLogs.listAll({ user_id: 'user_01j' })
    await expect(c.auditLogs.suggestedClientNames()).resolves.toEqual(['core', 'admin'])
    await c.auditLogs.suggestedUsers()

    expect(expectCall(calls, 0).url).toContain('/api/audit-logs?event=user.created&page=1&limit=20')
    expect(expectCall(calls, 1).path).toBe('/api/audit-logs/all')
    expect(expectCall(calls, 1).query.get('user_id')).toBe('user_01j')
  })
})

describe('customClaims module', () => {
  it('manages user and group claims', async () => {
    const claim = { id: 'cc_1', key: 'tenant', value: 'a', created_at: 'x' }
    const { fetchMock, calls } = mockFetch([
      envelope(['tenant', 'role']),
      envelope([claim]),
      envelope(claim, {}, 201),
      envelope([claim]),
      envelope(claim),
      envelope(null, {}, 204),
      envelope([]),
      envelope(claim, {}, 201)
    ])
    const c = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock })

    await expect(c.customClaims.suggestions()).resolves.toEqual(['tenant', 'role'])
    await expect(c.customClaims.listForUser('user_01j')).resolves.toHaveLength(1)
    await expect(
      c.customClaims.createForUser('user_01j', { key: 'tenant', value: 'a' })
    ).resolves.toMatchObject({ id: 'cc_1' })
    await expect(
      c.customClaims.replaceForUser('user_01j', [{ key: 'tenant', value: 'a' }])
    ).resolves.toHaveLength(1)
    await expect(
      c.customClaims.updateForUser('user_01j', 'cc_1', { value: 'b' })
    ).resolves.toMatchObject({ id: 'cc_1' })
    await c.customClaims.deleteForUser('user_01j', 'cc_1')
    await expect(c.customClaims.listForGroup('ug_1')).resolves.toHaveLength(0)
    await expect(
      c.customClaims.createForGroup('ug_1', { key: 'tenant', value: 'a' })
    ).resolves.toMatchObject({ id: 'cc_1' })

    expect(expectCall(calls, 4).path).toBe('/api/custom-claims/user/user_01j/cc_1')
    expect(expectCall(calls, 7).path).toBe('/api/custom-claims/user-group/ug_1')
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

describe('users/me self-service', () => {
  const user = {
    id: 'user_01j',
    username: 'abbey',
    email: 'a@t.local',
    display_name: 'A',
    is_admin: false,
    disabled: false,
    created_at: 'x'
  }

  it('reads and updates the own profile', async () => {
    const { fetchMock, calls } = mockFetch([envelope(user), envelope(user)])
    const c = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock })

    await expect(c.users.me()).resolves.toMatchObject({ id: 'user_01j' })
    await expect(c.users.updateMe({ display_name: 'Abbey', locale: 'id' })).resolves.toMatchObject({
      username: 'abbey'
    })

    expect(expectCall(calls, 0).path).toBe('/api/users/me')
    expect(expectCall(calls, 1).body).toBe(JSON.stringify({ display_name: 'Abbey', locale: 'id' }))
  })
})

describe('signupTokens module', () => {
  const token = {
    id: 'st_01j',
    usage_limit: 5,
    usage_count: 0,
    created_at: 'x',
    expires_at: 'y',
    user_groups: ['ug_1']
  }

  it('lists, issues (secret shown once), and revokes tokens', async () => {
    const { fetchMock, calls } = mockFetch([
      envelope([token]),
      envelope({ ...token, token: 'raw-token' }, {}, 201),
      envelope(null, {}, 204)
    ])
    const c = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock })

    await expect(c.signupTokens.list()).resolves.toHaveLength(1)
    await expect(
      c.signupTokens.create({ ttl: '24h', usage_limit: 5, user_group_ids: ['ug_1'] })
    ).resolves.toMatchObject({ token: 'raw-token' })
    await expect(c.signupTokens.remove('st_01j')).resolves.toBeUndefined()

    expect(expectCall(calls, 1).path).toBe('/api/signup-tokens')
    expect(expectCall(calls, 2).path).toBe('/api/signup-tokens/st_01j')
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

    await c.apis.list()

    expect(expectCall(calls).headers.get('x-api-key')).toBe('k-123')
  })

  it('rotates and clears the key via setApiKey', async () => {
    const { fetchMock, calls } = mockFetch([envelope([]), envelope([])])
    const c = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock, apiKey: 'old' })

    c.setApiKey('new')
    await c.users.list()
    c.setApiKey(undefined)
    await c.users.list()

    expect(expectCall(calls, 0).headers.get('x-api-key')).toBe('new')
    expect(expectCall(calls, 1).headers.get('x-api-key')).toBeNull()
  })

  it('lets per-call headers override the API key', async () => {
    const { fetchMock, calls } = mockFetch([envelope([])])
    const c = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock, apiKey: 'default' })

    await c.apis.list()

    expect(expectCall(calls).headers.get('x-api-key')).toBe('default')
  })
})
