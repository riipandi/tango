import * as z from 'zod'

export const UserSchema = z.object({})

export const UserCreateSchema = z.object({})

export type User = z.infer<typeof UserSchema>
