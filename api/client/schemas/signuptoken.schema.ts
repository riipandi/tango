// Signup tokens: admin-issued, token-gated account creation.

import { z } from 'zod'

export const SignupTokenSchema = z.object({
  id: z.string(),
  usage_limit: z.number(),
  usage_count: z.number(),
  created_at: z.string(),
  expires_at: z.string(),
  user_groups: z.array(z.string())
})

export const CreateSignupTokenSchema = z.object({
  /** Go duration string (e.g. 24h); omitted means no expiry policy. */
  ttl: z.string().optional(),
  usage_limit: z.number().int().min(1),
  user_group_ids: z.array(z.string()).optional()
})

export type SignupToken = z.infer<typeof SignupTokenSchema>
export type CreateSignupTokenParams = z.infer<typeof CreateSignupTokenSchema>
/** Creation response: the raw token appears exactly once. */
export type SignupTokenSecret = SignupToken & { token: string }
