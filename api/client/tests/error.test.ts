import { afterEach, describe, expect, it, vi } from 'vitest'
import { createApiClient, ApiClientError, toApiClientError } from '../index'
import { errorEnvelope, mockFetch } from './helpers'

const BASE_URL = 'http://localhost:3080'

type ApiError = InstanceType<typeof ApiClientError>

function catchError(promise: Promise<unknown>): Promise<ApiError> {
  return promise.catch((error: unknown) => error) as Promise<ApiError>
}

afterEach(() => {
  vi.restoreAllMocks()
})

describe('error normalization', () => {
  it('carries validation field errors from a 422 envelope', async () => {
    const { fetchMock } = mockFetch([
      errorEnvelope(422, 'validation failed', [
        { field: 'identity', message: 'cannot be blank' },
        { field: 'secret', message: 'cannot be blank' }
      ])
    ])
    const client = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock })

    const error = await catchError(client.auth.signInWithPassword({ identity: '', secret: '' }))

    expect(error).toBeInstanceOf(ApiClientError)
    expect(error.status).toBe(422)
    expect(error.code).toBe('api_error')
    expect(error.message).toBe('validation failed')
    expect(error.fieldErrors).toEqual([
      { field: 'identity', message: 'cannot be blank' },
      { field: 'secret', message: 'cannot be blank' }
    ])
  })

  it('carries the rate limit from a 429 envelope', async () => {
    const { fetchMock } = mockFetch([
      {
        status: 429,
        body: {
          status: 'error',
          message: 'too many requests',
          metadata: {
            status_code: 429,
            request_id: 'req_test',
            rate_limit: { limit: 5, remaining: 0, reset: 1760000000 }
          }
        }
      }
    ])
    const client = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock })

    const error = await catchError(
      client.auth.signInWithPassword({ identity: 'abbey', secret: 'x' })
    )

    expect(error.status).toBe(429)
    expect(error.rateLimit).toEqual({ limit: 5, remaining: 0, reset: 1760000000 })
  })

  it('passes through the envelope message on 401', async () => {
    const { fetchMock } = mockFetch([errorEnvelope(401, 'authentication required')])
    const client = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock })

    const error = await catchError(client.auth.getSession())

    expect(error.message).toBe('authentication required')
    expect(error.status).toBe(401)
    expect(error.fieldErrors).toEqual([])
  })

  it('falls back to api_error for non-envelope error bodies', async () => {
    const { fetchMock } = mockFetch([{ status: 500, body: 'oops' }])
    const client = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock })

    const error = await catchError(client.system.versionLatest())

    expect(error.code).toBe('api_error')
    expect(error.status).toBe(500)
  })

  it('classifies network failures as network_error', async () => {
    const fetchMock = vi.fn(async () => {
      throw new TypeError('fetch failed')
    }) as unknown as typeof globalThis.fetch
    const client = createApiClient({ baseUrl: BASE_URL, fetch: fetchMock })

    const error = await catchError(client.system.health())

    expect(error.code).toBe('network_error')
    expect(error.status).toBeUndefined()
  })

  it('classifies aborted requests as aborted', async () => {
    const abortingFetch = vi.fn(async () => {
      const error = new TypeError('The operation was aborted')
      error.name = 'AbortError'
      throw error
    }) as unknown as typeof globalThis.fetch
    const client = createApiClient({ baseUrl: BASE_URL, fetch: abortingFetch })

    const error = await catchError(client.system.health())

    expect(error.code).toBe('aborted')
  })

  it('classifies non-error throws as unknown', () => {
    expect(toApiClientError('boom').code).toBe('unknown')
  })
})
