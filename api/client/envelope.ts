// Envelope detection and projection: identifies the responder envelope on
// the wire and maps it onto the client-facing result shape.

import type { ApiEnvelope, ResponseMetadata, WireMetadata } from './types'

/** Reports whether a parsed response body is the standard envelope. */
export function isEnvelope(value: unknown): value is ApiEnvelope {
  if (typeof value !== 'object' || value === null) return false
  const candidate = value as Record<string, unknown>
  if (candidate['status'] !== 'success' && candidate['status'] !== 'error') return false
  return typeof candidate['metadata'] === 'object' && candidate['metadata'] !== null
}

/** Projects wire metadata onto the camelCase client shape, filling defaults. */
export function metadataOf(metadata: WireMetadata, fallbackStatus: number): ResponseMetadata {
  return {
    statusCode: metadata.status_code ?? fallbackStatus,
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
