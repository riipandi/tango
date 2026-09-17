// Device approval flow (RFC 8628, browser side): the consent page
// reads pending authorizations and approves or denies them. The
// device-side surfaces (token, authorize) are relying-party protocol
// endpoints and intentionally not part of the SDK.

import { z } from 'zod'

/** Bare document (WriteJSON), not wrapped in the response envelope. */
export const DeviceCodeInfoSchema = z.object({
  client_id: z.string(),
  client_name: z.string(),
  scope: z.string()
})

export type DeviceCodeInfo = z.infer<typeof DeviceCodeInfoSchema>
export type DeviceVerifyAction = 'approve' | 'deny'
