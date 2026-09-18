import { describe, expect, it } from 'vitest'
import { createApiClient } from '../index'
import { envelope, errorEnvelope, expectCall, mockFetch } from './helpers'

const BASE_URL = 'http://localhost:3080'

const variables = [
  { key: 'smtp_host', type: 'string', value: 'mailpit' },
  { key: 'smtp_port', type: 'int', value: '1025' }
]

describe('appConfig module', () => {
  it('lists public variables', async () => {
    const { fetchMock, calls } = mockFetch([envelope(variables)])
    const client = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock })

    const result = await client.appConfig.listPublic()

    expect(result).toHaveLength(2)
    expect(result[0]?.key).toBe('smtp_host')
    expect(expectCall(calls).path).toBe('/api/application-configuration')
  })

  it('lists all variables as admin', async () => {
    const withFlag = variables.map((v) => ({ ...v, is_public: false }))
    const { fetchMock, calls } = mockFetch([envelope(withFlag)])
    const client = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock })

    const result = await client.appConfig.listAll()

    expect(result[0]?.is_public).toBe(false)
    expect(expectCall(calls).path).toBe('/api/application-configuration/all')
  })

  it('updates the configuration map', async () => {
    const { fetchMock, calls } = mockFetch([envelope(variables)])
    const client = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock })

    await expect(client.appConfig.update({ smtp_host: 'smtpd' })).resolves.toHaveLength(2)

    const call = expectCall(calls)
    expect(call.method).toBe('PUT')
    expect(call.body).toBe(JSON.stringify({ smtp_host: 'smtpd' }))
  })

  it('queues a test email with an optional address', async () => {
    const { fetchMock, calls } = mockFetch([
      { status: 202, body: null },
      { status: 202, body: null }
    ])
    const client = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock })

    await client.appConfig.sendTestEmail('ops@tango.local')
    await client.appConfig.sendTestEmail()

    expect(expectCall(calls, 0).body).toBe(JSON.stringify({ email: 'ops@tango.local' }))
    expect(expectCall(calls, 1).body).toBe(JSON.stringify({}))
  })

  it('propagates guard errors (401) to the caller', async () => {
    const { fetchMock } = mockFetch([errorEnvelope(401, 'authentication required')])
    const client = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock })

    await expect(client.appConfig.listAll()).rejects.toMatchObject({
      name: 'ApiClientError',
      status: 401
    })
  })
})
