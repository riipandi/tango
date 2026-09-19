// Public entry point of the tango API client SDK.

import { createApiClient } from './client'
import type { ApiClient } from './client'

// Client factory and options.
export { createApiClient } from './client'
export type { ApiClient, ApiClientOptions } from './client'
export { ApiClientError, toApiClientError } from './error'
export type { ApiErrorCode } from './error'
export { toPaginated } from './pagination'

// Wire and result types.
export type {
  ApiEnvelope,
  CallInit,
  CallOptions,
  CallResult,
  Executor,
  FieldError,
  HttpMethod,
  Paginated,
  Pagination,
  QueryParams,
  QueryValue,
  RateLimitInfo,
  ResponseMetadata,
  WireMetadata
} from './types'

// Auth and identity types.
export type {
  ForgotPasswordParams,
  ResetPasswordParams,
  SessionView,
  SignInParams,
  SignInResult
} from './schemas/session.schema'
export type {
  AdminUpdateUserParams,
  ChangePasswordParams,
  CreateUserParams,
  UpdateProfileParams,
  User
} from './schemas/user.schema'
export type { WebAuthnBeginResult, WebAuthnCredential } from './schemas/webauthn.schema'
export type { DeviceCodeInfo, DeviceVerifyAction } from './schemas/device.schema'
export type {
  DeviceLoginExchangeResult,
  DeviceLoginPending,
  DeviceLoginRequest,
  VerificationInfo
} from './schemas/devicelogin.schema'

// Admin surface types.

/**
 * Default client for the embedded SPA: same origin, cookie-backed session.
 * Pass a custom baseUrl or apiKey through `createApiClient` for other
 * targets (e.g. machine-to-machine scripts).
 */
export const apiClient: ApiClient = createApiClient()
export default apiClient
