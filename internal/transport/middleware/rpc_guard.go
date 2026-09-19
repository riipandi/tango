package middleware

import (
	"context"
	"net/http"
	"strings"

	"connectrpc.com/connect"

	"github.com/riipandi/tango/internal/kernel"
	"github.com/riipandi/tango/internal/rpcerr"
)

// machinePrincipalKey marks a principal resolved from a machine
// credential (X-API-KEY). Only RPCAPIKeyAuth sets it, so a principal
// that a cookie-backed middleware placed in the context never
// satisfies an RPC guard: cookies stay token storage, never RPC
// authorization.
type machinePrincipalKey struct{}

// machinePrincipal returns the principal resolved from a machine
// credential, if the request carried one.
func machinePrincipal(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(machinePrincipalKey{}).(Principal)
	return p, ok
}

// RPCAPIKeyAuth resolves the X-API-KEY header into a principal for
// machine clients on the admin application API. Requests without the
// header pass through untouched so the bearer path keeps serving
// browsers; a request carrying both uses the API key, which keeps the
// machine credential explicit. Unknown, expired, and revoked keys
// answer one indistinguishable Connect unauthenticated error.
func RPCAPIKeyAuth(verify APIKeyVerifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := strings.TrimSpace(r.Header.Get("X-API-KEY"))
			if key == "" {
				next.ServeHTTP(w, r)
				return
			}
			principal, err := verify(r.Context(), key)
			if err != nil {
				writeConnectError(w, "unauthenticated", "invalid API key")
				return
			}
			ctx := context.WithValue(r.Context(), machinePrincipalKey{}, principal)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RPCPrincipalGuard protects selected procedures of a mixed-visibility
// Connect service: procedures named in admin demand an admin
// principal, procedures named in self demand any principal, and
// everything else passes through anonymously. A machine principal
// resolved by RPCAPIKeyAuth satisfies the same checks. Streaming hooks
// are absent by design: the first-party surface is unary only.
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
		principal, err := resolveRPCPrincipal(ctx, i.auth, req.Header())
		if err != nil {
			return nil, err
		}
		if isAdminProc && !principal.IsAdmin {
			return nil, rpcerr.PermissionDenied("admin access required")
		}
		return next(WithPrincipal(ctx, principal), req)
	}
}

// resolveRPCPrincipal prefers the machine principal RPCAPIKeyAuth
// resolved; otherwise it maps the bearer header onto a principal.
func resolveRPCPrincipal(ctx context.Context, auth kernel.AccessAuthenticator, h http.Header) (kernel.Principal, error) {
	if principal, ok := machinePrincipal(ctx); ok {
		return principal, nil
	}
	return ResolveBearer(ctx, auth, h)
}

// ResolveRPCPrincipal maps a Connect request onto a principal for
// handlers that guard their own procedures: a machine credential wins
// when the mount resolved one, otherwise the bearer header decides.
func ResolveRPCPrincipal(ctx context.Context, auth kernel.AccessAuthenticator, h http.Header) (kernel.Principal, error) {
	return resolveRPCPrincipal(ctx, auth, h)
}

// RPCPrincipalAuth requires an authenticated principal — from a
// machine credential or from the bearer access token — without an
// admin requirement. Pair it with RPCAPIKeyAuth at the mount when the
// surface accepts machine clients.
func RPCPrincipalAuth(auth kernel.AccessAuthenticator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			principal, err := resolveRPCPrincipal(r.Context(), auth, r.Header)
			if err != nil {
				writeConnectError(w, "unauthenticated", "invalid or expired token")
				return
			}
			next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), principal)))
		})
	}
}

// RPCMachineDenied rejects machine principals on the named procedures.
// API key self-management uses it so a leaked key cannot mint or renew
// further keys.
func RPCMachineDenied(procedures map[string]bool) connect.Interceptor {
	return machineDenyInterceptor{procedures: procedures}
}

type machineDenyInterceptor struct{ procedures map[string]bool }

func (i machineDenyInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (i machineDenyInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return next
}

func (i machineDenyInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		if i.procedures[req.Spec().Procedure] {
			if _, ok := machinePrincipal(ctx); ok {
				return nil, rpcerr.PermissionDenied("session authentication required")
			}
		}
		return next(ctx, req)
	}
}

// RPCAdminProcedureGuard rejects procedures named in admin unless
// the request context already carries an admin principal — pair it
// with RPCSessionAuth so authentication happens once at the mount.
func RPCAdminProcedureGuard(admin map[string]bool) connect.Interceptor {
	return adminProcedureGuard{admin: admin}
}

type adminProcedureGuard struct{ admin map[string]bool }

func (g adminProcedureGuard) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (g adminProcedureGuard) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return next
}

func (g adminProcedureGuard) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		if g.admin[req.Spec().Procedure] {
			principal, ok := PrincipalFromContext(ctx)
			if !ok || !principal.IsAdmin {
				return nil, rpcerr.PermissionDenied("admin access required")
			}
		}
		return next(ctx, req)
	}
}

// RPCAdminGuard wraps a whole Connect service behind the admin check:
// every procedure authenticates from the bearer access token, or from
// the machine principal RPCAPIKeyAuth resolved, and the principal must
// be an admin.
func RPCAdminGuard(auth kernel.AccessAuthenticator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			principal, ok := machinePrincipal(r.Context())
			if !ok {
				token, found := BearerFromHeader(r.Header)
				if !found {
					writeConnectError(w, "unauthenticated", "bearer token required")
					return
				}
				resolved, err := auth.ResolveAccess(r.Context(), token)
				if err != nil {
					// Enumeration-safe: expired, revoked, and unknown
					// tokens are indistinguishable.
					writeConnectError(w, "unauthenticated", "invalid or expired token")
					return
				}
				principal = resolved
			}
			if !principal.IsAdmin {
				writeConnectError(w, "permission_denied", "admin access required")
				return
			}
			next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), principal)))
		})
	}
}
