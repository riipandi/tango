// AuthWorkerApi — token lifecycle worker. The comlink plugin wraps
// these exports; the UI thread holds the access token only for the
// duration of one RPC header injection.
//
// There is no cookie channel: the session token is returned by
// sign-in and kept client-side, then posted to the same-origin
// /api/auth/token endpoint to mint a fresh access bearer. A "remember
// me" session lands in localStorage so it survives a reload; a plain
// session lives in sessionStorage and dies with the tab.
import { ofetch } from 'ofetch'

const tokenEndpoint = '/api/auth/token'
const signOutEndpoint = '/rpc/tango.identity.v1.AuthService/SignOut'

// storageKey is where the rotating session token lives.
const storageKey = 'tango.session_token'

export interface AuthSnapshot {
  accessToken: string
  expiresAtMs: number
}

interface BridgeResponse {
  access_token: string
  token_type: string
  expires_in: number
  expires_at: string
  session_token?: string
}

export class AuthError extends Error {
  readonly status: number

  constructor(message: string, status = 0) {
    super(message)
    this.name = 'AuthError'
    this.status = status
  }
}

const http = ofetch.create({ ignoreResponseError: true })

let accessToken: string | null = null
let expiresAtMs = 0

// readSessionToken returns the persisted session credential.
function readSessionToken(): string {
  return localStorage.getItem(storageKey) ?? sessionStorage.getItem(storageKey) ?? ''
}

// writeSessionToken persists the session credential for the lifetime
// the caller asked for.
function writeSessionToken(token: string, remember: boolean): void {
  clearSessionToken()
  if (remember) localStorage.setItem(storageKey, token)
  else sessionStorage.setItem(storageKey, token)
}

// clearSessionToken drops the persisted credential.
function clearSessionToken(): void {
  localStorage.removeItem(storageKey)
  sessionStorage.removeItem(storageKey)
}

// reset forgets the in-memory token; sign-out and fatal refresh
// errors land here.
function reset(): void {
  accessToken = null
  expiresAtMs = 0
}

// adoptTokens stores the session credential a sign-in response
// returned. The worker owns it from here on; the caller never keeps a
// copy. The access bearer is not cached — the first RPC mints one
// through the bridge, which also proves the credential works.
export function adoptTokens(sessionToken: string, remember: boolean): void {
  writeSessionToken(sessionToken, remember)
  reset()
}

// callBridge posts the persisted session token to the refresh
// endpoint; the answer carries a fresh access bearer and, when the
// session token rotated, its replacement.
async function callBridge(): Promise<AuthSnapshot> {
  const sessionToken = readSessionToken()
  if (!sessionToken) {
    reset()
    throw new AuthError('session expired', 401)
  }

  const response = await http.raw<BridgeResponse>(tokenEndpoint, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ session_token: sessionToken })
  })
  if (response.status === 401) {
    reset()
    clearSessionToken()
    throw new AuthError('session expired', 401)
  }
  if (!response.ok || !response._data || response._data.token_type !== 'Bearer') {
    throw new AuthError('token bridge returned an unexpected payload', response.status)
  }

  // The presented session token is dead after the call; keep the
  // replacement in the same store the old one lived in.
  if (response._data.session_token) {
    writeSessionToken(response._data.session_token, localStorage.getItem(storageKey) !== null)
  }
  accessToken = response._data.access_token
  expiresAtMs = Date.parse(response._data.expires_at)
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
  if (!readSessionToken()) return null
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

// signOut revokes the token family over Connect and clears both the
// cached bearer and the persisted session token. The family dies
// server-side, so a leaked copy of the session token stops resolving.
export async function signOut(): Promise<void> {
  try {
    const token = await getAccessToken()
    if (!token) return
    const response = await http.raw(signOutEndpoint, {
      method: 'POST',
      headers: {
        'connect-protocol-version': '1',
        authorization: `Bearer ${token}`
      }
    })
    if (!response.ok) {
      throw new AuthError(`sign out failed with ${response.status}`, response.status)
    }
  } finally {
    clearSessionToken()
    reset()
  }
}

// dispose releases the worker endpoint; the plugin terminates the
// underlying worker.
export async function dispose(): Promise<void> {
  reset()
}
