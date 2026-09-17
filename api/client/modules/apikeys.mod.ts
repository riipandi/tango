// Machine credentials sent as X-API-KEY: create (token shown once),
// renew (only expired keys), and revoke.

import { toPaginated } from '../pagination'
import type {
  APIKey,
  APIKeySecret,
  CreateAPIKeyParams,
  RenewAPIKeyParams
} from '../schemas/apikey.schema'
import type { Executor, Paginated } from '../types'

export interface APIKeysModule {
  list(): Promise<Paginated<APIKey>>
  create(params: CreateAPIKeyParams): Promise<APIKeySecret>
  renew(keyId: string, params: RenewAPIKeyParams): Promise<APIKeySecret>
  revoke(keyId: string): Promise<void>
}

export function createAPIKeysModule(exec: Executor): APIKeysModule {
  return {
    list: () => exec.get<APIKey[]>('/api-keys').then(toPaginated),
    create: (params) => exec.post<APIKeySecret>('/api-keys', params).then((r) => r.data),
    renew: (keyId, params) =>
      exec.post<APIKeySecret>(`/api-keys/${keyId}/renew`, params).then((r) => r.data),
    revoke: (keyId) => exec.delete(`/api-keys/${keyId}`).then(() => undefined)
  }
}
