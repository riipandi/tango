// Admin signup-token registry: list, issue, revoke. Tokens gate
// account creation when signup is restricted; the raw token is
// returned exactly once at creation.

import type {
  CreateSignupTokenParams,
  SignupToken,
  SignupTokenSecret
} from '../schemas/signuptoken.schema'
import type { Executor } from '../types'

export interface SignupTokensModule {
  list(): Promise<SignupToken[]>
  /** Issues a token; the plaintext appears exactly once (201). */
  create(params: CreateSignupTokenParams): Promise<SignupTokenSecret>
  remove(tokenId: string): Promise<void>
}

export function createSignupTokensModule(exec: Executor): SignupTokensModule {
  return {
    list: () => exec.get<SignupToken[]>('/signup-tokens').then((r) => r.data),
    create: (params) => exec.post<SignupTokenSecret>('/signup-tokens', params).then((r) => r.data),
    remove: (tokenId) => exec.delete(`/signup-tokens/${tokenId}`).then(() => undefined)
  }
}
