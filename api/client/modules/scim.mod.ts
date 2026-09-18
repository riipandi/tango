// SCIM service providers: registration, updates, and sync triggers.

import { ApiClientError } from '../error'
import type { CreateScimProviderParams, ScimProvider } from '../schemas/scim.schema'
import type { Executor } from '../types'

export interface ScimModule {
  list(): Promise<ScimProvider[]>
  getByClient(oidcClientId: string): Promise<ScimProvider | null>
  create(params: CreateScimProviderParams): Promise<ScimProvider>
  update(providerId: string, params: CreateScimProviderParams): Promise<ScimProvider>
  remove(providerId: string): Promise<void>
  /** Enqueues a provisioning sync for one provider. */
  sync(providerId: string): Promise<void>
}

export function createScimModule(exec: Executor): ScimModule {
  return {
    list: () => exec.get<ScimProvider[]>('/scim/service-provider').then((r) => r.data),
    getByClient: (oidcClientId) =>
      exec
        .get<ScimProvider>(`/oidc/clients/${oidcClientId}/scim-service-provider`)
        .then((r) => r.data)
        .catch((error: unknown) => {
          if (isNotFound(error)) return null
          throw error
        }),
    create: (params) =>
      exec.post<ScimProvider>('/scim/service-provider', params).then((r) => r.data),
    update: (providerId, params) =>
      exec.put<ScimProvider>(`/scim/service-provider/${providerId}`, params).then((r) => r.data),
    remove: (providerId) =>
      exec.delete(`/scim/service-provider/${providerId}`).then(() => undefined),
    sync: (providerId) =>
      exec.post(`/scim/service-provider/${providerId}/sync`).then(() => undefined)
  }
}

function isNotFound(error: unknown): boolean {
  return error instanceof ApiClientError && error.status === 404
}
