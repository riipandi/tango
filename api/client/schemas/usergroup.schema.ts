import { z } from 'zod'

export const UserGroupSchema = z.object({
  id: z.string(),
  name: z.string(),
  display_name: z.string(),
  created_at: z.string(),
  updated_at: z.string().nullable().optional()
})

export const CreateUserGroupSchema = z.object({
  name: z.string(),
  display_name: z.string()
})

export const UpdateUserGroupSchema = z.object({
  name: z.string().nullable().optional(),
  display_name: z.string().nullable().optional()
})

export type UserGroup = z.infer<typeof UserGroupSchema>
export type CreateUserGroupParams = z.infer<typeof CreateUserGroupSchema>
export type UpdateUserGroupParams = z.infer<typeof UpdateUserGroupSchema>
