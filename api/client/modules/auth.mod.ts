import { ApiClientError } from '../error'
import type { Executor } from '../types'
import type {
  ForgotPasswordParams,
  ResetPasswordParams,
  SignInParams,
  SignInResult,
  SignUpParams
} from '../schemas/session.schema'
import type { User } from '../schemas/user.schema'
import type { WebAuthnBeginResult, WebAuthnCredential } from '../schemas/webauthn.schema'

export interface TotpEnrollResult {
  secret: string
  provisioning_uri: string
}

export interface TotpStatus {
  confirmed: boolean
  recovery_codes_remaining: number
}

export interface MfaTotpModule {
  /** Verifies the code pending from a password sign-in; rotates the session cookie. */
  verify(params: { code: string }): Promise<User>
  enroll(): Promise<TotpEnrollResult>
  confirm(params: { code: string }): Promise<{ recovery_codes: string[] }>
  status(): Promise<TotpStatus>
  rotateRecoveryCodes(): Promise<{ recovery_codes: string[] }>
  disable(): Promise<void>
}

export interface WebAuthnModule {
  beginLogin(): Promise<WebAuthnBeginResult>
  finishLogin(assertion: Record<string, unknown>, sessionId: string): Promise<User>
  beginRegistration(): Promise<WebAuthnBeginResult>
  finishRegistration(attestation: Record<string, unknown>, sessionId: string): Promise<WebAuthnCredential>
}

export interface AuthModule {
  signInWithPassword(params: SignInParams): Promise<SignInResult>
  getSession(): Promise<SignInResult>
  signOut(): Promise<void>
  forgotPassword(params: ForgotPasswordParams): Promise<void>
  resetPassword(params: ResetPasswordParams): Promise<void>
  signUp(params: SignUpParams): Promise<User>
  /** True while the initial-admin setup can still run (204), false once any user exists (404). */
  setupAvailable(): Promise<boolean>
  setupAccount(params: SignUpParams): Promise<User>
  mfa: { totp: MfaTotpModule }
  webauthn: WebAuthnModule
}

export function createAuthModule(exec: Executor): AuthModule {
  const mfa: MfaTotpModule = {
    verify: (params) => exec.post<User>('/api/mfa/totp/verify', params).then((r) => r.data),
    enroll: () => exec.post<TotpEnrollResult>('/api/mfa/totp/enroll').then((r) => r.data),
    confirm: (params) =>
      exec.post<{ recovery_codes: string[] }>('/api/mfa/totp/confirm', params).then((r) => r.data),
    status: () => exec.get<TotpStatus>('/api/mfa/totp/status').then((r) => r.data),
    rotateRecoveryCodes: () =>
      exec.post<{ recovery_codes: string[] }>('/api/mfa/totp/recovery-codes').then((r) => r.data),
    disable: () => exec.delete('/api/mfa/totp').then(() => undefined)
  }

  const webauthn: WebAuthnModule = {
    beginLogin: () => exec.post<WebAuthnBeginResult>('/api/webauthn/login/begin').then((r) => r.data),
    finishLogin: (assertion, sessionId) =>
      exec
        .post<User>('/api/webauthn/login/finish', assertion, { query: { session_id: sessionId } })
        .then((r) => r.data),
    beginRegistration: () =>
      exec.post<WebAuthnBeginResult>('/api/webauthn/register/begin').then((r) => r.data),
    finishRegistration: (attestation, sessionId) =>
      exec
        .post<WebAuthnCredential>('/api/webauthn/register/finish', attestation, {
          query: { session_id: sessionId }
        })
        .then((r) => r.data)
  }

  return {
    signInWithPassword: (params) => exec.post<SignInResult>('/api/auth/sign-in', params).then((r) => r.data),
    getSession: () => exec.get<SignInResult>('/api/auth/session').then((r) => r.data),
    signOut: () => exec.post('/api/auth/sign-out').then(() => undefined),
    forgotPassword: (params) => exec.post('/api/auth/forgot-password', params).then(() => undefined),
    resetPassword: (params) => exec.post('/api/auth/reset-password', params).then(() => undefined),
    signUp: (params) => exec.post<User>('/api/signup', params).then((r) => r.data),
    setupAccount: (params) => exec.post<User>('/api/signup/setup', params).then((r) => r.data),
    setupAvailable: async () => {
      try {
        await exec.get('/api/signup/setup')
        return true
      } catch (error) {
        if (error instanceof ApiClientError && error.status === 404) return false
        throw error
      }
    },
    mfa: { totp: mfa },
    webauthn
  }
}
