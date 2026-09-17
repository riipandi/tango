// Audit trail: paginated listings, the unfiltered admin view, and
// suggested filter values.

import { toPaginated } from '../pagination'
import type { AuditLog, AuditLogListParams } from '../schemas/auditlog.schema'
import type { Executor, Paginated } from '../types'

export interface AuditLogsModule {
  /** Entries visible to the caller (self-scoped for non-admins). */
  list(params?: AuditLogListParams): Promise<Paginated<AuditLog>>
  /** Every entry; admin only. */
  listAll(params?: AuditLogListParams): Promise<Paginated<AuditLog>>
  suggestedClientNames(): Promise<string[]>
  suggestedUsers(): Promise<string[]>
}

export function createAuditLogsModule(exec: Executor): AuditLogsModule {
  const list = (scopedAll: boolean) => (params?: AuditLogListParams) =>
    exec
      .get<AuditLog[]>(scopedAll ? '/audit-logs/all' : '/audit-logs', {
        query: {
          event: params?.event,
          user_id: params?.user_id,
          from: params?.from,
          to: params?.to,
          page: params?.page,
          limit: params?.limit
        }
      })
      .then(toPaginated)

  return {
    list: list(false),
    listAll: list(true),
    suggestedClientNames: () =>
      exec.get<string[]>('/audit-logs/filters/client-names').then((r) => r.data),
    suggestedUsers: () => exec.get<string[]>('/audit-logs/filters/users').then((r) => r.data)
  }
}
