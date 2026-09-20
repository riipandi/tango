import { z } from 'zod'
import { UserSchema } from './user.schema'

// Mirrors modules/identity/devicelogin: the QR pairing payload and
// the long-poll answer. The approval screen reads the OIDC device
// flow (device.schema.ts) and decides through the Connect surface.
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

export type DeviceLoginRequest = z.infer<typeof DeviceLoginRequestSchema>
export type DeviceLoginPending = z.infer<typeof DeviceLoginPendingSchema>

/** Exchange answers with the signed-in user (200) or a poll hint (202). */
export type DeviceLoginExchangeResult = z.infer<typeof UserSchema> | DeviceLoginPending

export function isDeviceLoginPending(
  result: DeviceLoginExchangeResult
): result is DeviceLoginPending {
  return 'status' in result && (result as DeviceLoginPending).status === 'pending'
}
