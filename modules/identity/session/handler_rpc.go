package session

import (
	"context"
	"errors"
	"net/http"
	"time"

	"connectrpc.com/connect"
	identityv1 "github.com/riipandi/tango/codegen/proto/go/tango/identity/v1"
	identityv1connect "github.com/riipandi/tango/codegen/proto/go/tango/identity/v1/identityv1connect"
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
	opts := append(rpcerr.Options(), connect.WithInterceptors(middleware.RPCPrincipalGuard(s, protected, nil)))
	prefix, handler := identityv1connect.NewAuthServiceHandler(&authRPC{service: s}, opts...)
	return prefix, handler
}

type authRPC struct {
	service *Service
}

func (h *authRPC) SignIn(ctx context.Context, req *connect.Request[identityv1.SignInRequest]) (*connect.Response[identityv1.SignedIn], error) {
	identity, password := req.Msg.GetIdentity(), req.Msg.GetPassword()
	if identity == "" || password == "" {
		return nil, rpcerr.InvalidArgument("identity and password are required")
	}

	result, err := h.service.SignInWithPending(ctx, identity, password, Meta{
		UserAgent: req.Header().Get("User-Agent"),
		Remember:  req.Msg.GetRemember(),
	})
	if err != nil {
		return nil, rpcerr.Unauthenticated("invalid credentials")
	}

	resp := connect.NewResponse(signedInProto(result.User, result.Session, result.Pending))
	if result.Pending {
		resp.Msg.PendingToken = &result.Token
		return resp, nil
	}
	resp.Msg.SessionToken = result.Token
	// Best-effort mirror: the client refreshes through the bridge when
	// the bearer expires.
	if u, se, err := h.service.Resolve(ctx, result.Token); err == nil {
		if access, _, err := h.service.IssueAccess(ctx, principalFromResolve(u, se)); err == nil {
			resp.Msg.AccessToken = access
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
	return connect.NewResponse(&emptypb.Empty{}), nil
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

// ForgotPassword and ResetPassword stay REST-only: recovery is an
// unauthenticated email flow the SPA reaches through the retained
// HTTP routes, so the declared RPC procedures answer unimplemented
// with a pointer to the live contract.
func (h *authRPC) ForgotPassword(_ context.Context, _ *connect.Request[identityv1.ForgotPasswordRequest]) (*connect.Response[emptypb.Empty], error) {
	return nil, rpcerr.Unimplemented("password recovery serves the retained HTTP endpoints")
}

func (h *authRPC) ResetPassword(_ context.Context, _ *connect.Request[identityv1.ResetPasswordRequest]) (*connect.Response[emptypb.Empty], error) {
	return nil, rpcerr.Unimplemented("password recovery serves the retained HTTP endpoints")
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
		Remember: se.Remember,
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
