import { z } from 'zod'

// Mirrors the custom claim DTO shared by the user and user-group surfaces.
export const CustomClaimSchema = z.object({
  id: z.string(),
  key: z.string(),
  value: z.string(),
  user_id: z.string().nullable().optional(),
  user_group_id: z.string().nullable().optional(),
  created_at: z.string()
})

export const CreateCustomClaimSchema = z.object({
  key: z.string(),
  value: z.string()
})

export const UpdateCustomClaimSchema = z.object({
  value: z.string()
})

export type CustomClaim = z.infer<typeof CustomClaimSchema>
export type CreateCustomClaimParams = z.infer<typeof CreateCustomClaimSchema>
export type UpdateCustomClaimParams = z.infer<typeof UpdateCustomClaimSchema>
