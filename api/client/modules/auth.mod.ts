// Password, recovery, and one-time exchange endpoints; composes the
// WebAuthn ceremony namespace.

import type {
  ForgotPasswordParams,
  ResetPasswordParams,
  SignInParams,
  SignInResult
} from '../schemas/session.schema'
import type { User } from '../schemas/user.schema'
import type { Executor } from '../types'
import { createWebAuthnModule, type WebAuthnModule } from './webauthn.mod'

export interface AuthModule {
  signInWithPassword(params: SignInParams): Promise<SignInResult>
  getSession(): Promise<SignInResult>
  signOut(): Promise<void>
  forgotPassword(params: ForgotPasswordParams): Promise<void>
  resetPassword(params: ResetPasswordParams): Promise<void>
  /** Exchanges the emailed one-time token for a session; answers with the user. */
  exchangeOneTimeToken(token: string): Promise<User>
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
    exchangeOneTimeToken: (token) =>
      exec.post<User>(`/one-time-access-token/${token}`).then((r) => r.data),
    webauthn: createWebAuthnModule(exec)
  }
}
