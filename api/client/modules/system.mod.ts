// Build/version metadata and the dependency health probe.

import type { Executor } from '../types'

export interface SystemModule {
  /** Current deployed version; requires a session. */
  versionCurrent(): Promise<{ current_version: string }>
  /** Newest published release; public. */
  versionLatest(): Promise<{ latest_version: string }>
  /** Readiness probe with per-dependency results (bare document). */
  health(): Promise<{ status: string; details?: Record<string, unknown> }>
}

export function createSystemModule(exec: Executor): SystemModule {
  return {
    versionCurrent: () =>
      exec.get<{ current_version: string }>('/version/current').then((r) => r.data),
    versionLatest: () =>
      exec.get<{ latest_version: string }>('/version/latest').then((r) => r.data),
    health: () =>
      exec
        .get<{ status: string; details?: Record<string, unknown> }>('/healthz')
        .then((r) => r.data)
  }
}
