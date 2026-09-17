import { createApiClient } from './client'
import type { ApiClient } from './client'

export { createApiClient } from './client'
export type { ApiClient, ApiClientOptions } from './client'
export { ApiClientError, toApiClientError } from './error'
export type { ApiErrorCode } from './error'
export { toPaginated } from './pagination'
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
export type {
  ForgotPasswordParams,
  ResetPasswordParams,
  SessionView,
  SignInParams,
  SignInResult,
  SignUpParams
} from './schemas/session.schema'
export type {
  AdminUpdateUserParams,
  ChangePasswordParams,
  CreateUserParams,
  UpdateProfileParams,
  User
} from './schemas/user.schema'
export type {
  CreateUserGroupParams,
  UpdateUserGroupParams,
  UserGroup
} from './schemas/usergroup.schema'
export type { ConfigVariable } from './schemas/appconfig.schema'
export type { WebAuthnBeginResult, WebAuthnCredential } from './schemas/webauthn.schema'

/**
 * Default client for the embedded SPA: same origin, cookie-backed session.
 * Pass a custom baseUrl through `createApiClient` for cross-origin targets.
 */
export const apiClient: ApiClient = createApiClient()
export default apiClient
