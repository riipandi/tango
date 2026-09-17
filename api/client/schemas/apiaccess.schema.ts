import { z } from 'zod'

// Mirrors modules/admin/apiaccess: the API registry, its permissions,
// client references, and grant views.
export const PermissionSchema = z.object({
  id: z.string(),
  key: z.string(),
  name: z.string(),
  description: z.string().nullable().optional(),
  allowed_for_cimd_clients: z.boolean().optional()
})

export const APISchema = z.object({
  id: z.string(),
  name: z.string(),
  resource: z.string(),
  allow_cimd_clients: z.boolean(),
  permissions: z.array(PermissionSchema),
  created_at: z.string(),
  updated_at: z.string().nullable().optional()
})

export const ClientRefSchema = z.object({
  id: z.string(),
  name: z.string(),
  client_type: z.string(),
  is_public: z.boolean(),
  has_logo: z.boolean()
})

export const GrantParamsSchema = z.object({
  user_delegated_access: z.boolean(),
  user_delegated_permission_ids: z.array(z.string()),
  client_access: z.boolean(),
  client_permission_ids: z.array(z.string())
})

export const GrantSchema = z.object({
  client_access: z.boolean(),
  client_permission_ids: z.array(z.string()),
  user_delegated_access: z.boolean(),
  user_delegated_permission_ids: z.array(z.string())
})

// clientAPIGrantResponse: one grant plus the API it belongs to.
export const ClientAPIGrantSchema = z.object({
  api: APISchema,
  client_access: z.boolean(),
  client_permission_ids: z.array(z.string()),
  user_delegated_access: z.boolean(),
  user_delegated_permission_ids: z.array(z.string()),
  cimd_granted_access: z.boolean(),
  cimd_granted_permission_ids: z.array(z.string())
})

export type Permission = z.infer<typeof PermissionSchema>
export type API = z.infer<typeof APISchema>
export type ClientRef = z.infer<typeof ClientRefSchema>
export type GrantParams = z.infer<typeof GrantParamsSchema>
export type Grant = z.infer<typeof GrantSchema>
export type ClientAPIGrant = z.infer<typeof ClientAPIGrantSchema>
