import type { CallResult, Paginated } from './types'

/**
 * Builds the list result from unwrapped envelope metadata. `pagination` is
 * omitted when the server skipped paging (the page -1 "all" marker).
 */
export function toPaginated<T>(result: CallResult<T[]>): Paginated<T> {
  const { data, metadata } = result
  if (metadata.totalItems === undefined) return { data }

  return {
    data,
    pagination: {
      page: metadata.page ?? 1,
      limit: metadata.limit ?? data.length,
      totalPages: metadata.totalPages ?? 1,
      totalItems: metadata.totalItems,
      firstItemIndex: metadata.firstItemIndex,
      lastItemIndex: metadata.lastItemIndex
    }
  }
}
