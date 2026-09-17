import type { Executor } from '../types'
import type {
  ChangePasswordParams,
  UpdateProfileParams,
  User
} from '../schemas/user.schema'
import type { SessionView } from '../schemas/session.schema'

export interface AccountModule {
  getProfile(): Promise<User>
  updateProfile(patch: UpdateProfileParams): Promise<User>
  changePassword(params: ChangePasswordParams): Promise<void>
  listSessions(): Promise<SessionView[]>
  revokeSession(sessionId: string): Promise<void>
}

export function createAccountModule(exec: Executor): AccountModule {
  return {
    getProfile: () => exec.get<User>('/api/account').then((r) => r.data),
    updateProfile: (patch) => exec.patch<User>('/api/account', patch).then((r) => r.data),
    changePassword: (params) => exec.put('/api/account/password', params).then(() => undefined),
    listSessions: () => exec.get<SessionView[]>('/api/account/sessions').then((r) => r.data),
    revokeSession: (sessionId) =>
      exec.delete(`/api/account/sessions/${sessionId}`).then(() => undefined)
  }
}
