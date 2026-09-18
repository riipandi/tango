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

describe('users module', () => {
  it('lists with pagination metadata', async () => {
    const { fetchMock, calls } = mockFetch([
      envelope([user], {
        page: 2,
        limit: 10,
        total_pages: 3,
        total_items: 25,
        first_item_index: 11,
        last_item_index: 20
      })
    ])
    const client = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock })

    const result = await client.users.list({ query: 'abb', page: 2, limit: 10 })

    expect(result.data).toHaveLength(1)
    expect(result.pagination).toEqual({
      page: 2,
      limit: 10,
      totalPages: 3,
      totalItems: 25,
      firstItemIndex: 11,
      lastItemIndex: 20
    })
    expect(expectCall(calls).url).toBe(`${BASE_URL}/api/users?query=abb&page=2&limit=10`)
  })

  it('omits pagination when the server skipped paging', async () => {
    const { fetchMock } = mockFetch([envelope([user], { page: -1, limit: -1 })])
    const client = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock })

    const result = await client.users.list({ page: -1 })

    expect(result.pagination).toBeUndefined()
    expect(result.data).toHaveLength(1)
  })

  it('performs CRUD by id', async () => {
    const { fetchMock, calls } = mockFetch([
      envelope(user),
      envelope(user, {}, 201),
      envelope(user),
      envelope(null, {}, 204)
    ])
    const client = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock })

    await expect(client.users.get('user_01j')).resolves.toMatchObject({ username: 'abbey' })
    await expect(
      client.users.create({ username: 'abbey', email: 'abbey@tango.local', is_admin: false })
    ).resolves.toMatchObject({ id: 'user_01j' })
    await expect(
      client.users.update('user_01j', { is_admin: true, display_name: 'Abbey R.' })
    ).resolves.toMatchObject({ display_name: 'Abbey' })
    await expect(client.users.remove('user_01j')).resolves.toBeUndefined()

    expect(expectCall(calls, 0).path).toBe('/api/users/user_01j')
    expect(expectCall(calls, 1).method).toBe('POST')
    expect(expectCall(calls, 2).method).toBe('PUT')
    expect(expectCall(calls, 3).method).toBe('DELETE')
  })

  it('reads and replaces group membership', async () => {
    const groups = [
      { id: 'ug_1', name: 'admins', display_name: 'Admins', created_at: '2026-01-01T00:00:00Z' }
    ]
    const { fetchMock, calls } = mockFetch([envelope(groups), envelope(null)])
    const client = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock })

    await expect(client.users.listGroups('user_01j')).resolves.toHaveLength(1)
    await expect(client.users.setGroups('user_01j', ['ug_1', 'ug_2'])).resolves.toBeUndefined()

    expect(expectCall(calls, 0).path).toBe('/api/users/user_01j/groups')
    const set = expectCall(calls, 1)
    expect(set.path).toBe('/api/users/user_01j/user-groups')
    expect(set.body).toBe(JSON.stringify(['ug_1', 'ug_2']))
  })

  it('uploads and removes the profile picture as multipart', async () => {
    const { fetchMock, calls } = mockFetch([{ status: 204 }, { status: 204 }])
    const client = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock })
    const file = new Blob(['png-bytes'], { type: 'image/png' })

    await client.users.updateProfilePicture('user_01j', file, 'avatar.png')
    await client.users.removeProfilePicture('user_01j')

    const upload = expectCall(calls, 0)
    expect(upload.method).toBe('PUT')
    expect(upload.path).toBe('/api/users/user_01j/profile-picture')
    expect(upload.formData).toBeInstanceOf(FormData)
    expect(upload.formData?.get('file')).toBeInstanceOf(Blob)
    expect(upload.headers.get('content-type')).not.toBe('application/json')
    expect(expectCall(calls, 1).path).toBe('/api/users/user_01j/profile-picture')
  })

  it('builds the profile picture URL without a request', () => {
    const { fetchMock } = mockFetch([])
    const client = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock })

    expect(client.users.profilePictureUrl('user_01j')).toBe(
      `${BASE_URL}/api/users/user_01j/profile-picture.png`
    )
  })
})
