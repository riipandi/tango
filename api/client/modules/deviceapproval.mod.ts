// Browser side of the device flow: what the consent page shows for a
// user code and the approve/deny decision posted by the signed-in
// user. The verify endpoint is form-encoded per the OAuth profile.

import type { DeviceCodeInfo, DeviceVerifyAction } from '../schemas/device.schema'
import type { Executor } from '../types'

export interface DeviceApprovalModule {
  /** Pending authorization details for a user code (bare document). */
  info(userCode: string): Promise<DeviceCodeInfo>
  /** Approves or denies the pending code with the caller's session (204). */
  verify(userCode: string, action: DeviceVerifyAction): Promise<void>
}

export function createDeviceApprovalModule(exec: Executor): DeviceApprovalModule {
  return {
    info: (userCode) =>
      exec
        .get<DeviceCodeInfo>('/oidc/device/info', { query: { code: userCode } })
        .then((r) => r.data),
    verify: (userCode, action) =>
      exec
        .post('/oidc/device/verify', new URLSearchParams({ code: userCode, action }))
        .then(() => undefined)
  }
}
