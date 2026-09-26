package session

import (
	"context"
	"errors"
	"math"
	"time"

	"connectrpc.com/connect"
	"github.com/go-chi/chi/v5"

	authv1 "github.com/riipandi/tango/codegen/proto/go/tango/auth/v1"
	authv1connect "github.com/riipandi/tango/codegen/proto/go/tango/auth/v1/authv1connect"
	commonv1 "github.com/riipandi/tango/codegen/proto/go/tango/common/v1"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/pkg/jwtutils"
	"github.com/riipandi/tango/pkg/responder"
)

// ModuleName is the name this feature reports under. The area it belongs to
// qualifies it, so the name is the feature alone.
const ModuleName = "session"

// Module serves the session lifecycle procedures: the RPC surface, all of it.
// Authentication is the transport's middleware — the module reads the caller
// the context carries, it never verifies a token itself.
type Module struct {
	service *Service
}

// NewModule builds the module over the session service.
func NewModule(service *Service) *Module {
	return &Module{service: service}
}

// Name reports the module in composition reports.
func (m *Module) Name() string { return ModuleName }

// Mount registers the module's endpoints on the router. The surface is RPC
// alone — a session is managed through the API the token authenticates — so
// there is nothing on the HTTP router to claim.
func (m *Module) Mount(r chi.Router) {}

// MountRPC registers the procedures on the RPC router. The handler options
// are the transport's — the shared snake_case codec and the panic boundary —
// so the procedures answer exactly like the transport's own. Each procedure
// is registered at its own path: the generated handler answers a path under
// its prefix it does not know with a plain-text 404, which a Connect client
// cannot read.
func (m *Module) MountRPC(r chi.Router, opts ...connect.HandlerOption) {
	_, handler := authv1connect.NewSessionServiceHandler(newRPCHandler(m.service), opts...)
	r.Handle(authv1connect.SessionServiceSignOutProcedure, handler)
	r.Handle(authv1connect.SessionServiceGetSessionProcedure, handler)
	r.Handle(authv1connect.SessionServiceListSessionsProcedure, handler)
	r.Handle(authv1connect.SessionServiceRevokeSessionProcedure, handler)
	r.Handle(authv1connect.SessionServiceRefreshProcedure, handler)
}

// rpcHandler is the transport mapping of the procedures. The service carries
// the rules; this type carries the connect codes and the caller's identity:
// every procedure acts on the session the claims name, which the guard's
// session rule has already established is present.
type rpcHandler struct {
	service *Service
}

// newRPCHandler builds the handler over the service.
func newRPCHandler(service *Service) authv1connect.SessionServiceHandler {
	return &rpcHandler{service: service}
}

// sessionCaller reads the caller the transport's middleware resolved. The
// guard's session rule has already refused a machine credential, an
// impersonated caller, and a token without the session claim, so reaching
// here means the claims name a live session; a missing caller is the wiring
// defect it always is, and it is refused rather than dereferenced.
func sessionCaller(ctx context.Context) *jwtutils.Caller {
	caller, ok := jwtutils.CallerFrom(ctx)
	if !ok || caller == nil || caller.SessionID == "" {
		return nil
	}
	return caller
}

// SignOut ends the session the access token names.
func (h *rpcHandler) SignOut(ctx context.Context, req *connect.Request[authv1.SignOutRequest]) (*connect.Response[authv1.SignOutResponse], error) {
	caller := sessionCaller(ctx)
	if caller == nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("authentication state missing"))
	}

	if err := h.service.SignOut(ctx, caller.SessionID, caller.UserID); err != nil {
		return nil, mapError(err)
	}
	return connect.NewResponse(&authv1.SignOutResponse{
		Status:  responder.StatusSuccess,
		Message: "the session was signed out",
	}), nil
}

// GetSession answers the session the access token names.
func (h *rpcHandler) GetSession(ctx context.Context, req *connect.Request[authv1.GetSessionRequest]) (*connect.Response[authv1.GetSessionResponse], error) {
	caller := sessionCaller(ctx)
	if caller == nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("authentication state missing"))
	}

	row, view, err := h.service.GetSession(ctx, caller.SessionID)
	if err != nil {
		return nil, mapError(err)
	}
	return connect.NewResponse(&authv1.GetSessionResponse{
		Session: wireSession(row, caller.SessionID),
		User:    wireUser(view),
		Status:  responder.StatusSuccess,
		Message: "the session was fetched",
	}), nil
}

// ListSessions answers one page of the account's sessions.
func (h *rpcHandler) ListSessions(ctx context.Context, req *connect.Request[authv1.ListSessionsRequest]) (*connect.Response[authv1.ListSessionsResponse], error) {
	caller := sessionCaller(ctx)
	if caller == nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("authentication state missing"))
	}

	rows, pagination, err := h.service.ListSessions(ctx, caller.UserID, int(req.Msg.GetPage()), int(req.Msg.GetLimit()))
	if err != nil {
		return nil, mapError(err)
	}

	views := make([]*authv1.Session, 0, len(rows))
	for _, row := range rows {
		views = append(views, wireSession(row, caller.SessionID))
	}
	return connect.NewResponse(&authv1.ListSessionsResponse{
		Sessions: views,
		Metadata: metadataOf(pagination),
		Status:   responder.StatusSuccess,
		Message:  "the sessions were listed",
	}), nil
}

// RevokeSession ends one of the account's sessions.
func (h *rpcHandler) RevokeSession(ctx context.Context, req *connect.Request[authv1.RevokeSessionRequest]) (*connect.Response[authv1.RevokeSessionResponse], error) {
	caller := sessionCaller(ctx)
	if caller == nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("authentication state missing"))
	}

	if err := h.service.RevokeSession(ctx, caller.SessionID, caller.UserID, req.Msg.Id); err != nil {
		return nil, mapError(err)
	}
	return connect.NewResponse(&authv1.RevokeSessionResponse{
		Status:  responder.StatusSuccess,
		Message: "the session was revoked",
	}), nil
}

// Refresh exchanges the refresh token for a fresh pair. The procedure rides
// the session guard like its siblings, but the credential it spends is the
// body's refresh token, not the caller's session — a client rotating its
// token on a schedule may hold an access token whose session is the very row
// the body names.
func (h *rpcHandler) Refresh(ctx context.Context, req *connect.Request[authv1.RefreshRequest]) (*connect.Response[authv1.RefreshResponse], error) {
	refreshed, err := h.service.Refresh(ctx, req.Msg.RefreshToken)
	if err != nil {
		return nil, mapError(err)
	}
	return connect.NewResponse(&authv1.RefreshResponse{
		AccessToken:  refreshed.AccessToken,
		TokenType:    refreshed.TokenType,
		ExpiresIn:    refreshed.ExpiresIn,
		RefreshToken: refreshed.RefreshToken,
		SessionId:    refreshed.SessionID,
		User:         wireUser(refreshed.User),
		Status:       responder.StatusSuccess,
		Message:      "the token pair was refreshed",
	}), nil
}

// wireSession maps the stored row onto the wire message. The request facts
// are what the row stored when it was opened, and `current` is the one field
// the row cannot answer — it is the caller's question, not the row's.
func wireSession(row SessionSchema, current string) *authv1.Session {
	session := &authv1.Session{
		Id:        row.ID.String(),
		Provider:  row.Provider,
		Remember:  row.Remember,
		CreatedAt: row.CreatedAt.Format(time.RFC3339),
		ExpiresAt: row.ExpiresAt.Format(time.RFC3339),
		Current:   row.ID.String() == current,
	}
	if row.UserAgent != "" {
		session.UserAgent = &row.UserAgent
	}
	if row.IPAddress != nil {
		session.IpAddress = new(row.IPAddress.String())
	}
	if row.RefreshedAt != nil {
		session.RefreshedAt = new(row.RefreshedAt.Format(time.RFC3339))
	}
	if row.RevokedAt != nil {
		session.RevokedAt = new(row.RevokedAt.Format(time.RFC3339))
	}
	return session
}

// wireUser maps the account view onto the tokens' account view, the shape the
// sign-in answered with.
func wireUser(view user.UserView) *authv1.AuthenticatedUser {
	return &authv1.AuthenticatedUser{
		Id:          view.ID,
		Username:    view.Username,
		Email:       view.Email,
		DisplayName: view.DisplayName,
		IsAdmin:     view.IsAdmin,
	}
}

// metadataOf maps the responder's pagination onto the shared block. The
// wire fields are optional, so an unknown range is absent rather than zero.
func metadataOf(p responder.Pagination) *commonv1.ListMetadata {
	meta := &commonv1.ListMetadata{}
	set := func(dst **int32, src *int) {
		if src == nil {
			return
		}
		// The wire field is int32; a total beyond it saturates rather than
		// wrapping, and no page the rules allow can reach the bound.
		value := *src
		if value > math.MaxInt32 || value < math.MinInt32 {
			value = math.MaxInt32
		}
		narrowed := int32(value)
		*dst = &narrowed
	}
	set(&meta.Page, p.Page)
	set(&meta.Limit, p.Limit)
	set(&meta.TotalPages, p.TotalPages)
	set(&meta.TotalItems, p.TotalItems)
	set(&meta.FirstItemIndex, p.FirstItemIndex)
	set(&meta.LastItemIndex, p.LastItemIndex)
	return meta
}

// mapError translates the service's failures into the codes the Connect
// protocol carries. The internal ones are collapsed to one answer whose text
// names nothing a caller could aim at. A malformed field never reaches the
// service: the transport's validate interceptor refuses it with the typed
// violation details the contracts carry.
func mapError(err error) error {
	switch {
	case errors.Is(err, ErrSessionEnded):
		return connect.NewError(connect.CodeUnauthenticated, errors.New("the session has ended"))
	default:
		return connect.NewError(connect.CodeInternal, errors.New("session operation failed"))
	}
}
