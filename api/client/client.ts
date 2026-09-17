import { ofetch } from 'ofetch'

import { ApiClientError, toApiClientError } from './error'
import { createAccountModule, type AccountModule } from './modules/account.mod'
import { createAppConfigModule, type AppConfigModule } from './modules/appconfig.mod'
import { createAuthModule, type AuthModule } from './modules/auth.mod'
import { createUserGroupsModule, type UserGroupsModule } from './modules/usergroup.mod'
import { createUsersModule, type UsersModule } from './modules/user.mod'
import { isEnvelope } from './types'
import type {
  CallInit,
  CallResult,
  Executor,
  HttpMethod,
  ResponseMetadata,
  WireMetadata
} from './types'

export interface ApiClientOptions {
  /** API origin; empty string targets the same origin the page was served from. */
  baseUrl?: string
  headers?: Record<string, string>
  /** Defaults to `include` so the `tango_session` cookie flows on every call. */
  credentials?: RequestCredentials
  timeout?: number
  fetch?: typeof globalThis.fetch
}

export interface ApiClient {
  auth: AuthModule
  account: AccountModule
  users: UsersModule
  userGroups: UserGroupsModule
  appConfig: AppConfigModule
  /** Typed escape hatch for endpoints without a dedicated namespace yet. */
  raw<T>(method: HttpMethod, path: string, init?: CallInit): Promise<CallResult<T>>
}

/**
 * Builds the namespaced SDK client. One ofetch instance backs all modules;
 * failures surface as {@link ApiClientError} with no transport-level retries
 * (retry policy belongs to the caller, e.g. TanStack Query).
 */
export function createApiClient(options: ApiClientOptions = {}): ApiClient {
  const baseUrl = options.baseUrl ?? ''

  // `fetch` is a global option in ofetch: it must arrive via the second
  // create() argument, not the request defaults, or it is ignored.
  const http = ofetch.create(
    {
      baseURL: baseUrl,
      credentials: options.credentials ?? 'include',
      retry: 0,
      headers: options.headers,
      ...(options.timeout !== undefined && { timeout: options.timeout })
    },
    options.fetch !== undefined ? { fetch: options.fetch } : {}
  )

  async function request<T>(method: HttpMethod, path: string, init: CallInit = {}): Promise<CallResult<T>> {
    let response
    try {
      response = await http.raw<unknown>(path, {
        method,
        body: init.body as never,
        query: init.query,
        headers: init.headers,
        signal: init.signal
      })
    } catch (cause) {
      throw toApiClientError(cause)
    }

    const body = response._data
    if (isEnvelope(body)) {
      if (body.status === 'error') throw ApiClientError.fromEnvelope(body, response.status)
      return { data: body.data as T, metadata: metadataOf(body.metadata, response.status) }
    }

    // Bare document (WebAuthn begin payloads, JWKS, discovery): no envelope.
    const requestId = response.headers.get('x-request-id') ?? undefined
    return { data: body as T, metadata: { statusCode: response.status, requestId } }
  }

  const exec: Executor = {
    base: baseUrl,
    request: (method, path, init) => request(method, path, init),
    get: (path, opts) => request('GET', path, { ...opts }),
    post: (path, body, opts) => request('POST', path, { ...opts, body }),
    put: (path, body, opts) => request('PUT', path, { ...opts, body }),
    patch: (path, body, opts) => request('PATCH', path, { ...opts, body }),
    delete: (path, opts) => request('DELETE', path, { ...opts })
  }

  return {
    auth: createAuthModule(exec),
    account: createAccountModule(exec),
    users: createUsersModule(exec),
    userGroups: createUserGroupsModule(exec),
    appConfig: createAppConfigModule(exec),
    raw: (method, path, init) => request(method, path, init)
  }
}

function metadataOf(metadata: WireMetadata, status: number): ResponseMetadata {
  return {
    statusCode: metadata.status_code ?? status,
    requestId: metadata.request_id,
    traceId: metadata.trace_id,
    rateLimit: metadata.rate_limit,
    page: metadata.page,
    limit: metadata.limit,
    totalPages: metadata.total_pages,
    totalItems: metadata.total_items,
    firstItemIndex: metadata.first_item_index,
    lastItemIndex: metadata.last_item_index
  }
}
