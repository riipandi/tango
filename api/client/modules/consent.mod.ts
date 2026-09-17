// Consent surfaces: the user's own authorized clients plus the admin
// view of any user's consents.

import type { ClientRef } from '../schemas/apiaccess.schema'
import type { AuthorizedClient } from '../schemas/oidcclient.schema'
import type { Executor } from '../types'

export interface ConsentModule {
  /** Clients this user has consented to. */
  listMine(): Promise<AuthorizedClient[]>
  /** Revokes one consent and kills the user's active tokens for it. */
  revokeMine(clientId: string): Promise<void>
  /** Clients the signed-in user may access (group rules applied). */
  listAccessible(): Promise<ClientRef[]>
  /** Admin: consents of one user. */
  listForUser(userId: string): Promise<AuthorizedClient[]>
  /** Admin: every consent in the instance. */
  listAll(): Promise<AuthorizedClient[]>
}

export function createConsentModule(exec: Executor): ConsentModule {
  return {
    listMine: () =>
      exec.get<AuthorizedClient[]>('/oidc/users/me/authorized-clients').then((r) => r.data),
    revokeMine: (clientId) =>
      exec.delete(`/oidc/users/me/authorized-clients/${clientId}`).then(() => undefined),
    listAccessible: () => exec.get<ClientRef[]>('/oidc/users/me/clients').then((r) => r.data),
    listForUser: (userId) =>
      exec.get<AuthorizedClient[]>(`/oidc/users/${userId}/authorized-clients`).then((r) => r.data),
    listAll: () => exec.get<AuthorizedClient[]>('/oidc/authorized-clients').then((r) => r.data)
  }
}
