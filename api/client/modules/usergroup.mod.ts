import { toPaginated } from '../pagination'
import type {
  CreateUserGroupParams,
  UpdateUserGroupParams,
  UserGroup
} from '../schemas/usergroup.schema'
import type { CallOptions, Executor, Paginated } from '../types'

export interface UserGroupListParams {
  query?: string
  page?: number
  limit?: number
}

export interface UserGroupsModule {
  list(params?: UserGroupListParams): Promise<Paginated<UserGroup>>
  get(groupId: string): Promise<UserGroup>
  create(params: CreateUserGroupParams): Promise<UserGroup>
  update(groupId: string, patch: UpdateUserGroupParams): Promise<UserGroup>
  remove(groupId: string): Promise<void>
  listMembers(groupId: string): Promise<string[]>
  setMembers(groupId: string, userIds: string[]): Promise<void>
  setAllowedClients(groupId: string, clientIds: string[]): Promise<void>
}

export function createUserGroupsModule(exec: Executor): UserGroupsModule {
  return {
    list: (params) => {
      const options: CallOptions | undefined = params
        ? { query: { query: params.query, page: params.page, limit: params.limit } }
        : undefined
      return exec.get<UserGroup[]>('/api/user-groups', options).then(toPaginated)
    },
    get: (groupId) => exec.get<UserGroup>(`/api/user-groups/${groupId}`).then((r) => r.data),
    create: (params) => exec.post<UserGroup>('/api/user-groups', params).then((r) => r.data),
    update: (groupId, patch) =>
      exec.put<UserGroup>(`/api/user-groups/${groupId}`, patch).then((r) => r.data),
    remove: (groupId) => exec.delete(`/api/user-groups/${groupId}`).then(() => undefined),
    listMembers: (groupId) =>
      exec.get<string[]>(`/api/user-groups/${groupId}/users`).then((r) => r.data),
    setMembers: (groupId, userIds) =>
      exec.put(`/api/user-groups/${groupId}/users`, userIds).then(() => undefined),
    setAllowedClients: (groupId, clientIds) =>
      exec.put(`/api/user-groups/${groupId}/allowed-oidc-clients`, clientIds).then(() => undefined)
  }
}
