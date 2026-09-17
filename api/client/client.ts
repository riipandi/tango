// Public SDK factory: composes the feature namespaces over one executor.

import { createHttp } from './http'
import { createAccountModule, type AccountModule } from './modules/account.mod'
import { createApiAccessModule, type ApiAccessModule } from './modules/apiaccess.mod'
import { createAPIKeysModule, type APIKeysModule } from './modules/apikeys.mod'
import { createApisModule, type ApisModule } from './modules/apis.mod'
import { createAppConfigModule, type AppConfigModule } from './modules/appconfig.mod'
import { createAuditLogsModule, type AuditLogsModule } from './modules/auditlogs.mod'
import { createAuthModule, type AuthModule } from './modules/auth.mod'
import { createConsentModule, type ConsentModule } from './modules/consent.mod'
import { createCustomClaimsModule, type CustomClaimsModule } from './modules/customclaims.mod'
import { createDeviceLoginModule, type DeviceLoginModule } from './modules/devicelogin.mod'
import { createOidcClientsModule, type OidcClientsModule } from './modules/oidcclients.mod'
import { createScimModule, type ScimModule } from './modules/scim.mod'
import { createSystemModule, type SystemModule } from './modules/system.mod'
import { createUserGroupsModule, type UserGroupsModule } from './modules/usergroups.mod'
import { createUsersModule, type UsersModule } from './modules/users.mod'
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
  /** Self-service profile, password, sessions, email verification. */
  account: AccountModule
  /** Admin user CRUD, membership, credentials, one-time access. */
  users: UsersModule
  userGroups: UserGroupsModule
  /** Public bootstrap payload plus the admin settings surface. */
  appConfig: AppConfigModule
  /** Admin relying-party registry. */
  oidcClients: OidcClientsModule
  /** The user's consents plus the admin view of any user's consents. */
  consent: ConsentModule
  /** SCIM service providers. */
  scim: ScimModule
  /** API resources, permissions, client grants, CIMD access. */
  apis: ApisModule
  /** Client-centric grant views. */
  apiAccess: ApiAccessModule
  /** The user's own X-API-KEY machine credentials. */
  apiKeys: APIKeysModule
  customClaims: CustomClaimsModule
  auditLogs: AuditLogsModule
  webhooks: WebhooksModule
  /** Passwordless device pairing (QR + polling). */
  deviceLogin: DeviceLoginModule
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
    account: createAccountModule(exec),
    users: createUsersModule(exec),
    userGroups: createUserGroupsModule(exec),
    appConfig: createAppConfigModule(exec),
    oidcClients: createOidcClientsModule(exec),
    consent: createConsentModule(exec),
    scim: createScimModule(exec),
    apis: createApisModule(exec),
    apiAccess: createApiAccessModule(exec),
    apiKeys: createAPIKeysModule(exec),
    customClaims: createCustomClaimsModule(exec),
    auditLogs: createAuditLogsModule(exec),
    webhooks: createWebhooksModule(exec),
    deviceLogin: createDeviceLoginModule(exec),
    system: createSystemModule(exec),
    raw: (method, path, init) => exec.request(method, path, init),
    setApiKey: (apiKey) => exec.setApiKey(apiKey)
  }
}
