// Password, recovery, and signup endpoints; composes the MFA and WebAuthn
// ceremony namespaces.

import { ApiClientError } from '../error'
import type {
  ForgotPasswordParams,
  ResetPasswordParams,
  SignInParams,
  SignInResult,
  SignUpParams
} from '../schemas/session.schema'
import type { User } from '../schemas/user.schema'
import type { Executor } from '../types'
import { createMfaTotpModule, type MfaTotpModule } from './mfa.mod'
import { createWebAuthnModule, type WebAuthnModule } from './webauthn.mod'

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
  /** Requests a one-time access email (passwordless sign-in, 204). */
  requestOneTimeEmail(params: { email: string; redirect_path?: string }): Promise<void>
  /** Exchanges the emailed token for a session; answers with the user. */
  exchangeOneTimeToken(token: string): Promise<User>
  mfa: { totp: MfaTotpModule }
  webauthn: WebAuthnModule
}

export function createAuthModule(exec: Executor): AuthModule {
  return {
    signInWithPassword: (params) =>
      exec.post<SignInResult>('/auth/sign-in', params).then((r) => r.data),
    getSession: () => exec.get<SignInResult>('/auth/session').then((r) => r.data),
    signOut: () => exec.post('/auth/sign-out').then(() => undefined),
    forgotPassword: (params) => exec.post('/auth/forgot-password', params).then(() => undefined),
    resetPassword: (params) => exec.post('/auth/reset-password', params).then(() => undefined),
    signUp: (params) => exec.post<User>('/signup', params).then((r) => r.data),
    setupAccount: (params) => exec.post<User>('/signup/setup', params).then((r) => r.data),
    requestOneTimeEmail: (params) =>
      exec.post('/one-time-access-email', params).then(() => undefined),
    exchangeOneTimeToken: (token) =>
      exec.post<User>(`/one-time-access-token/${token}`).then((r) => r.data),
    setupAvailable: async () => {
      try {
        await exec.get('/signup/setup')
        return true
      } catch (error) {
        if (error instanceof ApiClientError && error.status === 404) return false
        throw error
      }
    },
    mfa: { totp: createMfaTotpModule(exec) },
    webauthn: createWebAuthnModule(exec)
  }
}
