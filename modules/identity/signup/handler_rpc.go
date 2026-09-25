package signup

import (
	"context"
	"errors"
	"strings"

	"connectrpc.com/connect"
	"github.com/go-chi/chi/v5"

	identityv1 "github.com/riipandi/tango/codegen/proto/go/tango/identity/v1"
	identityv1connect "github.com/riipandi/tango/codegen/proto/go/tango/identity/v1/identityv1connect"
	"github.com/riipandi/tango/pkg/validate"
)

// ModuleName is the name this feature reports under. The area it belongs to
// qualifies it, so the name is the feature alone.
const ModuleName = "signup"

// Module serves the sign-up procedure. Everything it answers is an RPC
// procedure, so its HTTP mount is empty by construction.
type Module struct {
	service *Service
}

// NewModule builds the module over the sign-up service.
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
	_, handler := identityv1connect.NewSignupServiceHandler(newRPCHandler(m.service), opts...)
	r.Handle(identityv1connect.SignupServiceSignupProcedure, handler)
}

// rpcHandler is the transport mapping of the sign-up procedure. The service
// carries the rules; this type carries the connect codes.
type rpcHandler struct {
	service *Service
}

// newRPCHandler builds the handler over the service.
func newRPCHandler(service *Service) identityv1connect.SignupServiceHandler {
	return &rpcHandler{service: service}
}

// Signup creates an account from a signup token.
func (h *rpcHandler) Signup(ctx context.Context, req *connect.Request[identityv1.SignupRequest]) (*connect.Response[identityv1.SignupResponse], error) {
	body := req.Msg
	if body.Username == "" || body.Email == "" || body.Password == "" || body.Token == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("username, email, password, and token are required"))
	}

	user, err := h.service.Signup(ctx, Params{
		Username:  body.Username,
		Email:     body.Email,
		Password:  body.Password,
		Token:     body.Token,
		FirstName: body.GetFirstName(),
		LastName:  body.GetLastName(),
	})
	if err != nil {
		return nil, mapError(err)
	}

	return connect.NewResponse(&identityv1.SignupResponse{
		User: &identityv1.User{
			Id:          user.ID,
			Username:    user.Username,
			Email:       user.Email,
			DisplayName: user.DisplayName,
		},
	}), nil
}

// mapError translates the service's failures into the codes the Connect
// protocol carries. The internal ones are collapsed to one answer whose text
// names nothing a caller could aim at.
func mapError(err error) error {
	switch {
	case validate.IsValidationError(err):
		return connect.NewError(connect.CodeInvalidArgument, errors.New(fieldMessage(err)))
	case errors.Is(err, ErrInvalidToken):
		return connect.NewError(connect.CodePermissionDenied, errors.New("signup token is invalid or expired"))
	case errors.Is(err, ErrAccountExists):
		return connect.NewError(connect.CodeAlreadyExists, errors.New("account already exists"))
	default:
		return connect.NewError(connect.CodeInternal, errors.New("sign-up failed"))
	}
}

// fieldMessage flattens the validation failures into one wire detail, each
// field leading its rule.
func fieldMessage(err error) string {
	parts := make([]string, 0, 2)
	for _, fe := range validate.FieldErrors(err) {
		parts = append(parts, fe.Field+": "+fe.Message)
	}
	return strings.Join(parts, "; ")
}
