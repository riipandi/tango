// Device-side passwordless pairing: a device creates the request and
// polls the exchange. The approval side is the deviceApproval
// namespace (the OIDC device-flow browser surface) and the Connect
// DeviceApprovalService.

import { isDeviceLoginPending } from '../schemas/devicelogin.schema'
import type { DeviceLoginExchangeResult, DeviceLoginRequest } from '../schemas/devicelogin.schema'
import type { Executor } from '../types'

export { isDeviceLoginPending }

export interface DeviceLoginModule {
  /** Creates a request (pairing cookie rides the response). */
  create(): Promise<DeviceLoginRequest>
  /**
   * Long-polls one exchange: the signed-in user (200), a poll hint
   * (202, use {@link isDeviceLoginPending}), or an error.
   */
  exchange(requestId: string): Promise<DeviceLoginExchangeResult>
}

export function createDeviceLoginModule(exec: Executor): DeviceLoginModule {
  return {
    create: () => exec.post<DeviceLoginRequest>('/device-login/requests').then((r) => r.data),
    exchange: (requestId) =>
      exec
        .post<DeviceLoginExchangeResult>(`/device-login/requests/${requestId}/exchange`)
        .then((r) => r.data)
  }
}
