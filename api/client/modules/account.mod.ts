// Self-service account surface: profile, credentials, sessions, email
// verification, and profile picture.

import type { SessionView } from '../schemas/session.schema'
import type { ChangePasswordParams, UpdateProfileParams, User } from '../schemas/user.schema'
import type { Executor } from '../types'

export interface AccountModule {
  getProfile(): Promise<User>
  updateProfile(patch: UpdateProfileParams): Promise<User>
  changePassword(params: ChangePasswordParams): Promise<void>
  listSessions(): Promise<SessionView[]>
  revokeSession(sessionId: string): Promise<void>
  /** Queues the verification email; the token travels by email only (204). */
  sendEmailVerification(): Promise<void>
  /** Confirms the address with the emailed token. */
  verifyEmail(token: string): Promise<void>
  /** Uploads the signed-in user's picture as the multipart `file` field. */
  updateProfilePicture(file: Blob, filename?: string): Promise<void>
  removeProfilePicture(): Promise<void>
}

export function createAccountModule(exec: Executor): AccountModule {
  return {
    getProfile: () => exec.get<User>('/account').then((r) => r.data),
    updateProfile: (patch) => exec.patch<User>('/account', patch).then((r) => r.data),
    changePassword: (params) => exec.put('/account/password', params).then(() => undefined),
    listSessions: () => exec.get<SessionView[]>('/account/sessions').then((r) => r.data),
    revokeSession: (sessionId) =>
      exec.delete(`/account/sessions/${sessionId}`).then(() => undefined),
    sendEmailVerification: () =>
      exec.post('/users/me/send-email-verification').then(() => undefined),
    verifyEmail: (token) => exec.post('/users/me/verify-email', { token }).then(() => undefined),
    updateProfilePicture: (file, filename) => {
      const form = new FormData()
      form.append('file', file, filename)
      return exec.put('/users/me/profile-picture', form).then(() => undefined)
    },
    removeProfilePicture: () => exec.delete('/users/me/profile-picture').then(() => undefined)
  }
}
