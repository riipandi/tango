// Custom token claims attached to users or user groups.

import type {
  CreateCustomClaimParams,
  CustomClaim,
  UpdateCustomClaimParams
} from '../schemas/customclaim.schema'
import type { Executor } from '../types'

export interface CustomClaimsModule {
  suggestions(): Promise<string[]>
  listForUser(userId: string): Promise<CustomClaim[]>
  createForUser(userId: string, params: CreateCustomClaimParams): Promise<CustomClaim>
  /** Replaces the user's whole claim set. */
  replaceForUser(userId: string, claims: CreateCustomClaimParams[]): Promise<CustomClaim[]>
  updateForUser(
    userId: string,
    claimId: string,
    params: UpdateCustomClaimParams
  ): Promise<CustomClaim>
  deleteForUser(userId: string, claimId: string): Promise<void>
  listForGroup(userGroupId: string): Promise<CustomClaim[]>
  createForGroup(userGroupId: string, params: CreateCustomClaimParams): Promise<CustomClaim>
  replaceForGroup(userGroupId: string, claims: CreateCustomClaimParams[]): Promise<CustomClaim[]>
  updateForGroup(
    userGroupId: string,
    claimId: string,
    params: UpdateCustomClaimParams
  ): Promise<CustomClaim>
  deleteForGroup(userGroupId: string, claimId: string): Promise<void>
}

export function createCustomClaimsModule(exec: Executor): CustomClaimsModule {
  return {
    suggestions: () => exec.get<string[]>('/custom-claims/suggestions').then((r) => r.data),
    listForUser: (userId) =>
      exec.get<CustomClaim[]>(`/custom-claims/user/${userId}`).then((r) => r.data),
    createForUser: (userId, params) =>
      exec.post<CustomClaim>(`/custom-claims/user/${userId}`, params).then((r) => r.data),
    replaceForUser: (userId, claims) =>
      exec.put<CustomClaim[]>(`/custom-claims/user/${userId}`, claims).then((r) => r.data),
    updateForUser: (userId, claimId, params) =>
      exec.put<CustomClaim>(`/custom-claims/user/${userId}/${claimId}`, params).then((r) => r.data),
    deleteForUser: (userId, claimId) =>
      exec.delete(`/custom-claims/user/${userId}/${claimId}`).then(() => undefined),
    listForGroup: (userGroupId) =>
      exec.get<CustomClaim[]>(`/custom-claims/user-group/${userGroupId}`).then((r) => r.data),
    createForGroup: (userGroupId, params) =>
      exec
        .post<CustomClaim>(`/custom-claims/user-group/${userGroupId}`, params)
        .then((r) => r.data),
    replaceForGroup: (userGroupId, claims) =>
      exec
        .put<CustomClaim[]>(`/custom-claims/user-group/${userGroupId}`, claims)
        .then((r) => r.data),
    updateForGroup: (userGroupId, claimId, params) =>
      exec
        .put<CustomClaim>(`/custom-claims/user-group/${userGroupId}/${claimId}`, params)
        .then((r) => r.data),
    deleteForGroup: (userGroupId, claimId) =>
      exec.delete(`/custom-claims/user-group/${userGroupId}/${claimId}`).then(() => undefined)
  }
}
