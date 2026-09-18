package account

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/go-ozzo/ozzo-validation/v4"
	identityv1 "github.com/riipandi/tango/gen/proto/go/tango/identity/v1"
	identityv1connect "github.com/riipandi/tango/gen/proto/go/tango/identity/v1/identityv1connect"
	"github.com/riipandi/tango/internal/rpcerr"
	"github.com/riipandi/tango/internal/transport/middleware"
	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/modules/identity/password"
	"github.com/riipandi/tango/modules/identity/session"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/pkg/validate"
	"google.golang.org/protobuf/types/known/emptypb"
)

// rfc3339 is the wire timestamp layout.
const rfc3339 = time.RFC3339

// RPCService returns the Connect registration for the self-service
// account surface: every procedure authenticates from the bearer
// access token, so the composition root wraps it with the session
// middleware.
func (s *Service) RPCService() (string, http.Handler) {
	prefix, handler := identityv1connect.NewAccountServiceHandler(&accountRPC{service: s}, rpcerr.RecoverOption())
	return prefix, handler
}

type accountRPC struct {
	service *Service
}

func (h *accountRPC) GetAccount(ctx context.Context, _ *connect.Request[emptypb.Empty]) (*connect.Response[identityv1.User], error) {
	id, err := h.principalID(ctx)
	if err != nil {
		return nil, err
	}
	u, err := h.service.users.GetByID(ctx, id)
	if err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(user.ProtoView(u)), nil
}

func (h *accountRPC) UpdateAccount(ctx context.Context, req *connect.Request[identityv1.UpdateProfileRequest]) (*connect.Response[identityv1.User], error) {
	id, err := h.principalID(ctx)
	if err != nil {
		return nil, err
	}
	params := user.UpdateProfileParams{
		FirstName:   req.Msg.FirstName,
		LastName:    req.Msg.LastName,
		DisplayName: req.Msg.DisplayName,
		AvatarURL:   req.Msg.AvatarUrl,
		Locale:      req.Msg.Locale,
	}
	u, err := h.service.users.UpdateProfile(ctx, id, params)
	if err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(user.ProtoView(u)), nil
}

// accountChangePasswordRequest mirrors the REST payload rules: both
// fields required, minimum length 8.
type accountChangePasswordRequest struct {
	CurrentPassword string
	NewPassword     string
}

func (r accountChangePasswordRequest) Validate() error {
	return validation.ValidateStruct(&r,
		validation.Field(&r.CurrentPassword, validation.Required),
		validation.Field(&r.NewPassword, validation.Required, validation.Length(8, 0)),
	)
}

func (h *accountRPC) ChangePassword(ctx context.Context, req *connect.Request[identityv1.ChangePasswordRequest]) (*connect.Response[emptypb.Empty], error) {
	id, err := h.principalID(ctx)
	if err != nil {
		return nil, err
	}
	creq := accountChangePasswordRequest{CurrentPassword: req.Msg.GetCurrentPassword(), NewPassword: req.Msg.GetNewPassword()}
	if verr := creq.Validate(); verr != nil {
		return nil, validationError(verr)
	}

	principal, _ := middleware.PrincipalFromContext(ctx)
	if err := h.service.changePassword(ctx, id, creq.CurrentPassword, creq.NewPassword, principal.SessionID); err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

func (h *accountRPC) ListSessions(ctx context.Context, _ *connect.Request[emptypb.Empty]) (*connect.Response[identityv1.ListSessionsResponse], error) {
	id, err := h.principalID(ctx)
	if err != nil {
		return nil, err
	}
	sessions, err := h.service.sessions.ListForUser(ctx, id)
	if err != nil {
		return nil, rpcerr.Internal("internal error")
	}
	out := make([]*identityv1.SessionView, 0, len(sessions))
	for _, se := range sessions {
		out = append(out, sessionProtoView(se))
	}
	return connect.NewResponse(&identityv1.ListSessionsResponse{Sessions: out}), nil
}

func (h *accountRPC) RevokeSession(ctx context.Context, req *connect.Request[identityv1.RevokeSessionRequest]) (*connect.Response[emptypb.Empty], error) {
	id, err := h.principalID(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.service.sessions.RevokeForUser(ctx, id, req.Msg.GetSessionId()); err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

// principalID resolves the caller's user ID from the guarded
// context.
func (h *accountRPC) principalID(ctx context.Context) (user.UserID, error) {
	principal, ok := middleware.PrincipalFromContext(ctx)
	if !ok {
		return user.UserID{}, rpcerr.Unauthenticated("authentication required")
	}
	id, err := identity.ParseID[user.UserID](principal.UserID)
	if err != nil {
		return user.UserID{}, rpcerr.Unauthenticated("authentication required")
	}
	return id, nil
}

// sessionProtoView maps the domain session onto the wire message.
func sessionProtoView(se session.Session) *identityv1.SessionView {
	out := &identityv1.SessionView{
		Id:        se.ID,
		Provider:  se.Provider,
		CreatedAt: se.CreatedAt.UTC().Format(rfc3339),
		ExpiresAt: se.ExpiresAt.UTC().Format(rfc3339),
	}
	out.UserAgent = se.UserAgent
	out.DeviceName = se.DeviceName
	out.IpAddress = se.IPAddress
	if se.RefreshedAt != nil {
		at := se.RefreshedAt.UTC().Format(rfc3339)
		out.RefreshedAt = &at
	}
	return out
}

// rpcError maps account domain errors onto Connect codes; wrong
// passwords surface as invalid_argument without leaking whether the
// account exists, and unknown sessions are unauthenticated.
func rpcError(err error) error {
	var cerr *connect.Error
	if errors.As(err, &cerr) {
		return cerr
	}
	switch {
	case errors.Is(err, password.ErrInvalidCredentials):
		return rpcerr.InvalidArgument("current password is incorrect")
	case errors.Is(err, password.ErrWeakPassword):
		return rpcerr.InvalidArgument(err.Error())
	case errors.Is(err, session.ErrNotFound):
		return rpcerr.NotFound("session not found")
	default:
		return rpcerr.Internal("internal error")
	}
}

// validationError maps ozzo field errors onto invalid_argument.
func validationError(verr error) error {
	parts := make([]string, 0, 4)
	for _, fe := range validate.FieldErrors(verr) {
		parts = append(parts, fe.Field+": "+fe.Message)
	}
	return rpcerr.InvalidArgument("validation failed: " + strings.Join(parts, "; "))
}
