import { z } from 'zod'
import { UserSchema } from './user.schema'

// Mirrors modules/identity/devicelogin: the QR pairing payload, the
// long-poll answer, and the approval-screen info.
export const DeviceLoginRequestSchema = z.object({
  id: z.string(),
  user_code: z.string(),
  expires_at: z.string(),
  interval: z.number(),
  verification_uri: z.string(),
  verification_uri_complete: z.string()
})

export const DeviceLoginPendingSchema = z.object({
  status: z.literal('pending'),
  interval: z.number()
})

export const VerificationInfoSchema = z.object({
  user_code: z.string(),
  device: z.string(),
  ip_address: z.string(),
  city: z.string(),
  country: z.string(),
  expires_at: z.string()
})

export type DeviceLoginRequest = z.infer<typeof DeviceLoginRequestSchema>
export type DeviceLoginPending = z.infer<typeof DeviceLoginPendingSchema>

/** Exchange answers with the signed-in user (200) or a poll hint (202). */
export type DeviceLoginExchangeResult = z.infer<typeof UserSchema> | DeviceLoginPending
export type VerificationInfo = z.infer<typeof VerificationInfoSchema>

export function isDeviceLoginPending(
  result: DeviceLoginExchangeResult
): result is DeviceLoginPending {
  return 'status' in result && (result as DeviceLoginPending).status === 'pending'
}
