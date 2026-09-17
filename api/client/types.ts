// Wire and client-facing types for the tango API: the responder envelope,
// its metadata projection, and the internal executor contract shared by all
// SDK modules.

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

/** Transport surface the SDK modules are built against. */
export interface Executor {
  base: string
  request<T>(method: HttpMethod, path: string, init?: CallInit): Promise<CallResult<T>>
  get<T>(path: string, options?: CallOptions): Promise<CallResult<T>>
  post<T>(path: string, body?: unknown, options?: CallOptions): Promise<CallResult<T>>
  put<T>(path: string, body?: unknown, options?: CallOptions): Promise<CallResult<T>>
  patch<T>(path: string, body?: unknown, options?: CallOptions): Promise<CallResult<T>>
  delete<T>(path: string, options?: CallOptions): Promise<CallResult<T>>
}

export function isEnvelope(value: unknown): value is ApiEnvelope {
  if (typeof value !== 'object' || value === null) return false
  const candidate = value as Record<string, unknown>
  if (candidate['status'] !== 'success' && candidate['status'] !== 'error') return false
  return typeof candidate['metadata'] === 'object' && candidate['metadata'] !== null
}
