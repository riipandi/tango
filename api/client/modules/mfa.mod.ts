// TOTP multi-factor lifecycle: enrollment, confirmation, and recovery codes.

import type { User } from '../schemas/user.schema'
import type { Executor } from '../types'

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

export function createMfaTotpModule(exec: Executor): MfaTotpModule {
  return {
    verify: (params) => exec.post<User>('/mfa/totp/verify', params).then((r) => r.data),
    enroll: () => exec.post<TotpEnrollResult>('/mfa/totp/enroll').then((r) => r.data),
    confirm: (params) =>
      exec.post<{ recovery_codes: string[] }>('/mfa/totp/confirm', params).then((r) => r.data),
    status: () => exec.get<TotpStatus>('/mfa/totp/status').then((r) => r.data),
    rotateRecoveryCodes: () =>
      exec.post<{ recovery_codes: string[] }>('/mfa/totp/recovery-codes').then((r) => r.data),
    disable: () => exec.delete('/mfa/totp').then(() => undefined)
  }
}
