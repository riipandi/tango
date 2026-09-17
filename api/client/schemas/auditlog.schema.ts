import { z } from 'zod'

// Mirrors modules/admin/auditlog: one audited event with its actor and
// request context.
export const AuditLogSchema = z.object({
  id: z.string(),
  event: z.string(),
  trigger: z.string(),
  status: z.string(),
  payload: z.record(z.string(), z.unknown()).optional(),
  resource_type: z.string().optional(),
  resource_id: z.string().nullable().optional(),
  user_id: z.string().nullable().optional(),
  ip_address: z.string().nullable().optional(),
  user_agent: z.string().nullable().optional(),
  device: z.string().optional(),
  created_at: z.string()
})

export type AuditLog = z.infer<typeof AuditLogSchema>

export interface AuditLogListParams {
  event?: string
  user_id?: string
  from?: string
  to?: string
  page?: number
  limit?: number
}
