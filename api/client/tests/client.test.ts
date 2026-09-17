import { afterEach, describe, expect, it, vi } from 'vitest'

import { createApiClient, ApiClientError } from '../index'
import { envelope, expectCall, mockFetch } from './helpers'

const BASE_URL = 'http://localhost:3080'
const SESSION_TOKEN = 'st_testtoken'

afterEach(() => {
  vi.restoreAllMocks()
})

describe('client core', () => {
  it('unwraps the envelope into data plus camelCase metadata', async () => {
    const { fetchMock, calls } = mockFetch([
      envelope({ username: 'abbey' }, { trace_id: 'trace-1', total_items: 7 })
    ])
    const client = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock })

    const result = await client.raw<{ username: string }>('GET', '/api/account')

    expect(result.data).toEqual({ username: 'abbey' })
    expect(result.metadata.statusCode).toBe(200)
    expect(result.metadata.requestId).toBe('req_test')
    expect(result.metadata.traceId).toBe('trace-1')
    expect(result.metadata.totalItems).toBe(7)
    expect(expectCall(calls).url).toBe(`${BASE_URL}/api/account`)
  })

  it('passes bare documents through without envelope unwrapping', async () => {
    const begin = { publicKey: { challenge: 'abc' }, session_id: 'ws_1' }
    const { fetchMock } = mockFetch([{ status: 200, body: begin }])
    const client = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock })

    const result = await client.raw<Record<string, unknown>>('POST', '/api/webauthn/login/begin')

    expect(result.data).toEqual(begin)
    expect(result.metadata.statusCode).toBe(200)
  })

  it('joins baseUrl with API paths', async () => {
    const { fetchMock, calls } = mockFetch([envelope([])])
    const client = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock })

    await client.raw('GET', '/api/users')

    expect(expectCall(calls).url).toBe(`${BASE_URL}/api/users`)
  })

  it('defaults to include credentials so the session cookie flows', async () => {
    const { fetchMock, calls } = mockFetch([envelope({})])
    const client = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock })

    await client.raw('GET', '/api/auth/session')

    const call = expectCall(calls)
    expect(call.credentials).toBe('include')
  })

  it('merges client headers with per-call headers', async () => {
    const { fetchMock, calls } = mockFetch([envelope({})])
    const client = createApiClient({
      baseUrl: BASE_URL,
      fetch: fetchMock,
      headers: { 'X-API-KEY': 'admin_key' }
    })

    await client.raw('GET', '/api/users', { headers: { 'X-Trace-Id': 't-42' } })

    const call = expectCall(calls)
    expect(call.headers.get('x-api-key')).toBe('admin_key')
    expect(call.headers.get('x-trace-id')).toBe('t-42')
  })

  it('forwards the abort signal', async () => {
    const { fetchMock, calls } = mockFetch([envelope({})])
    const client = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock })
    const controller = new AbortController()

    await client.raw('GET', '/api/users', { signal: controller.signal })

    expect(expectCall(calls).signal).toBe(controller.signal)
  })

  it('encodes query parameters and drops undefined values', async () => {
    const { fetchMock, calls } = mockFetch([envelope([])])
    const client = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock })

    await client.raw('GET', '/api/users', { query: { query: 'abb', page: 2, limit: undefined } })

    const call = expectCall(calls)
    expect(call.url).toBe(`${BASE_URL}/api/users?query=abb&page=2`)
  })

  it('throws ApiClientError for network failures', async () => {
    const fetchMock = vi.fn(async () => {
      throw new TypeError('fetch failed')
    }) as unknown as typeof globalThis.fetch
    const client = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock })

    await expect(client.raw('GET', '/api/account')).rejects.toMatchObject({
      name: 'ApiClientError',
      code: 'network_error',
      status: undefined
    })
  })

  it('throws when a 2xx envelope still reports an error status', async () => {
    const { fetchMock } = mockFetch([{ status: 200, body: { status: 'error', metadata: { status_code: 200, request_id: 'req_test' } } }])
    const client = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock })

    await expect(client.raw('GET', '/api/account')).rejects.toBeInstanceOf(ApiClientError)
  })

  it('signs in through the auth namespace', async () => {
    const user = {
      id: 'user_01j',
      username: 'abbey',
      email: 'abbey@tango.local',
      display_name: 'Abbey',
      is_admin: true,
      disabled: false,
      created_at: '2026-01-01T00:00:00Z'
    }
    const { fetchMock, calls } = mockFetch([
      envelope({ user, session_id: SESSION_TOKEN, provider: 'password', expires_at: '2026-02-01T00:00:00Z' })
    ])
    const client = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock })

    const result = await client.auth.signInWithPassword({ identity: 'abbey', secret: 's3cret' })

    expect(result.session_id).toBe(SESSION_TOKEN)
    expect(result.user.username).toBe('abbey')
    const call = expectCall(calls)
    expect(call.method).toBe('POST')
    expect(call.path).toBe('/api/auth/sign-in')
    expect(call.body).toBe(JSON.stringify({ identity: 'abbey', secret: 's3cret' }))
  })
})
