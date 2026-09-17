import type { Executor } from '../types'
import type { ConfigVariable } from '../schemas/appconfig.schema'

export interface AppConfigModule {
  /** Bootstrap payload the unauthenticated SPA may read. */
  listPublic(): Promise<ConfigVariable[]>
  /** All keys; sensitive values list as empty strings. */
  listAll(): Promise<ConfigVariable[]>
  update(values: Record<string, string>): Promise<ConfigVariable[]>
  /** Queues a test message (202); defaults to the signed-in administrator's address. */
  sendTestEmail(email?: string): Promise<void>
}

export function createAppConfigModule(exec: Executor): AppConfigModule {
  return {
    listPublic: () => exec.get<ConfigVariable[]>('/api/application-configuration').then((r) => r.data),
    listAll: () =>
      exec.get<ConfigVariable[]>('/api/application-configuration/all').then((r) => r.data),
    update: (values) =>
      exec.put<ConfigVariable[]>('/api/application-configuration', values).then((r) => r.data),
    sendTestEmail: (email) =>
      exec.post('/api/application-configuration/test-email', { email }).then(() => undefined)
  }
}
