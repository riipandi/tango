import { describe, expect, it } from 'vitest'

import { createApiClient } from '../index'
import { envelope, expectCall, mockFetch } from './helpers'

const BASE_URL = 'http://localhost:3080'

const group = {
  id: 'ug_1',
  name: 'admins',
  display_name: 'Admins',
  created_at: '2026-01-01T00:00:00Z'
}

describe('userGroups module', () => {
  it('lists groups with pagination', async () => {
    const { fetchMock, calls } = mockFetch([
      envelope([group], { page: 1, limit: 20, total_pages: 1, total_items: 1 })
    ])
    const client = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock })

    const result = await client.userGroups.list({ query: 'adm', page: 1, limit: 20 })

    expect(result.pagination?.totalItems).toBe(1)
    expect(expectCall(calls).url).toBe(`${BASE_URL}/api/user-groups?query=adm&page=1&limit=20`)
  })

  it('performs CRUD by id', async () => {
    const { fetchMock, calls } = mockFetch([
      envelope(group),
      envelope(group, {}, 201),
      envelope(group),
      envelope(null, {}, 204)
    ])
    const client = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock })

    await expect(client.userGroups.get('ug_1')).resolves.toMatchObject({ name: 'admins' })
    await expect(
      client.userGroups.create({ name: 'admins', display_name: 'Admins' })
    ).resolves.toMatchObject({ id: 'ug_1' })
    await expect(
      client.userGroups.update('ug_1', { display_name: 'Administrators' })
    ).resolves.toMatchObject({ id: 'ug_1' })
    await expect(client.userGroups.remove('ug_1')).resolves.toBeUndefined()

    expect(expectCall(calls, 1).method).toBe('POST')
    expect(expectCall(calls, 2).method).toBe('PUT')
    expect(expectCall(calls, 3).method).toBe('DELETE')
  })

  it('manages members and allowed clients', async () => {
    const { fetchMock, calls } = mockFetch([envelope(['user_01j']), envelope(null), envelope(null)])
    const client = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock })

    await expect(client.userGroups.listMembers('ug_1')).resolves.toEqual(['user_01j'])
    await expect(client.userGroups.setMembers('ug_1', ['user_01j'])).resolves.toBeUndefined()
    await expect(
      client.userGroups.setAllowedClients('ug_1', ['oidc_client_01j'])
    ).resolves.toBeUndefined()

    expect(expectCall(calls, 1).path).toBe('/api/user-groups/ug_1/users')
    expect(expectCall(calls, 2).path).toBe('/api/user-groups/ug_1/allowed-oidc-clients')
    expect(expectCall(calls, 2).body).toBe(JSON.stringify(['oidc_client_01j']))
  })
})
