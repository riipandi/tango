package middleware

import (
	"context"
	"net/http"

	"connectrpc.com/authn"
	"connectrpc.com/connect"
)

// Authenticator authenticates one request and answers the identity the
// procedures read from the context. It is the authn library's own shape, so
// the middleware below and the transport that mounts it share one type.
type Authenticator = authn.AuthFunc

// BearerAuth wraps the RPC surface with authentication. The authn middleware
// runs before a request is decoded — an unauthenticated call costs no
// unmarshal — and it receives the same handler options the procedures are
// registered with, so a refusal is marshaled in the protocol the caller used.
//
// A procedure named in public is answered without a caller; every other path
// requires one, so a new procedure is protected by default and a public one
// is a deliberate entry in the caller's set. A nil authenticator returns the
// handler unchanged, which is the state a test that reads only responses is
// in.
func BearerAuth(auth Authenticator, public map[string]struct{}, options []connect.HandlerOption, inner http.Handler) http.Handler {
	if auth == nil {
		return inner
	}
	return authn.NewMiddleware(publicOnly(auth, public), options...).Wrap(inner)
}

// publicOnly excuses the public procedures from the authenticator. An unknown
// path is not public, so it is refused as unauthenticated rather than
// unimplemented — the refusal hides which procedures exist from a caller
// without a token.
func publicOnly(auth Authenticator, public map[string]struct{}) Authenticator {
	return func(ctx context.Context, req *http.Request) (any, error) {
		if procedure, ok := authn.InferProcedure(req.URL); ok {
			if _, isPublic := public[procedure]; isPublic {
				return nil, nil
			}
		}
		return auth(ctx, req)
	}
}
