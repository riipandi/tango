// Dependency health probe. Version metadata serves ConnectRPC
// exclusively (VersionService below /rpc).

import type { Executor } from '../types'

export interface SystemModule {
  /** Readiness probe with per-dependency results (bare document). */
  health(): Promise<{ status: string; details?: Record<string, unknown> }>
}

export function createSystemModule(exec: Executor): SystemModule {
  return {
    health: () =>
      exec
        .get<{ status: string; details?: Record<string, unknown> }>('/healthz')
        .then((r) => r.data)
  }
}
