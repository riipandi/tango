// Client-centric grant views: which APIs a client may reach and which
// are still assignable.

import { toPaginated } from '../pagination'
import type { API, ClientAPIGrant } from '../schemas/apiaccess.schema'
import type { Executor, Paginated } from '../types'

export interface ApiAccessModule {
  /** Grants of one client, each with its API resource. */
  listForClient(clientId: string): Promise<ClientAPIGrant[]>
  /** APIs the client has no grant on yet (paginated). */
  listAssignableForClient(clientId: string): Promise<Paginated<API>>
}

export function createApiAccessModule(exec: Executor): ApiAccessModule {
  return {
    listForClient: (clientId) =>
      exec.get<ClientAPIGrant[]>(`/api-access/${clientId}/apis`).then((r) => r.data),
    listAssignableForClient: (clientId) =>
      exec.get<API[]>(`/api-access/${clientId}/assignable-apis`).then(toPaginated)
  }
}
