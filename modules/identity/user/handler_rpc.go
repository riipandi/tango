package user

import (
	"context"
	"errors"
	"math"

	"connectrpc.com/authn"
	"connectrpc.com/connect"
	"github.com/go-chi/chi/v5"

	commonv1 "github.com/riipandi/tango/codegen/proto/go/tango/common/v1"
	identityv1 "github.com/riipandi/tango/codegen/proto/go/tango/identity/v1"
	identityv1connect "github.com/riipandi/tango/codegen/proto/go/tango/identity/v1/identityv1connect"
	"github.com/riipandi/tango/pkg/jwtutils"
	"github.com/riipandi/tango/pkg/responder"
)

// ModuleName is the name this feature reports under. The area it belongs to
// qualifies it, so the name is the feature alone.
const ModuleName = "user"

// Module serves the account administration procedures. Everything it answers
// is an RPC procedure, so its HTTP mount is empty by construction.
type Module struct {
	service *Service
}

// NewModule builds the module over the account service.
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
// so the procedures answer exactly like the transport's own. Each procedure
// is registered at its own path: the generated handler answers a path under
// its prefix it does not know with a plain-text 404, which a Connect client
// cannot read.
func (m *Module) MountRPC(r chi.Router, opts ...connect.HandlerOption) {
	_, handler := identityv1connect.NewUserServiceHandler(newRPCHandler(m.service), opts...)
	r.Handle(identityv1connect.UserServiceListUsersProcedure, handler)
	r.Handle(identityv1connect.UserServiceGetUserProcedure, handler)
	r.Handle(identityv1connect.UserServiceCreateUserProcedure, handler)
	r.Handle(identityv1connect.UserServiceUpdateUserProcedure, handler)
	r.Handle(identityv1connect.UserServiceDeleteUserProcedure, handler)
}

// rpcHandler is the transport mapping of the procedures. The service carries
// the rules; this type carries the connect codes.
type rpcHandler struct {
	service *Service
}

// newRPCHandler builds the handler over the service.
func newRPCHandler(service *Service) identityv1connect.UserServiceHandler {
	return &rpcHandler{service: service}
}

// ListUsers answers one page of the accounts.
func (h *rpcHandler) ListUsers(ctx context.Context, req *connect.Request[identityv1.ListUsersRequest]) (*connect.Response[identityv1.ListUsersResponse], error) {
	if _, ok := adminFrom(ctx); !ok {
		return nil, connect.NewError(connect.CodePermissionDenied, errAdminRequired)
	}

	users, pagination, err := h.service.ListUsers(ctx, req.Msg.GetSearch(), int(req.Msg.GetPage()), int(req.Msg.GetLimit()))
	if err != nil {
		return nil, mapError(err)
	}

	views := make([]*identityv1.User, 0, len(users))
	for _, user := range users {
		views = append(views, WireView(user))
	}
	return connect.NewResponse(&identityv1.ListUsersResponse{
		Users:    views,
		Metadata: listMetadata(pagination),
	}), nil
}

// GetUser answers one account.
func (h *rpcHandler) GetUser(ctx context.Context, req *connect.Request[identityv1.GetUserRequest]) (*connect.Response[identityv1.GetUserResponse], error) {
	if _, ok := adminFrom(ctx); !ok {
		return nil, connect.NewError(connect.CodePermissionDenied, errAdminRequired)
	}

	user, err := h.service.GetUser(ctx, req.Msg.Id)
	if err != nil {
		return nil, mapError(err)
	}
	return connect.NewResponse(&identityv1.GetUserResponse{User: WireView(user)}), nil
}

// CreateUser creates an account directly, without a signup token.
func (h *rpcHandler) CreateUser(ctx context.Context, req *connect.Request[identityv1.CreateUserRequest]) (*connect.Response[identityv1.CreateUserResponse], error) {
	if _, ok := adminFrom(ctx); !ok {
		return nil, connect.NewError(connect.CodePermissionDenied, errAdminRequired)
	}

	body := req.Msg
	user, err := h.service.CreateUser(ctx, CreateParams{
		Username:      body.Username,
		Email:         body.Email,
		Password:      body.GetPassword(),
		FirstName:     body.GetFirstName(),
		LastName:      body.GetLastName(),
		DisplayName:   body.GetDisplayName(),
		Locale:        body.GetLocale(),
		IsAdmin:       body.GetIsAdmin(),
		Disabled:      body.GetDisabled(),
		EmailVerified: body.GetEmailVerified(),
	})
	if err != nil {
		return nil, mapError(err)
	}
	return connect.NewResponse(&identityv1.CreateUserResponse{User: WireView(user)}), nil
}

// UpdateUser replaces an account's fields.
func (h *rpcHandler) UpdateUser(ctx context.Context, req *connect.Request[identityv1.UpdateUserRequest]) (*connect.Response[identityv1.UpdateUserResponse], error) {
	if _, ok := adminFrom(ctx); !ok {
		return nil, connect.NewError(connect.CodePermissionDenied, errAdminRequired)
	}

	body := req.Msg
	params := UpdateParams{
		Username:    body.Username,
		Email:       body.Email,
		FirstName:   body.FirstName,
		LastName:    body.LastName,
		DisplayName: body.DisplayName,
		Locale:      body.Locale,
		IsAdmin:     body.IsAdmin,
		Disabled:    body.Disabled,
		BanReason:   optional(body.GetBanReason()),
	}
	if body.BanExpires != nil {
		at := body.BanExpires.AsTime()
		params.BanExpiresAt = &at
	}
	user, err := h.service.UpdateUser(ctx, body.Id, params)
	if err != nil {
		return nil, mapError(err)
	}
	return connect.NewResponse(&identityv1.UpdateUserResponse{User: WireView(user)}), nil
}

// DeleteUser removes an account.
func (h *rpcHandler) DeleteUser(ctx context.Context, req *connect.Request[identityv1.DeleteUserRequest]) (*connect.Response[identityv1.DeleteUserResponse], error) {
	claims, ok := adminFrom(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodePermissionDenied, errAdminRequired)
	}

	if err := h.service.DeleteUser(ctx, req.Msg.Id, claims.Username); err != nil {
		return nil, mapError(err)
	}
	return connect.NewResponse(&identityv1.DeleteUserResponse{}), nil
}

// listMetadata maps the responder's pagination onto the shared block. The
// wire fields are optional, so an unknown range is absent rather than zero.
func listMetadata(p responder.Pagination) *commonv1.ListMetadata {
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
		*dst = new(int32(value))
	}
	set(&meta.Page, p.Page)
	set(&meta.Limit, p.Limit)
	set(&meta.TotalPages, p.TotalPages)
	set(&meta.TotalItems, p.TotalItems)
	set(&meta.FirstItemIndex, p.FirstItemIndex)
	set(&meta.LastItemIndex, p.LastItemIndex)
	return meta
}

// adminFrom reads the authenticated caller's claims the bearer middleware
// attached, and reports whether the caller holds the administrator role.
func adminFrom(ctx context.Context) (*jwtutils.AccessClaims, bool) {
	claims, ok := authn.GetInfo(ctx).(*jwtutils.AccessClaims)
	return claims, ok && claims.IsAdmin
}

// errAdminRequired is the refusal a caller without the administrator role reads.
var errAdminRequired = errors.New("administrator role required")

// mapError translates the service's failures into the codes the Connect
// protocol carries. The internal ones are collapsed to one answer whose text
// names nothing a caller could aim at. A malformed field never reaches the
// service: the transport's validate interceptor refuses it with the typed
// violation details the contracts carry.
func mapError(err error) error {
	switch {
	case errors.Is(err, ErrUserNotFound):
		return connect.NewError(connect.CodeNotFound, errors.New("user not found"))
	case errors.Is(err, ErrAccountExists):
		return connect.NewError(connect.CodeAlreadyExists, errors.New("account already exists"))
	case errors.Is(err, ErrSelfDeletion):
		return connect.NewError(connect.CodeFailedPrecondition, errors.New("an administrator cannot delete the account they are signed in with"))
	default:
		return connect.NewError(connect.CodeInternal, errors.New("user operation failed"))
	}
}
