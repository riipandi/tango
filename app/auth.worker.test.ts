// Auth worker contract tests: the module runs as plain TypeScript
// under vitest (the comlink plugin only adds worker wiring at build
// time). Fetch is mocked so the cookie bridge is exercised without a
// server.
import { afterEach, beforeEach, describe, expect, vi, it } from 'vitest'
import * as worker from './auth.worker'

const { AuthError } = worker

// bridgeBody renders the Go tokenBridgeResponse shape.
function bridgeBody(accessToken: string, expiresInSeconds = 600): string {
  return JSON.stringify({
    access_token: accessToken,
    token_type: 'Bearer',
    expires_in: expiresInSeconds,
    expires_at: new Date(Date.now() + expiresInSeconds * 1000).toISOString()
  })
}

// One shared fetch spy for the whole file: re-implications reset the
// call history so each test counts its own round-trips.
let fetchSpy: ReturnType<typeof vi.spyOn>

function mockFetch(handler: (url: string, init?: RequestInit) => Response | Promise<Response>) {
  fetchSpy.mockReset()
  fetchSpy.mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url =
      typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url
    return handler(url, init)
  })
  return fetchSpy
}

beforeEach(() => {
  fetchSpy = vi.spyOn(globalThis, 'fetch').mockName('fetch')
})

afterEach(() => {
  fetchSpy.mockRestore()
})

describe('auth worker', () => {
  it('bootstraps from the cookie bridge and caches the token', async () => {
    const fetchMock = mockFetch((url) => {
      expect(url).toBe('/api/auth/token')
      return new Response(bridgeBody('tok-1'), { status: 200 })
    })

    const snapshot = await worker.bootstrap()
    expect(snapshot).not.toBeNull()
    expect(snapshot?.accessToken).toBe('tok-1')
    expect(fetchMock.mock.calls[0]?.[1]).toMatchObject({ method: 'POST', credentials: 'include' })

    // getAccessToken serves the cached token without a round-trip.
    await expect(worker.getAccessToken()).resolves.toBe('tok-1')
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('resolves bootstrap to null when the session is gone', async () => {
    mockFetch(() => new Response('{"code":"unauthenticated"}', { status: 401 }))
    await expect(worker.bootstrap()).resolves.toBeNull()
    await expect(worker.getAccessToken()).resolves.toBeNull()
  })

  it('refreshes through the bridge when the cached token passes its window', async () => {
    mockFetch(() => new Response(bridgeBody('tok-short', 1), { status: 200 }))
    await worker.bootstrap()

    // The 1s token is inside the 5s safety window → immediate
    // refresh on the next request.
    mockFetch(() => new Response(bridgeBody('tok-2'), { status: 200 }))
    await expect(worker.getAccessToken()).resolves.toBe('tok-2')
  })

  it('throws typed AuthError when the bridge is broken and nothing is cached', async () => {
    mockFetch(() => new Response('{"code":"unauthenticated"}', { status: 401 }))
    await expect(worker.bootstrap()).resolves.toBeNull()

    mockFetch(() => new Response('boom', { status: 500 }))
    await expect(worker.refresh()).rejects.toMatchObject({
      name: 'AuthError',
      status: 500
    })
    await expect(worker.getAccessToken()).rejects.toBeInstanceOf(AuthError)
  })

  it('signs out over Connect with bearer injection and clears memory', async () => {
    mockFetch(() => new Response(bridgeBody('tok-3'), { status: 200 }))
    await worker.bootstrap()

    const fetchMock = mockFetch((url, init) => {
      expect(url).toBe('/rpc/tango.identity.v1.AuthService/SignOut')
      expect(init?.headers).toMatchObject({
        authorization: 'Bearer tok-3',
        'connect-protocol-version': '1'
      })
      return new Response('{"status":"ok"}', { status: 200 })
    })

    await worker.signOut()
    expect(fetchMock).toHaveBeenCalledTimes(1)

    // Memory is cleared; the next token request re-bootstraps.
    mockFetch(() => new Response('{"code":"unauthenticated"}', { status: 401 }))
    await expect(worker.getAccessToken()).resolves.toBeNull()
  })

  it('falls back to the REST sign-out when the bearer died', async () => {
    mockFetch(() => new Response(bridgeBody('tok-4'), { status: 200 }))
    await worker.bootstrap()

    mockFetch((url) => {
      if (url.endsWith('/SignOut')) {
        return new Response('{"code":"unauthenticated"}', { status: 401 })
      }
      expect(url).toBe('/api/auth/sign-out')
      return new Response('{"signed_out":true}', { status: 200 })
    })

    await worker.signOut()
  })

  it('never exposes a refresh token in any payload it returns', async () => {
    mockFetch(() => new Response(bridgeBody('tok-5'), { status: 200 }))
    const snapshot = await worker.bootstrap()
    expect(Object.keys(snapshot ?? {})).toEqual(['accessToken', 'expiresAtMs'])
  })
})
