package session

import (
	"context"
	"errors"
	"net/http"
	"time"

	"connectrpc.com/connect"
	identityv1 "github.com/riipandi/tango/gen/proto/go/tango/identity/v1"
	identityv1connect "github.com/riipandi/tango/gen/proto/go/tango/identity/v1/identityv1connect"
	"github.com/riipandi/tango/internal/kernel"
	"github.com/riipandi/tango/internal/rpcerr"
	"github.com/riipandi/tango/internal/transport/middleware"
	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/modules/identity/user"
	"google.golang.org/protobuf/types/known/emptypb"
)

// RPCService returns the Connect registration for the auth surface:
// the procedure prefix and the handler. SignIn is public; SignOut and
// GetSession resolve the RPC bearer token and are guarded by the
// interceptor, because a service-wide middleware would lock out the
// anonymous entry point.
func (s *Service) RPCService() (string, http.Handler) {
	protected := map[string]bool{
		identityv1connect.AuthServiceSignOutProcedure:    true,
		identityv1connect.AuthServiceGetSessionProcedure: true,
	}
	prefix, handler := identityv1connect.NewAuthServiceHandler(&authRPC{service: s},
		connect.WithInterceptors(middleware.RPCPrincipalGuard(s, protected, nil)),
		rpcerr.RecoverOption(),
	)
	return prefix, handler
}

type authRPC struct {
	service *Service
}

func (h *authRPC) SignIn(ctx context.Context, req *connect.Request[identityv1.SignInRequest]) (*connect.Response[identityv1.SignedIn], error) {
	identity, secret := req.Msg.GetIdentity(), req.Msg.GetSecret()
	if identity == "" || secret == "" {
		return nil, rpcerr.InvalidArgument("identity and secret are required")
	}

	result, err := h.service.SignInWithPending(ctx, identity, secret, Meta{
		UserAgent: req.Header().Get("User-Agent"),
	})
	if err != nil {
		return nil, rpcerr.Unauthenticated("invalid credentials")
	}

	resp := connect.NewResponse(signedInProto(result.User, result.Session, result.Pending))
	secure := h.service.cookieSecure
	if result.Pending {
		addCookie(resp, pendingCookie(result.Token, secure))
		return resp, nil
	}
	addCookie(resp, sessionCookie(result.Token, result.Session.ExpiresAt, secure))
	// Best-effort mirror: the bridge mints it on the worker's first
	// bootstrap when absent.
	if u, se, err := h.service.Resolve(ctx, result.Token); err == nil {
		if access, expiresAt, err := h.service.IssueAccess(ctx, principalFromResolve(u, se)); err == nil {
			addCookie(resp, accessCookie(access, expiresAt, secure))
		}
	}
	return resp, nil
}

func (h *authRPC) SignOut(ctx context.Context, _ *connect.Request[emptypb.Empty]) (*connect.Response[emptypb.Empty], error) {
	principal, _ := middleware.PrincipalFromContext(ctx)
	userID, err := identity.ParseID[user.UserID](principal.UserID)
	if err != nil {
		return nil, rpcerr.Internal("internal error")
	}
	if err := h.service.RevokeForUser(ctx, userID, principal.SessionID); err != nil {
		return nil, rpcError(err)
	}
	if h.service.mfa != nil {
		_ = h.service.mfa.ClearPending(ctx, principal.UserID)
	}

	resp := connect.NewResponse(&emptypb.Empty{})
	secure := h.service.cookieSecure
	addCookie(resp, expiredCookie(CookieName, "/", secure))
	addCookie(resp, expiredCookie(AccessTokenCookieName, AccessTokenPath, secure))
	addCookie(resp, expiredCookie(identity.PendingCookieName, "/", secure))
	return resp, nil
}

func (h *authRPC) GetSession(ctx context.Context, _ *connect.Request[emptypb.Empty]) (*connect.Response[identityv1.SignedIn], error) {
	principal, _ := middleware.PrincipalFromContext(ctx)
	userID, err := identity.ParseID[user.UserID](principal.UserID)
	if err != nil {
		return nil, rpcerr.Internal("internal error")
	}
	u, err := h.service.users.GetByID(ctx, userID)
	if err != nil {
		return nil, rpcerr.Unauthenticated("invalid or expired session")
	}
	return connect.NewResponse(signedInProto(u, Session{ID: principal.SessionID, Provider: principal.Provider}, false)), nil
}

// ForgotPassword and ResetPassword keep the REST+ConnectRPC duality;
// their Connect cutover lands with the password-flow phase.
func (h *authRPC) ForgotPassword(_ context.Context, _ *connect.Request[identityv1.ForgotPasswordRequest]) (*connect.Response[emptypb.Empty], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("forgot_password: use the REST endpoint"))
}

func (h *authRPC) ResetPassword(_ context.Context, _ *connect.Request[identityv1.ResetPasswordRequest]) (*connect.Response[emptypb.Empty], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("reset_password: use the REST endpoint"))
}

// rpcError maps store sentinels onto Connect codes.
func rpcError(err error) error {
	switch {
	case errors.Is(err, ErrNotFound):
		return rpcerr.NotFound("session not found")
	default:
		return rpcerr.Internal("internal error")
	}
}

// signedInProto maps the domain result onto the shared response.
func signedInProto(u user.User, se Session, pending bool) *identityv1.SignedIn {
	out := &identityv1.SignedIn{
		User:     user.ProtoView(u),
		Pending:  pending,
		Provider: se.Provider,
	}
	if !pending {
		out.SessionId = se.ID
		if !se.ExpiresAt.IsZero() {
			expires := se.ExpiresAt.UTC().Format(time.RFC3339)
			out.ExpiresAt = &expires
		}
	}
	return out
}

// principalFromResolve rebuilds the principal from a Resolve call.
func principalFromResolve(u user.User, se Session) kernel.Principal {
	return kernel.Principal{
		SessionID: se.ID,
		UserID:    u.ID.String(),
		Username:  u.Username,
		Email:     u.Email,
		Provider:  se.Provider,
		IsAdmin:   u.IsAdmin,
	}
}

// addCookie attaches one cookie to the Connect response headers.
func addCookie[M any](resp *connect.Response[M], c *http.Cookie) {
	resp.Header().Add("Set-Cookie", c.String())
}
