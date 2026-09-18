import { z } from 'zod'

// Mirrors modules/identity/user.User: nullable Go pointers serialize as
// absent when nil (omitzero), so fields are both optional and nullable.
export const UserSchema = z.object({
  id: z.string(),
  username: z.string(),
  email: z.string(),
  first_name: z.string().nullable().optional(),
  last_name: z.string().nullable().optional(),
  avatar_url: z.string().nullable().optional(),
  locale: z.string().nullable().optional(),
  display_name: z.string(),
  is_admin: z.boolean(),
  disabled: z.boolean(),
  email_verified_at: z.string().nullable().optional(),
  created_at: z.string(),
  updated_at: z.string().nullable().optional(),
  last_login_at: z.string().nullable().optional()
})

export const CreateUserSchema = z.object({
  username: z.string(),
  email: z.email(),
  first_name: z.string().optional(),
  last_name: z.string().optional(),
  display_name: z.string().optional(),
  is_admin: z.boolean()
})

export const AdminUpdateUserSchema = z.object({
  email: z.string().nullable().optional(),
  first_name: z.string().nullable().optional(),
  last_name: z.string().nullable().optional(),
  display_name: z.string().nullable().optional(),
  is_admin: z.boolean().optional(),
  disabled: z.boolean().optional()
})

export const UpdateProfileSchema = z.object({
  first_name: z.string().nullable().optional(),
  last_name: z.string().nullable().optional(),
  display_name: z.string().nullable().optional(),
  avatar_url: z.string().nullable().optional(),
  locale: z.string().nullable().optional()
})

export const ChangePasswordSchema = z.object({
  current_password: z.string(),
  new_password: z.string().min(8)
})

export type User = z.infer<typeof UserSchema>
export type CreateUserParams = z.infer<typeof CreateUserSchema>
export type AdminUpdateUserParams = z.infer<typeof AdminUpdateUserSchema>
export type UpdateProfileParams = z.infer<typeof UpdateProfileSchema>
export type ChangePasswordParams = z.infer<typeof ChangePasswordSchema>
