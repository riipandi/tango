import { z } from 'zod'

// Wire shape of one editable setting (modules/admin/appconfig). Sensitive
// keys list with an empty value, never their stored content.
export const ConfigVariableSchema = z.object({
  key: z.string(),
  type: z.enum(['string', 'int', 'bool']),
  value: z.string(),
  is_public: z.boolean().optional()
})

export type ConfigVariable = z.infer<typeof ConfigVariableSchema>
