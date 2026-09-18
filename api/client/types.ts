// Wire and client-facing types for the tango API: the responder envelope,
// its camelCase projection, and the transport contract shared by all SDK
// modules. Types only — runtime helpers live in envelope.ts / http.ts.

export interface FieldError {
  field: string
  message: string
}

export interface RateLimitInfo {
  limit: number
  remaining: number
  reset: number
}

/** Envelope metadata exactly as `pkg/responder` serializes it. */
export interface WireMetadata {
  status_code: number
  request_id: string
  trace_id?: string
  rate_limit?: RateLimitInfo
  page?: number
  limit?: number
  total_pages?: number
  total_items?: number
  first_item_index?: number
  last_item_index?: number
}

export interface ApiEnvelope<T = unknown> {
  status: 'success' | 'error'
  message?: string
  data?: T
  error?: unknown
  metadata: WireMetadata
  links?: Record<string, string | null>
}

/** Client-facing camelCase projection of the envelope metadata. */
export interface ResponseMetadata {
  statusCode: number
  requestId?: string
  traceId?: string
  rateLimit?: RateLimitInfo
  page?: number
  limit?: number
  totalPages?: number
  totalItems?: number
  firstItemIndex?: number
  lastItemIndex?: number
}

export interface CallResult<T> {
  data: T
  metadata: ResponseMetadata
  /** Link relations from the envelope, when present. */
  links?: Record<string, string | null>
}

export interface Pagination {
  page: number
  limit: number
  totalPages: number
  totalItems: number
  firstItemIndex?: number
  lastItemIndex?: number
}

export interface Paginated<T> {
  data: T[]
  pagination?: Pagination
}

export type QueryValue = string | number | boolean | undefined | null

export interface QueryParams {
  [key: string]: QueryValue | QueryValue[]
}

export interface CallOptions {
  query?: QueryParams
  headers?: Record<string, string>
  signal?: AbortSignal
}

export interface CallInit extends CallOptions {
  body?: unknown
}

export type HttpMethod = 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE'

/**
 * Transport surface every SDK module builds on. Paths are resource paths
 * relative to the API prefix: `/users/{id}`, not `/api/users/{id}`.
 */
export interface Executor {
  /** Configured origin; empty string means same origin. */
  base: string
  /** Absolute URL for a resource path under the API prefix. */
  url(path: string): string
  request<T>(method: HttpMethod, path: string, init?: CallInit): Promise<CallResult<T>>
  get<T>(path: string, options?: CallOptions): Promise<CallResult<T>>
  post<T>(path: string, body?: unknown, options?: CallOptions): Promise<CallResult<T>>
  put<T>(path: string, body?: unknown, options?: CallOptions): Promise<CallResult<T>>
  patch<T>(path: string, body?: unknown, options?: CallOptions): Promise<CallResult<T>>
  delete<T>(path: string, options?: CallOptions): Promise<CallResult<T>>
}
