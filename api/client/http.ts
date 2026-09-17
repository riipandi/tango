// Transport layer: one ofetch instance, envelope unwrapping, and the
// executor every SDK module builds on.

import { ofetch, type FetchOptions } from 'ofetch'
import { isEnvelope, metadataOf } from './envelope'
import { ApiClientError, toApiClientError } from './error'
import type { CallInit, CallResult, Executor, HttpMethod } from './types'

/** Executor plus runtime credential rotation for the composition root. */
export interface HttpTransport extends Executor {
  /** Sets (or clears with `undefined`) the X-API-KEY machine credential. */
  setApiKey(value: string | undefined): void
}

/** Every endpoint mounts under this prefix on the server. */
const API_PREFIX = '/api'

export interface HttpOptions {
  baseUrl?: string
  headers?: Record<string, string>
  credentials?: RequestCredentials
  timeout?: number
  fetch?: typeof globalThis.fetch
  /** Machine credential sent as X-API-KEY on every request. */
  apiKey?: string
}

/** Header the server reads for machine credentials (`RequireAPIKey`). */
const API_KEY_HEADER = 'X-API-KEY'

interface RawResponse {
  status: number
  headers: Headers
  _data?: unknown
}

function unwrap<T>(response: RawResponse): CallResult<T> {
  const body = response._data
  if (isEnvelope(body)) {
    if (body.status === 'error') throw ApiClientError.fromEnvelope(body, response.status)
    return {
      data: body.data as T,
      metadata: metadataOf(body.metadata, response.status),
      links: body.links
    }
  }

  // Bare document (WebAuthn begin payloads, JWKS, discovery): no envelope.
  return {
    data: body as T,
    metadata: {
      statusCode: response.status,
      requestId: response.headers.get('x-request-id') ?? undefined
    }
  }
}

/**
 * Builds the transport executor. Failures surface as ApiClientError with no
 * transport-level retries — retry policy belongs to the caller (e.g. TanStack
 * Query).
 */
export function createHttp(options: HttpOptions = {}): HttpTransport {
  const baseUrl = options.baseUrl ?? ''

  // Mutable credential: setApiKey rotates the header without rebuilding.
  let apiKey = options.apiKey

  // `fetch` is a global option in ofetch: it must arrive via the second
  // create() argument, not the request defaults, or it is ignored.
  const http = ofetch.create(
    {
      baseURL: baseUrl,
      credentials: options.credentials ?? 'include',
      retry: 0,
      headers: options.headers,
      ...(options.timeout !== undefined && { timeout: options.timeout })
    },
    options.fetch !== undefined ? { fetch: options.fetch } : {}
  )

  async function request<T>(
    method: HttpMethod,
    path: string,
    init: CallInit = {}
  ): Promise<CallResult<T>> {
    let response: RawResponse
    try {
      response = await http.raw<unknown>(`${API_PREFIX}${path}`, {
        method,
        body: init.body as FetchOptions['body'],
        query: init.query,
        headers: {
          ...(apiKey !== undefined && { [API_KEY_HEADER]: apiKey }),
          ...init.headers
        },
        signal: init.signal
      })
    } catch (cause) {
      throw toApiClientError(cause)
    }
    return unwrap<T>(response)
  }

  return {
    base: baseUrl,
    url: (path) => `${baseUrl}${API_PREFIX}${path}`,
    setApiKey: (value) => {
      apiKey = value
    },
    request: (method, path, init) => request(method, path, init),
    get: (path, opts) => request('GET', path, { ...opts }),
    post: (path, body, opts) => request('POST', path, { ...opts, body }),
    put: (path, body, opts) => request('PUT', path, { ...opts, body }),
    patch: (path, body, opts) => request('PATCH', path, { ...opts, body }),
    delete: (path, opts) => request('DELETE', path, { ...opts })
  }
}
