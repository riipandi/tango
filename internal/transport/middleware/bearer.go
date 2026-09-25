package middleware

import (
	"context"
	"net/http"
	"strings"

	"connectrpc.com/authn"
	"connectrpc.com/connect"

	"github.com/riipandi/tango/pkg/responder"
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

// PublicRoute is one REST route the bearer requirement excuses. The pattern
// is a chi-style path: literal segments, `{param}` matching any one segment.
type PublicRoute struct {
	// Method is the HTTP method the route answers, GET, PUT, and friends.
	Method string
	// Pattern is the path the route claims, without a leading slash.
	Pattern string
}

// matches reports whether the request names the route. The path is compared
// segment by segment, so a wildcard stands for one segment — never for the
// rest of the path.
func (p PublicRoute) matches(r *http.Request) bool {
	if r.Method != p.Method {
		return false
	}
	path := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	pattern := strings.Split(strings.Trim(p.Pattern, "/"), "/")
	if len(path) != len(pattern) {
		return false
	}
	for i, segment := range pattern {
		if strings.HasPrefix(segment, "{") {
			continue
		}
		if segment != path[i] {
			return false
		}
	}
	return true
}

// RESTBearer guards the REST surface's routes with the same authenticator the
// RPC surface runs. A route named in public is answered without a caller;
// every other path requires one, so a new route is protected by default and a
// public one is a deliberate entry in the caller's set.
//
// The verified caller's claims travel through the context the authn library
// reads — the same store BearerAuth's middleware fills — so a handler reads
// its caller the same way on both transports. The refusal is the REST
// envelope, because the caller here is one that reads envelopes.
//
// A nil authenticator answers with the inner handler unchanged, which is the
// state a test that reads only responses is in.
func RESTBearer(auth Authenticator, public []PublicRoute) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if auth == nil {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for _, route := range public {
				if route.matches(r) {
					next.ServeHTTP(w, r)
					return
				}
			}

			claims, err := auth(r.Context(), r)
			if err != nil {
				responder.Fail(w, r, http.StatusUnauthorized, "authentication required")
				return
			}
			next.ServeHTTP(w, r.WithContext(authn.SetInfo(r.Context(), claims)))
		})
	}
}
