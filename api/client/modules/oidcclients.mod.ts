// Admin relying-party client registry: CRUD, secrets, logo, group
// allowlist, token preview, and metadata refresh.

import type {
  OidcClient,
  OidcClientMeta,
  OidcClientParams,
  OidcSecretEntry,
  OidcTokenPreview
} from '../schemas/oidcclient.schema'
import type { Executor } from '../types'

export interface OidcClientsModule {
  list(): Promise<OidcClient[]>
  get(clientId: string): Promise<OidcClient>
  create(params: OidcClientParams): Promise<OidcClient & { client_secret: string }>
  update(clientId: string, params: OidcClientParams): Promise<OidcClient>
  remove(clientId: string): Promise<void>
  /** Replaces the group allowlist of a group-restricted client. */
  setAllowedGroups(clientId: string, groupIds: string[]): Promise<OidcClient>
  meta(clientId: string): Promise<OidcClientMeta>
  /** Preview of the tokens a user would receive (debug surface). */
  preview(clientId: string, userId: string, scopes?: string): Promise<OidcTokenPreview>
  listSecrets(clientId: string): Promise<OidcSecretEntry[]>
  /** Creates a client secret; the plaintext appears exactly once. */
  createSecret(clientId: string): Promise<OidcSecretEntry & { secret: string }>
  removeSecret(clientId: string, secretId: string): Promise<void>
  /** Uploads the logo as the multipart `file` field. */
  updateLogo(clientId: string, file: Blob, filename?: string): Promise<void>
  removeLogo(clientId: string): Promise<void>
  /** Re-fetches CIMD metadata for a CIMD client. */
  refresh(clientId: string): Promise<OidcClient>
}

export function createOidcClientsModule(exec: Executor): OidcClientsModule {
  return {
    list: () => exec.get<OidcClient[]>('/oidc/clients').then((r) => r.data),
    get: (clientId) => exec.get<OidcClient>(`/oidc/clients/${clientId}`).then((r) => r.data),
    create: (params) =>
      exec
        .post<OidcClient & { client_secret: string }>('/oidc/clients', params)
        .then((r) => r.data),
    update: (clientId, params) =>
      exec.put<OidcClient>(`/oidc/clients/${clientId}`, params).then((r) => r.data),
    remove: (clientId) => exec.delete(`/oidc/clients/${clientId}`).then(() => undefined),
    setAllowedGroups: (clientId, groupIds) =>
      exec
        .put<OidcClient>(`/oidc/clients/${clientId}/allowed-user-groups`, {
          user_group_ids: groupIds
        })
        .then((r) => r.data),
    meta: (clientId) =>
      exec.get<OidcClientMeta>(`/oidc/clients/${clientId}/meta`).then((r) => r.data),
    preview: (clientId, userId, scopes) =>
      exec
        .get<OidcTokenPreview>(`/oidc/clients/${clientId}/preview/${userId}`, {
          query: { scopes: scopes ?? undefined }
        })
        .then((r) => r.data),
    listSecrets: (clientId) =>
      exec.get<OidcSecretEntry[]>(`/oidc/clients/${clientId}/secrets`).then((r) => r.data),
    createSecret: (clientId) =>
      exec
        .post<OidcSecretEntry & { secret: string }>(`/oidc/clients/${clientId}/secrets`)
        .then((r) => r.data),
    removeSecret: (clientId, secretId) =>
      exec.delete(`/oidc/clients/${clientId}/secrets/${secretId}`).then(() => undefined),
    updateLogo: (clientId, file, filename) => {
      const form = new FormData()
      form.append('file', file, filename)
      return exec.post(`/oidc/clients/${clientId}/logo`, form).then(() => undefined)
    },
    removeLogo: (clientId) => exec.delete(`/oidc/clients/${clientId}/logo`).then(() => undefined),
    refresh: (clientId) =>
      exec.post<OidcClient>(`/oidc/clients/${clientId}/refresh`).then((r) => r.data)
  }
}
