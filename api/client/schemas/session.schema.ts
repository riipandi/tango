import * as z from 'zod'

export const SignInSchema = z.object({
  identity: z.string(),
  password: z.string()
})
