import { z } from 'zod'
import { UserSchema } from './user.schema'

export const SignInSchema = z.object({
  identity: z.string(),
  secret: z.string()
})

export const ForgotPasswordSchema = z.object({
  identity: z.string()
})

export const ResetPasswordSchema = z.object({
  token: z.string(),
  new_password: z.string().min(8)
})

export const SignUpSchema = z.object({
  username: z.string(),
  email: z.email(),
  first_name: z.string().optional(),
  last_name: z.string().optional(),
  token: z.string().optional()
})

export const SignInResultSchema = z.object({
  user: UserSchema,
  session_id: z.string(),
  provider: z.string(),
  expires_at: z.string().optional()
})

// Client-safe session projection served by /account/sessions: never the
// token hash.
export const SessionViewSchema = z.object({
  id: z.string(),
  provider: z.string(),
  user_agent: z.string().nullable().optional(),
  device_name: z.string().nullable().optional(),
  ip_address: z.string().nullable().optional(),
  created_at: z.string(),
  expires_at: z.string(),
  refreshed_at: z.string().nullable().optional()
})

export type SignInParams = z.infer<typeof SignInSchema>
export type ForgotPasswordParams = z.infer<typeof ForgotPasswordSchema>
export type ResetPasswordParams = z.infer<typeof ResetPasswordSchema>
export type SignUpParams = z.infer<typeof SignUpSchema>
export type SignInResult = z.infer<typeof SignInResultSchema>
export type SessionView = z.infer<typeof SessionViewSchema>
