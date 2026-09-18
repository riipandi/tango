import { z } from 'zod'

// Mirrors modules/admin/apikey: the APIKey row and the one-time token
// envelope (create and renew answers carry the plaintext exactly once).
export const APIKeySchema = z.object({
  id: z.string(),
  name: z.string(),
  description: z.string().nullable().optional(),
  expires_at: z.string(),
  created_at: z.string(),
  last_used_at: z.string().nullable().optional()
})

export const APIKeySecretSchema = z.object({
  api_key: APIKeySchema,
  token: z.string()
})

export const CreateAPIKeySchema = z.object({
  name: z.string(),
  description: z.string().nullable().optional(),
  expires_at: z.string()
})

export const RenewAPIKeySchema = z.object({
  expires_at: z.string()
})

export type APIKey = z.infer<typeof APIKeySchema>
export type APIKeySecret = z.infer<typeof APIKeySecretSchema>
export type CreateAPIKeyParams = z.infer<typeof CreateAPIKeySchema>
export type RenewAPIKeyParams = z.infer<typeof RenewAPIKeySchema>
