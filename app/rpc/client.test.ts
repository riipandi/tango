import { Code, ConnectError, createClient } from '@connectrpc/connect'
// Connect transport contract tests: bearer injection from the auth
// worker and the same-origin base URL are pinned without a live
// server by mocking fetch.
import { afterEach, beforeEach, describe, expect, vi, it } from 'vitest'
import { AuthService } from '../generated/rpc/identity_pb'
import { createRPCTransport } from './client'
import type { AuthWorker } from './client'

function fakeWorker(accessToken: string | null): AuthWorker {
  return { getAccessToken: async () => accessToken } as unknown as AuthWorker
}

let fetchSpy: ReturnType<typeof vi.spyOn>

beforeEach(() => {
  fetchSpy = vi.spyOn(globalThis, 'fetch').mockName('fetch')
})

afterEach(() => {
  fetchSpy.mockRestore()
})

// headerOf reads the authorization header from the fetch init; the
// transport hands the mock a Headers instance.
function headerOf(init?: RequestInit): string | null {
  const headers = init?.headers
  if (headers instanceof Headers) {
    return headers.get('authorization')
  }
  return (headers as Record<string, string> | undefined)?.authorization ?? null
}

describe('rpc transport', () => {
  it('injects the worker token as the Authorization header', async () => {
    fetchSpy.mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url =
        typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url
      expect(url).toContain('/rpc/tango.identity.v1.AuthService/GetSession')
      expect(headerOf(init)).toBe('Bearer tok-9')
      return new Response('{"user":null,"pending":false}', {
        status: 200,
        headers: { 'content-type': 'application/json' }
      })
    })

    const client = createClient(AuthService, createRPCTransport(fakeWorker('tok-9')))
    await client.getSession({})
    expect(fetchSpy).toHaveBeenCalledTimes(1)
  })

  it('sends no Authorization header for anonymous visitors', async () => {
    fetchSpy.mockImplementation(async (_input: RequestInfo | URL, _init?: RequestInit) => {
      return new Response('{"code":"unauthenticated"}', { status: 401 })
    })

    const client = createClient(AuthService, createRPCTransport(fakeWorker(null)))
    const error = await client.getSession({}).then(
      () => null,
      (e) => e
    )
    expect(error).toBeInstanceOf(ConnectError)
    expect((error as ConnectError).code).toBe(Code.Unauthenticated)
    expect(fetchSpy).toHaveBeenCalledTimes(1)
  })
})
