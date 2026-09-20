// Generated Connect clients for the first-party /rpc surface. The
// transport is same-origin Connect Protocol over HTTP; bearer
// injection pulls the short-lived access token from the auth worker
// per request — the token never persists on the UI thread.
import { createClient, type Interceptor } from '@connectrpc/connect'
import { createConnectTransport } from '@connectrpc/connect-web'
import type { Remote } from 'comlink'
import { AuthService } from '~/codegen/identity_pb'

// AuthWorker is the plugin-typed worker API: every export of the
// worker module, Promisified by Comlink. The ComlinkWorker
// constructor is ambient from vite-plugin-comlink/client.
export type AuthWorker = Remote<typeof import('../auth.worker')>

// createAuthWorker instantiates the token lifecycle worker through
// the plugin; the module worker type keeps dev/preview identical.
export function createAuthWorker(): AuthWorker {
  return new ComlinkWorker(new URL('../auth.worker', import.meta.url), { type: 'module' })
}

// bearerInterceptor attaches the Authorization header from the
// worker; a missing token (anonymous visitor) sends no header so the
// server answers its own unauthenticated error.
function bearerInterceptor(worker: AuthWorker): Interceptor {
  return (next) => async (req) => {
    const token = await worker.getAccessToken()
    if (token) {
      req.header.set('Authorization', `Bearer ${token}`)
    }
    return next(req)
  }
}

// createRPCTransport builds the same-origin Connect Protocol
// transport bound to the auth worker's token lifecycle.
export function createRPCTransport(worker: AuthWorker) {
  return createConnectTransport({
    baseUrl: '/rpc',
    interceptors: [bearerInterceptor(worker)]
  })
}

// createAuthClient returns the generated AuthService client.
export function createAuthClient(worker: AuthWorker) {
  return createClient(AuthService, createRPCTransport(worker))
}
