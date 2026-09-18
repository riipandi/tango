// AuthWorkerApi — token lifecycle worker. The comlink plugin wraps
// these exports; the UI thread never sees a refresh token and holds
// an access token only for the duration of one RPC header injection.
//
// The cookie channel is the only bridge into this worker: bootstrap
// and refresh POST to the same-origin /api/auth/token endpoint with
// credentials: include, so the browser attaches the HttpOnly cookies
// and the worker keeps just the short-lived access token in memory.

const tokenEndpoint = '/api/auth/token'
const signOutEndpoint = '/rpc/tango.identity.v1.AuthService/SignOut'

export interface AuthSnapshot {
  accessToken: string
  expiresAtMs: number
}

interface BridgeResponse {
  access_token: string
  token_type: string
  expires_in: number
  expires_at: string
}

export class AuthError extends Error {
  readonly status: number

  constructor(message: string, status = 0) {
    super(message)
    this.name = 'AuthError'
    this.status = status
  }
}

let accessToken: string | null = null
let expiresAtMs = 0

// reset forgets the in-memory token; sign-out and fatal refresh
// errors land here.
function reset(): void {
  accessToken = null
  expiresAtMs = 0
}

// callBridge posts to the token endpoint; the browser attaches the
// HttpOnly cookies, the answer carries the access token only.
async function callBridge(): Promise<AuthSnapshot> {
  const response = await fetch(tokenEndpoint, {
    method: 'POST',
    credentials: 'include',
    headers: { 'content-type': 'application/json' }
  })
  if (response.status === 401) {
    reset()
    throw new AuthError('session expired', 401)
  }
  if (!response.ok) {
    throw new AuthError(`token bridge failed with ${response.status}`, response.status)
  }
  const body = (await response.json()) as BridgeResponse
  if (!body.access_token || body.token_type !== 'Bearer') {
    throw new AuthError('token bridge returned an unexpected payload', response.status)
  }
  accessToken = body.access_token
  expiresAtMs = Date.parse(body.expires_at)
  return { accessToken, expiresAtMs }
}

// loadFromBridge is the one-shot variant that leaves the cached
// token untouched on failure.
async function loadFromBridge(): Promise<string | null> {
  try {
    return (await callBridge()).accessToken
  } catch (error) {
    if (error instanceof AuthError && error.status === 401) return null
    throw error
  }
}

// bootstrap primes the worker after page load or worker restart; a
// dead session (401) resolves null instead of throwing.
export async function bootstrap(): Promise<AuthSnapshot | null> {
  reset()
  try {
    return await callBridge()
  } catch (error) {
    if (error instanceof AuthError && error.status === 401) return null
    throw error
  }
}

// getAccessToken serves the RPC interceptor: the fresh cached token
// or a silent refresh when the window passed. Expired sessions
// resolve null so callers can route to sign-in.
export async function getAccessToken(): Promise<string | null> {
  if (accessToken && Date.now() < expiresAtMs - 5_000) {
    return accessToken
  }
  return loadFromBridge()
}

// refresh forces a rotation round-trip; concurrent callers must
// serialize through their own dedupe (the UI keeps one worker).
export async function refresh(): Promise<AuthSnapshot> {
  return callBridge()
}

// signOut revokes the token family over Connect and clears worker
// memory; the response clears the HttpOnly cookies. An expired
// bearer falls back to the cookie channel so the family never
// survives a sign-out.
export async function signOut(): Promise<void> {
  try {
    const token = await getAccessToken()
    const headers: Record<string, string> = {
      'content-type': 'application/json',
      'connect-protocol-version': '1'
    }
    if (token) {
      headers.authorization = `Bearer ${token}`
    }
    const response = await fetch(signOutEndpoint, {
      method: 'POST',
      credentials: 'include',
      headers
    })
    if (response.ok) return
    if (response.status !== 401) {
      throw new AuthError(`sign out failed with ${response.status}`, response.status)
    }
    // Cookie fallback: the refresh cookie still identifies the
    // family even though the bearer died.
    const fallback = await fetch('/api/auth/sign-out', {
      method: 'POST',
      credentials: 'include'
    })
    if (!fallback.ok) {
      throw new AuthError(`sign out failed with ${fallback.status}`, fallback.status)
    }
  } finally {
    reset()
  }
}

// dispose releases the worker endpoint; the plugin terminates the
// underlying worker.
export async function dispose(): Promise<void> {
  reset()
}
