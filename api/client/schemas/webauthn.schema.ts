import { z } from 'zod'

// WebAuthn ceremony payloads. Begin endpoints answer with a bare document
// (no envelope): the raw options object plus the one-time ceremony session.
export const WebAuthnBeginSchema = z.object({
  publicKey: z.record(z.string(), z.unknown()),
  session_id: z.string()
})

// Credential projection returned by finish-registration (credentialView).
export const WebAuthnCredentialSchema = z.object({
  id: z.string(),
  name: z.string().nullable().optional(),
  credential_id: z.string(),
  attestation_type: z.string().nullable().optional(),
  transport: z.array(z.string()).optional(),
  backup_eligible: z.boolean().optional(),
  backup_state: z.boolean().optional(),
  created_at: z.string().optional(),
  last_used_at: z.string().nullable().optional()
})

export type WebAuthnBeginResult = z.infer<typeof WebAuthnBeginSchema>
export type WebAuthnCredential = z.infer<typeof WebAuthnCredentialSchema>
