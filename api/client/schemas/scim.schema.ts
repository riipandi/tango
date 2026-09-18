import { z } from 'zod'

// Mirrors modules/federation/scimsync: one SCIM provider bound to an OIDC
// client. The token appears on create only (token,omitempty on read).
export const ScimProviderSchema = z.object({
  id: z.string(),
  endpoint: z.string(),
  token: z.string().optional(),
  oidc_client_id: z.string(),
  last_synced_at: z.string().nullable().optional(),
  created_at: z.string()
})

export const CreateScimProviderSchema = z.object({
  endpoint: z.string(),
  token: z.string()
})

export type ScimProvider = z.infer<typeof ScimProviderSchema>
export type CreateScimProviderParams = z.infer<typeof CreateScimProviderSchema>
