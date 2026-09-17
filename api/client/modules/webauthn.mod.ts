// WebAuthn ceremonies. Begin endpoints answer with a bare document — the
// raw options object plus a one-time ceremony session id that finish
// endpoints echo back via the session_id query parameter.

import type { User } from '../schemas/user.schema'
import type { WebAuthnBeginResult, WebAuthnCredential } from '../schemas/webauthn.schema'
import type { Executor } from '../types'

export interface WebAuthnModule {
  beginLogin(): Promise<WebAuthnBeginResult>
  finishLogin(assertion: Record<string, unknown>, sessionId: string): Promise<User>
  beginRegistration(): Promise<WebAuthnBeginResult>
  finishRegistration(
    attestation: Record<string, unknown>,
    sessionId: string
  ): Promise<WebAuthnCredential>
}

export function createWebAuthnModule(exec: Executor): WebAuthnModule {
  return {
    beginLogin: () => exec.post<WebAuthnBeginResult>('/webauthn/login/begin').then((r) => r.data),
    finishLogin: (assertion, sessionId) =>
      exec
        .post<User>('/webauthn/login/finish', assertion, { query: { session_id: sessionId } })
        .then((r) => r.data),
    beginRegistration: () =>
      exec.post<WebAuthnBeginResult>('/webauthn/register/begin').then((r) => r.data),
    finishRegistration: (attestation, sessionId) =>
      exec
        .post<WebAuthnCredential>('/webauthn/register/finish', attestation, {
          query: { session_id: sessionId }
        })
        .then((r) => r.data)
  }
}
