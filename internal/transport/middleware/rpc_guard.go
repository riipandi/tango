package middleware

import (
	"context"
	"net/http"

	"connectrpc.com/connect"

	"github.com/riipandi/tango/internal/kernel"
	"github.com/riipandi/tango/internal/rpcerr"
)

// RPCPrincipalGuard protects selected procedures of a mixed-visibility
// Connect service: procedures named in admin demand an admin
// principal, procedures named in self demand any principal, and
// everything else passes through anonymously. Streaming hooks are
// absent by design: the first-party surface is unary only.
func RPCPrincipalGuard(auth kernel.AccessAuthenticator, admin, self map[string]bool) connect.Interceptor {
	return rpcGuardInterceptor{auth: auth, admin: admin, self: self}
}

type rpcGuardInterceptor struct {
	auth  kernel.AccessAuthenticator
	admin map[string]bool
	self  map[string]bool
}

func (i rpcGuardInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (i rpcGuardInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return next
}

func (i rpcGuardInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		procedure := req.Spec().Procedure
		isAdminProc := i.admin[procedure]
		isSelfProc := i.self[procedure]
		if !isAdminProc && !isSelfProc {
			return next(ctx, req)
		}
		principal, err := ResolveBearer(ctx, i.auth, req.Header())
		if err != nil {
			return nil, err
		}
		if isAdminProc && !principal.IsAdmin {
			return nil, rpcerr.PermissionDenied("admin access required")
		}
		return next(WithPrincipal(ctx, principal), req)
	}
}

// RPCAdminGuard wraps a whole Connect service behind the admin check:
// every procedure authenticates from the bearer access token and the
// principal must be an admin.
func RPCAdminGuard(auth kernel.AccessAuthenticator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := BearerFromHeader(r.Header)
			if !ok {
				writeConnectError(w, "unauthenticated", "bearer token required")
				return
			}
			principal, err := auth.ResolveAccess(r.Context(), token)
			if err != nil {
				// Enumeration-safe: expired, revoked, and unknown
				// tokens are indistinguishable.
				writeConnectError(w, "unauthenticated", "invalid or expired token")
				return
			}
			if !principal.IsAdmin {
				writeConnectError(w, "permission_denied", "admin access required")
				return
			}
			next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), principal)))
		})
	}
}
