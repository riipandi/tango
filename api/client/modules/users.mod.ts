// Admin accounts core: user CRUD, group membership, and profile pictures.

import { toPaginated } from '../pagination'
import type { AdminUpdateUserParams, CreateUserParams, User } from '../schemas/user.schema'
import type { UserGroup } from '../schemas/usergroup.schema'
import type { WebAuthnCredential } from '../schemas/webauthn.schema'
import type { CallOptions, Executor, Paginated } from '../types'

export interface UserListParams {
  query?: string
  page?: number
  limit?: number
}

export interface UsersModule {
  list(params?: UserListParams): Promise<Paginated<User>>
  get(userId: string): Promise<User>
  create(params: CreateUserParams): Promise<User>
  update(userId: string, patch: AdminUpdateUserParams): Promise<User>
  remove(userId: string): Promise<void>
  listGroups(userId: string): Promise<UserGroup[]>
  setGroups(userId: string, groupIds: string[]): Promise<void>
  /** Uploads the picture as the multipart `file` field. */
  updateProfilePicture(userId: string, file: Blob, filename?: string): Promise<void>
  removeProfilePicture(userId: string): Promise<void>
  /** Direct URL of the served PNG (no request is made). */
  profilePictureUrl(userId: string): string
  /** Passkeys registered by this user. */
  listCredentials(userId: string): Promise<WebAuthnCredential[]>
  renameCredential(userId: string, credentialId: string, name: string): Promise<WebAuthnCredential>
  removeCredential(userId: string, credentialId: string): Promise<void>
  /** Issues a one-time access token for the user; plaintext shown once. */
  issueOneTimeToken(userId: string): Promise<{ token: string }>
  /** Queues a one-time access email for the user (204). */
  sendOneTimeEmail(userId: string): Promise<void>
}

export function createUsersModule(exec: Executor): UsersModule {
  return {
    list: (params) => {
      const options: CallOptions | undefined = params
        ? { query: { query: params.query, page: params.page, limit: params.limit } }
        : undefined
      return exec.get<User[]>('/users', options).then(toPaginated)
    },
    get: (userId) => exec.get<User>(`/users/${userId}`).then((r) => r.data),
    create: (params) => exec.post<User>('/users', params).then((r) => r.data),
    update: (userId, patch) => exec.put<User>(`/users/${userId}`, patch).then((r) => r.data),
    remove: (userId) => exec.delete(`/users/${userId}`).then(() => undefined),
    listGroups: (userId) => exec.get<UserGroup[]>(`/users/${userId}/groups`).then((r) => r.data),
    setGroups: (userId, groupIds) =>
      exec.put(`/users/${userId}/user-groups`, groupIds).then(() => undefined),
    updateProfilePicture: (userId, file, filename) => {
      const form = new FormData()
      form.append('file', file, filename)
      return exec.put(`/users/${userId}/profile-picture`, form).then(() => undefined)
    },
    removeProfilePicture: (userId) =>
      exec.delete(`/users/${userId}/profile-picture`).then(() => undefined),
    profilePictureUrl: (userId) => exec.url(`/users/${userId}/profile-picture.png`),
    listCredentials: (userId) =>
      exec.get<WebAuthnCredential[]>(`/users/${userId}/webauthn-credentials`).then((r) => r.data),
    renameCredential: (userId, credentialId, name) =>
      exec
        .put<WebAuthnCredential>(`/users/${userId}/webauthn-credentials/${credentialId}`, { name })
        .then((r) => r.data),
    removeCredential: (userId, credentialId) =>
      exec.delete(`/users/${userId}/webauthn-credentials/${credentialId}`).then(() => undefined),
    issueOneTimeToken: (userId) =>
      exec.post<{ token: string }>(`/users/${userId}/one-time-access-token`).then((r) => r.data),
    sendOneTimeEmail: (userId) =>
      exec.post(`/users/${userId}/one-time-access-email`).then(() => undefined)
  }
}
