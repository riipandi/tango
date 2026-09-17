import { isEnvelope } from './types'
import type { ApiEnvelope, FieldError, RateLimitInfo } from './types'

export type ApiErrorCode = 'api_error' | 'network_error' | 'unknown'

interface ApiClientErrorInit {
  code?: ApiErrorCode
  status?: number
  requestId?: string
  traceId?: string
  fieldErrors?: FieldError[]
  rateLimit?: RateLimitInfo
  envelope?: ApiEnvelope
}

/**
 * Normalized failure for every SDK call: HTTP-level errors keep the
 * envelope's message and validation details; transport failures become
 * `network_error` with no status.
 */
export class ApiClientError extends Error {
  readonly code: ApiErrorCode
  readonly status?: number
  readonly requestId?: string
  readonly traceId?: string
  readonly fieldErrors: FieldError[]
  readonly rateLimit?: RateLimitInfo
  readonly envelope?: ApiEnvelope

  constructor(message: string, init: ApiClientErrorInit = {}) {
    super(message)
    this.name = 'ApiClientError'
    this.code = init.code ?? 'unknown'
    if (init.status !== undefined) this.status = init.status
    if (init.requestId !== undefined) this.requestId = init.requestId
    if (init.traceId !== undefined) this.traceId = init.traceId
    this.fieldErrors = init.fieldErrors ?? []
    if (init.rateLimit !== undefined) this.rateLimit = init.rateLimit
    if (init.envelope !== undefined) this.envelope = init.envelope
  }

  static fromEnvelope(envelope: ApiEnvelope, status?: number): ApiClientError {
    const metadata = envelope.metadata
    return new ApiClientError(envelope.message ?? 'request failed', {
      code: 'api_error',
      status: status ?? metadata.status_code,
      requestId: metadata.request_id,
      traceId: metadata.trace_id,
      fieldErrors: fieldErrorsOf(envelope.error),
      rateLimit: metadata.rate_limit,
      envelope
    })
  }
}

function fieldErrorsOf(detail: unknown): FieldError[] {
  if (!Array.isArray(detail)) return []
  return detail.filter(
    (item): item is FieldError =>
      typeof item === 'object' &&
      item !== null &&
      typeof (item as FieldError).field === 'string' &&
      typeof (item as FieldError).message === 'string'
  )
}

/** Normalizes ofetch FetchError (and raw fetch failures) into ApiClientError. */
export function toApiClientError(error: unknown): ApiClientError {
  if (error instanceof ApiClientError) return error

  const cause = error as {
    response?: { status?: number }
    statusCode?: number
    data?: unknown
    message?: string
  }
  const status = cause.response?.status ?? (typeof cause.statusCode === 'number' ? cause.statusCode : undefined)

  if (status !== undefined) {
    if (isEnvelope(cause.data)) return ApiClientError.fromEnvelope(cause.data, status)
    return new ApiClientError(cause.message ?? 'request failed', { code: 'api_error', status })
  }

  return new ApiClientError('network error', { code: 'network_error' })
}
