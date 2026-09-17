// API resource registry: resources, permission sets, client grants, and
// CIMD access. Resource-server side of the apiaccess module.

import { toPaginated } from '../pagination'
import type {
  API,
  ClientAPIGrant,
  ClientRef,
  GrantParams,
  Grant
} from '../schemas/apiaccess.schema'
import type { CallOptions, Executor, Paginated } from '../types'

export interface PermissionInput {
  key: string
  name: string
  description?: string
}

export interface ClientListParams {
  search?: string
  page?: number
  limit?: number
}

export interface ApisModule {
  list(): Promise<Paginated<API>>
  get(apiId: string): Promise<API>
  create(params: { name: string; resource: string }): Promise<API>
  update(apiId: string, params: { name: string }): Promise<API>
  remove(apiId: string): Promise<void>
  /** Replaces the whole permission list (POST semantics). */
  setPermissions(
    apiId: string,
    permissions: PermissionInput[]
  ): Promise<{ permissions: API['permissions'] }>
  /** Clients holding a grant on this API. */
  listClients(apiId: string, params?: ClientListParams): Promise<Paginated<ClientRef>>
  /** Clients without a grant yet. */
  listAssignableClients(apiId: string, params?: ClientListParams): Promise<Paginated<ClientRef>>
  /** Upserts one client's grant on this API. */
  setGrant(apiId: string, clientId: string, params: GrantParams): Promise<Grant>
  removeGrant(apiId: string, clientId: string): Promise<{ deleted: boolean }>
  /** Toggles CIMD client access with an optional permission subset. */
  setCimdAccess(
    apiId: string,
    params: { enabled: boolean; permission_ids?: string[] }
  ): Promise<API>
}

export function createApisModule(exec: Executor): ApisModule {
  const clientListOptions = (params?: ClientListParams): CallOptions | undefined =>
    params
      ? { query: { search: params.search, page: params.page, limit: params.limit } }
      : undefined

  return {
    list: () => exec.get<API[]>('/apis').then(toPaginated),
    get: (apiId) => exec.get<API>(`/apis/${apiId}`).then((r) => r.data),
    create: (params) => exec.post<API>('/apis', params).then((r) => r.data),
    update: (apiId, params) => exec.put<API>(`/apis/${apiId}`, params).then((r) => r.data),
    remove: (apiId) => exec.delete(`/apis/${apiId}`).then(() => undefined),
    setPermissions: (apiId, permissions) =>
      exec
        .put<{ permissions: API['permissions'] }>(`/apis/${apiId}/permissions`, { permissions })
        .then((r) => r.data),
    listClients: (apiId, params) =>
      exec.get<ClientRef[]>(`/apis/${apiId}/clients`, clientListOptions(params)).then(toPaginated),
    listAssignableClients: (apiId, params) =>
      exec
        .get<ClientRef[]>(`/apis/${apiId}/assignable-clients`, clientListOptions(params))
        .then(toPaginated),
    setGrant: (apiId, clientId, params) =>
      exec.put<Grant>(`/apis/${apiId}/clients/${clientId}`, params).then((r) => r.data),
    removeGrant: (apiId, clientId) =>
      exec.delete<{ deleted: boolean }>(`/apis/${apiId}/clients/${clientId}`).then((r) => r.data),
    setCimdAccess: (apiId, params) =>
      exec.put<API>(`/apis/${apiId}/cimd-access`, params).then((r) => r.data)
  }
}

// Re-exported for consumers building client-centric admin views.
export type { ClientAPIGrant }
