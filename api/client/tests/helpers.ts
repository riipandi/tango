import { vi } from 'vitest'

export interface RecordedRequest {
  url: string
  path: string
  method: string
  headers: Headers
  body?: string
  formData?: FormData
  signal?: AbortSignal
  credentials: RequestCredentials
  query: URLSearchParams
}

export interface QueuedResponse {
  status: number
  body?: unknown
  headers?: Record<string, string>
}

/**
 * Fakes globalThis-style fetch: records each Request and plays queued
 * responses in order. Bodies are JSON-encoded unless omitted (204-style).
 */
export function mockFetch(responses: QueuedResponse[]) {
  const calls: RecordedRequest[] = []
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const request = new Request(input, init)
    calls.push({
      url: request.url,
      path: new URL(request.url).pathname,
      method: request.method,
      headers: request.headers,
      body: typeof init?.body === 'string' ? init.body : undefined,
      formData: init?.body instanceof FormData ? init.body : undefined,
      signal: init?.signal ?? undefined,
      credentials: request.credentials,
      query: new URL(request.url).searchParams
    })
    const next = responses.shift()
    if (!next) throw new Error('unexpected extra request')
    // Null-body statuses reject any body; sending one throws TypeError.
    const nullBody = next.status === 204 || next.status === 205 || next.status === 304
    return new Response(nullBody ? null : next.body === undefined ? null : JSON.stringify(next.body), {
      status: next.status,
      headers: { 'content-type': 'application/json', ...next.headers }
    })
  })
  return { fetchMock: fetchMock as unknown as typeof globalThis.fetch, calls }
}

/** Throws when the call index does not exist — keeps assertions index-safe. */
export function expectCall(calls: RecordedRequest[], index = 0): RecordedRequest {
  const call = calls[index]
  if (!call) throw new Error(`expected request #${index}, got ${calls.length} call(s)`)
  return call
}

export function envelope(data: unknown, metadata: Record<string, unknown> = {}, status = 200) {
  return {
    status,
    body: {
      status: 'success',
      data,
      metadata: { status_code: status, request_id: 'req_test', ...metadata }
    }
  }
}

export function errorEnvelope(status: number, message: string, error?: unknown) {
  return {
    status,
    body: {
      status: 'error',
      message,
      ...(error !== undefined && { error }),
      metadata: { status_code: status, request_id: 'req_test' }
    }
  }
}
