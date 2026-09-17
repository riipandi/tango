import { toPaginated } from '../pagination'
import type { CallOptions, Executor, Paginated } from '../types'
import type { AdminUpdateUserParams, CreateUserParams, User } from '../schemas/user.schema'
import type { UserGroup } from '../schemas/usergroup.schema'

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
}

export function createUsersModule(exec: Executor): UsersModule {
  return {
    list: (params) => {
      const options: CallOptions | undefined = params
        ? { query: { query: params.query, page: params.page, limit: params.limit } }
        : undefined
      return exec.get<User[]>('/api/users', options).then(toPaginated)
    },
    get: (userId) => exec.get<User>(`/api/users/${userId}`).then((r) => r.data),
    create: (params) => exec.post<User>('/api/users', params).then((r) => r.data),
    update: (userId, patch) => exec.put<User>(`/api/users/${userId}`, patch).then((r) => r.data),
    remove: (userId) => exec.delete(`/api/users/${userId}`).then(() => undefined),
    listGroups: (userId) => exec.get<UserGroup[]>(`/api/users/${userId}/groups`).then((r) => r.data),
    setGroups: (userId, groupIds) =>
      exec.put(`/api/users/${userId}/user-groups`, groupIds).then(() => undefined),
    updateProfilePicture: (userId, file, filename) => {
      const form = new FormData()
      form.append('file', file, filename)
      return exec.put(`/api/users/${userId}/profile-picture`, form).then(() => undefined)
    },
    removeProfilePicture: (userId) =>
      exec.delete(`/api/users/${userId}/profile-picture`).then(() => undefined),
    profilePictureUrl: (userId) => `${exec.base}/api/users/${userId}/profile-picture.png`
  }
}
