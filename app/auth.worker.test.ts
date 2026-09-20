// Auth worker contract tests: the module runs as plain TypeScript
// under vitest (the comlink plugin only adds worker wiring at build
// time). Fetch is mocked so the refresh channel is exercised without
// a server; jsdom supplies the Web Storage the worker persists to.
import { afterEach, beforeEach, describe, expect, vi, it } from 'vitest'
import * as worker from './auth.worker'

const { AuthError } = worker

const storageKey = 'tango.session_token'

// bridgeBody renders the Go tokenBridgeResponse shape.
function bridgeBody(accessToken: string, expiresInSeconds = 600, sessionToken?: string): Response {
  return new Response(
    JSON.stringify({
      access_token: accessToken,
      token_type: 'Bearer',
      expires_in: expiresInSeconds,
      expires_at: new Date(Date.now() + expiresInSeconds * 1000).toISOString(),
      ...(sessionToken ? { session_token: sessionToken } : {})
    }),
    { status: 200, headers: { 'content-type': 'application/json' } }
  )
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
  localStorage.clear()
  sessionStorage.clear()
})

afterEach(() => {
  fetchSpy.mockRestore()
  localStorage.clear()
  sessionStorage.clear()
})

describe('auth worker', () => {
  it('bootstraps from the persisted session token and caches the bearer', async () => {
    worker.adoptTokens('sess-1', true)
    const fetchMock = mockFetch((url, init) => {
      expect(url).toBe('/api/auth/token')
      expect(JSON.parse(init?.body as string)).toEqual({ session_token: 'sess-1' })
      return bridgeBody('tok-1')
    })

    const snapshot = await worker.bootstrap()
    expect(snapshot).not.toBeNull()
    expect(snapshot?.accessToken).toBe('tok-1')
    expect(fetchMock.mock.calls[0]?.[1]).toMatchObject({ method: 'POST' })

    // getAccessToken serves the cached token without a round-trip.
    await expect(worker.getAccessToken()).resolves.toBe('tok-1')
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('keeps a remembered session in localStorage and a plain one in sessionStorage', () => {
    worker.adoptTokens('sess-long', true)
    expect(localStorage.getItem(storageKey)).toBe('sess-long')
    expect(sessionStorage.getItem(storageKey)).toBeNull()

    worker.adoptTokens('sess-short', false)
    expect(sessionStorage.getItem(storageKey)).toBe('sess-short')
    expect(localStorage.getItem(storageKey)).toBeNull()
  })

  it('resolves bootstrap to null without a persisted token or a dead session', async () => {
    await expect(worker.bootstrap()).resolves.toBeNull()

    worker.adoptTokens('sess-dead', false)
    mockFetch(() => new Response('{"code":"unauthenticated"}', { status: 401 }))
    await expect(worker.bootstrap()).resolves.toBeNull()
    expect(sessionStorage.getItem(storageKey)).toBeNull()
    await expect(worker.getAccessToken()).resolves.toBeNull()
  })

  it('stores the rotated session token the bridge returns', async () => {
    worker.adoptTokens('sess-old', true)
    mockFetch(() => bridgeBody('tok-1', 600, 'sess-new'))

    await worker.bootstrap()
    expect(localStorage.getItem(storageKey)).toBe('sess-new')
  })

  it('refreshes through the bridge when the cached token passes its window', async () => {
    worker.adoptTokens('sess-2', false)
    mockFetch(() => bridgeBody('tok-short', 1))
    await worker.bootstrap()

    // The 1s token is inside the 5s safety window → immediate refresh.
    mockFetch(() => bridgeBody('tok-2'))
    await expect(worker.getAccessToken()).resolves.toBe('tok-2')
  })

  it('throws typed AuthError when the bridge is broken and nothing is cached', async () => {
    worker.adoptTokens('sess-3', false)
    mockFetch(() => new Response('{"code":"unauthenticated"}', { status: 401 }))
    await expect(worker.bootstrap()).resolves.toBeNull()

    // A fresh credential, then a server-side failure: the error
    // surfaces instead of being swallowed as an expired session.
    worker.adoptTokens('sess-3b', false)
    mockFetch(() => new Response('boom', { status: 500 }))
    await expect(worker.refresh()).rejects.toMatchObject({
      name: 'AuthError',
      status: 500
    })
    await expect(worker.getAccessToken()).rejects.toBeInstanceOf(AuthError)
  })

  it('signs out over Connect with bearer injection and clears the credential', async () => {
    worker.adoptTokens('sess-4', true)
    mockFetch(() => bridgeBody('tok-3'))
    await worker.bootstrap()

    const fetchMock = mockFetch((url, init) => {
      expect(url).toBe('/rpc/tango.identity.v1.AuthService/SignOut')
      const headers = new Headers(init?.headers)
      expect(headers.get('authorization')).toBe('Bearer tok-3')
      expect(headers.get('connect-protocol-version')).toBe('1')
      return new Response('{"status":"ok"}', {
        status: 200,
        headers: { 'content-type': 'application/json' }
      })
    })

    await worker.signOut()
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(localStorage.getItem(storageKey)).toBeNull()

    // Memory is cleared; the next token request finds no credential.
    await expect(worker.getAccessToken()).resolves.toBeNull()
  })

  it('never exposes the session token in the snapshots it returns', async () => {
    worker.adoptTokens('sess-5', false)
    mockFetch(() => bridgeBody('tok-5'))
    const snapshot = await worker.bootstrap()
    expect(Object.keys(snapshot ?? {})).toEqual(['accessToken', 'expiresAtMs'])
  })
})
