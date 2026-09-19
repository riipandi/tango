// Public SDK factory: composes the feature namespaces over one executor.

import { createHttp } from './http'
import { createAPIKeysModule, type APIKeysModule } from './modules/apikeys.mod'
import { createAuthModule, type AuthModule } from './modules/auth.mod'
import { createDeviceApprovalModule, type DeviceApprovalModule } from './modules/deviceapproval.mod'
import { createDeviceLoginModule, type DeviceLoginModule } from './modules/devicelogin.mod'
import { createSystemModule, type SystemModule } from './modules/system.mod'
import { createWebhooksModule, type WebhooksModule } from './modules/webhooks.mod'
import type { CallInit, CallResult, HttpMethod } from './types'

export interface ApiClientOptions {
  /** API origin; empty string targets the same origin the page was served from. */
  baseUrl?: string
  headers?: Record<string, string>
  /** Defaults to `include` so the `tango_session` cookie flows on every call. */
  credentials?: RequestCredentials
  timeout?: number
  fetch?: typeof globalThis.fetch
  /** Machine credential for X-API-KEY-guarded admin surfaces. */
  apiKey?: string
}

export interface ApiClient {
  /** Sign-in, recovery, signup, MFA, WebAuthn, one-time access. */
  auth: AuthModule
  /** The user's own X-API-KEY machine credentials. */
  apiKeys: APIKeysModule
  webhooks: WebhooksModule
  /** Passwordless device pairing (QR + polling). */
  deviceLogin: DeviceLoginModule
  /** Browser side of the OAuth device flow: consent info + decision. */
  deviceApproval: DeviceApprovalModule
  /** Version metadata and the readiness probe. */
  system: SystemModule
  /**
   * Typed escape hatch for endpoints without a dedicated namespace yet.
   * The path is a resource path under the API prefix: `/users/{id}` calls
   * `/api/users/{id}`.
   */
  raw<T>(method: HttpMethod, path: string, init?: CallInit): Promise<CallResult<T>>
  /** Rotates (or clears with `undefined`) the X-API-KEY credential. */
  setApiKey(apiKey: string | undefined): void
}

/**
 * Builds the namespaced SDK client. Session auth rides the `tango_session`
 * cookie (`credentials: 'include'`); machine auth rides `X-API-KEY` when an
 * `apiKey` is configured. Failures surface as {@link ApiClientError} with no
 * transport-level retries (retry policy belongs to the caller, e.g. TanStack
 * Query).
 */
export function createApiClient(options: ApiClientOptions = {}): ApiClient {
  const exec = createHttp(options)

  return {
    auth: createAuthModule(exec),
    apiKeys: createAPIKeysModule(exec),
    webhooks: createWebhooksModule(exec),
    deviceLogin: createDeviceLoginModule(exec),
    deviceApproval: createDeviceApprovalModule(exec),
    system: createSystemModule(exec),
    raw: (method, path, init) => exec.request(method, path, init),
    setApiKey: (apiKey) => exec.setApiKey(apiKey)
  }
}
