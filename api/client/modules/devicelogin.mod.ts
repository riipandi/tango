// Passwordless device pairing: a device creates the request and polls,
// an authenticated user inspects the code and approves or denies.

import { isDeviceLoginPending } from '../schemas/devicelogin.schema'
import type {
  DeviceLoginExchangeResult,
  DeviceLoginRequest,
  VerificationInfo
} from '../schemas/devicelogin.schema'
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
  /** What the approving device will see for a user code. */
  inspect(code: string): Promise<VerificationInfo>
  /** Approves (`decision: 'approve'`) or denies the pairing. */
  decide(code: string, decision: 'approve' | 'deny'): Promise<void>
}

export function createDeviceLoginModule(exec: Executor): DeviceLoginModule {
  return {
    create: () => exec.post<DeviceLoginRequest>('/device-login/requests').then((r) => r.data),
    exchange: (requestId) =>
      exec
        .post<DeviceLoginExchangeResult>(`/device-login/requests/${requestId}/exchange`)
        .then((r) => r.data),
    inspect: (code) =>
      exec.post<VerificationInfo>('/device-login/verification', { code }).then((r) => r.data),
    decide: (code, decision) =>
      exec.post('/device-login/verification/decision', { code, decision }).then(() => undefined)
  }
}
