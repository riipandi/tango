package signin

import (
	"context"
	"errors"
	"github.com/riipandi/tango/pkg/responder"
	"net"
	"net/http"

	"connectrpc.com/connect"
	"github.com/go-chi/chi/v5"

	authv1 "github.com/riipandi/tango/codegen/proto/go/tango/auth/v1"
	authv1connect "github.com/riipandi/tango/codegen/proto/go/tango/auth/v1/authv1connect"
	"github.com/riipandi/tango/modules/identity/jwks"
)

// ModuleName is the name this feature reports under. The area it belongs to
// qualifies it, so the name is the feature alone.
const ModuleName = "signin"

// Module serves the sign-in procedures. Everything it answers is an RPC
// procedure, so its HTTP mount is empty by construction.
type Module struct {
	service *Service
}

// NewModule builds the module over the sign-in service.
func NewModule(service *Service) *Module {
	return &Module{service: service}
}

// Name reports the module in composition reports.
func (m *Module) Name() string { return ModuleName }

// Mount registers the endpoints on the HTTP router. The feature serves no
// plain HTTP route: a procedure is POST-only on the RPC surface.
func (m *Module) Mount(r chi.Router) {}

// MountRPC registers the procedures on the RPC router. The handler options
// are the transport's — the shared snake_case codec and the panic boundary —
// so the procedure answers exactly like the transport's own.
func (m *Module) MountRPC(r chi.Router, opts ...connect.HandlerOption) {
	_, handler := authv1connect.NewAuthServiceHandler(newRPCHandler(m.service), opts...)
	r.Handle(authv1connect.AuthServiceSignInProcedure, clientAddrMiddleware(handler))
}

// rpcHandler is the transport mapping of the sign-in procedures. The service
// carries the rules; this type carries the connect codes and the request
// facts the protocol supplies on its own.
type rpcHandler struct {
	service *Service
}

// newRPCHandler builds the handler over the service.
func newRPCHandler(service *Service) authv1connect.AuthServiceHandler {
	return &rpcHandler{service: service}
}

// SignIn verifies the credential and answers the token pair.
func (h *rpcHandler) SignIn(ctx context.Context, req *connect.Request[authv1.SignInRequest]) (*connect.Response[authv1.SignInResponse], error) {
	body := req.Msg
	if body.Identity == "" || body.Password == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("identity and password are required"))
	}

	result, err := h.service.SignIn(ctx, Params{
		Identity:  body.Identity,
		Password:  body.Password,
		Remember:  body.GetRemember(),
		UserAgent: req.Header().Get("User-Agent"),
		IPAddress: peerIP(ctx),
	})
	if err != nil {
		return nil, mapError(err)
	}

	return connect.NewResponse(&authv1.SignInResponse{
		AccessToken:  result.AccessToken,
		TokenType:    result.TokenType,
		ExpiresIn:    result.ExpiresIn,
		RefreshToken: result.RefreshToken,
		SessionId:    result.SessionID,
		User: &authv1.AuthenticatedUser{
			Id:          result.User.ID,
			Username:    result.User.Username,
			Email:       result.User.Email,
			DisplayName: result.User.DisplayName,
			IsAdmin:     result.User.IsAdmin,
		},

		Status:  responder.StatusSuccess,
		Message: "the token pair was issued",
	}), nil
}

// mapError translates the service's failures into the codes the Connect
// protocol carries. The internal ones are collapsed to one answer whose text
// names nothing a caller could aim at.
func mapError(err error) error {
	switch {
	case errors.Is(err, ErrInvalidCredentials):
		return connect.NewError(connect.CodeUnauthenticated, errors.New("invalid credentials"))
	case errors.Is(err, ErrAccountDisabled):
		return connect.NewError(connect.CodePermissionDenied, errors.New("account is disabled"))
	case errors.Is(err, ErrAccountBanned):
		return connect.NewError(connect.CodePermissionDenied, errors.New("account is banned"))
	case errors.Is(err, jwks.ErrNoSigningKey):
		return connect.NewError(connect.CodeInternal, errors.New("sign-in is not answerable"))
	default:
		return connect.NewError(connect.CodeInternal, errors.New("sign-in failed"))
	}
}

// clientAddrKey is the context key the mount middleware stores the caller's
// address under. Connect hands a unary handler the request context but no
// connection, so the address is read into the context before it arrives.
type ctxKey int

const clientAddrKey ctxKey = 0

// peerIP reads the caller's address the mount middleware captured. The port
// is stripped there; an unknown address is no address, not an error.
func peerIP(ctx context.Context) string {
	if addr, ok := ctx.Value(clientAddrKey).(string); ok {
		return addr
	}
	return ""
}

// clientAddrMiddleware captures the host part of the request's remote
// address. A proxy deployment sees the proxy's address, which is the honest
// answer until a trusted-proxy setting decides to look further.
func clientAddrMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		addr := r.RemoteAddr
		if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
			addr = host
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), clientAddrKey, addr)))
	})
}
