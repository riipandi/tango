import { z } from 'zod'

// Mirrors the federation/oidc client view (handler.go client.view()).
export const OidcClientSchema = z.object({
  id: z.string(),
  name: z.string(),
  description: z.string().nullable().optional(),
  has_secret: z.boolean(),
  callback_urls: z.array(z.string()),
  logout_callback_urls: z.array(z.string()).optional(),
  launch_url: z.string().nullable().optional(),
  is_public: z.boolean().optional(),
  pkce_enabled: z.boolean().optional(),
  pkce_supported: z.boolean().optional(),
  skip_consent: z.boolean().optional(),
  is_group_restricted: z.boolean().optional(),
  access_token_duration_minutes: z.number().optional(),
  refresh_token_duration_minutes: z.number().optional(),
  allowed_user_group_ids: z.array(z.string()).optional(),
  client_type: z.string().optional(),
  metadata_url: z.string().nullable().optional()
})

// Create echoes the view plus the generated secret exactly once.
export const OidcClientSecretSchema = z.looseObject({
  client_secret: z.string()
})

// handler_meta.go metaView().
export const OidcClientMetaSchema = z.object({
  id: z.string(),
  name: z.string(),
  description: z.string().nullable().optional(),
  has_logo: z.boolean(),
  launch_url: z.string().nullable().optional(),
  requires_reauthentication: z.boolean().optional(),
  client_type: z.string().optional()
})

// store_client.go SecretEntryView.
export const OidcSecretEntrySchema = z.object({
  id: z.string(),
  created_at: z.string(),
  expires_at: z.string().nullable().optional(),
  is_active: z.boolean()
})

// handler_meta.go token preview payload.
export const OidcTokenPreviewSchema = z.object({
  id_token: z.string(),
  access_token: z.string(),
  user_info: z.record(z.string(), z.unknown())
})

// store_authz.go AuthorizedClient.
export const AuthorizedClientSchema = z.object({
  user_id: z.string(),
  client_id: z.string(),
  scopes: z.array(z.string()),
  last_used_at: z.string()
})

export type OidcClient = z.infer<typeof OidcClientSchema>
export type OidcClientMeta = z.infer<typeof OidcClientMetaSchema>
export type OidcSecretEntry = z.infer<typeof OidcSecretEntrySchema>
export type OidcTokenPreview = z.infer<typeof OidcTokenPreviewSchema>
export type AuthorizedClient = z.infer<typeof AuthorizedClientSchema>

/**
 * Create/update body (handler.go clientRequest). All optional fields keep
 * their zero values server-side; create additionally accepts `secret`
 * (BYO secret), `metadata_url` (CIMD), and `allowed_user_group_ids`.
 */
export interface OidcClientParams {
  name: string
  description?: string
  secret?: string | null
  callback_urls: string[]
  logout_callback_urls?: string[]
  launch_url?: string
  is_public?: boolean
  pkce_enabled?: boolean
  pkce_supported?: boolean
  skip_consent?: boolean
  is_group_restricted?: boolean
  access_token_duration_minutes?: number
  refresh_token_duration_minutes?: number
  allowed_user_group_ids?: string[]
  metadata_url?: string
}
