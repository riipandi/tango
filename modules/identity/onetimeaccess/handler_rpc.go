package onetimeaccess

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"connectrpc.com/connect"
	identityv1 "github.com/riipandi/tango/gen/proto/go/tango/identity/v1"
	identityv1connect "github.com/riipandi/tango/gen/proto/go/tango/identity/v1/identityv1connect"
	"github.com/riipandi/tango/internal/kernel"
	"github.com/riipandi/tango/internal/rpcerr"
	"github.com/riipandi/tango/internal/transport/middleware"
	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/pkg/validate"
	"google.golang.org/protobuf/types/known/emptypb"
)

// RPCService returns the Connect registration for the one-time
// access surface. RequestEmail is anonymous (enumeration-safe 204);
// the admin procedures resolve the bearer and demand an admin.
// The token exchange itself stays the REST email-link endpoint.
func (s *Service) RPCService(access kernel.AccessAuthenticator) (string, http.Handler) {
	admin := map[string]bool{
		identityv1connect.OneTimeAccessServiceAdminSendEmailProcedure:  true,
		identityv1connect.OneTimeAccessServiceAdminIssueTokenProcedure: true,
	}
	prefix, handler := identityv1connect.NewOneTimeAccessServiceHandler(&otaRPC{service: s},
		connect.WithInterceptors(middleware.RPCPrincipalGuard(access, admin, nil)),
		rpcerr.RecoverOption(),
	)
	return prefix, handler
}

type otaRPC struct {
	service *Service
}

func (h *otaRPC) RequestEmail(ctx context.Context, req *connect.Request[identityv1.RequestEmailRequest]) (*connect.Response[emptypb.Empty], error) {
	// Same contract as the retired REST endpoint: validate the body,
	// answer 204-equivalent unconditionally — the policy that decides
	// whether a token is minted belongs to appconfig, and a constant
	// answer avoids account enumeration.
	if err := validateEmail(req.Msg.GetIdentity()); err != nil {
		return nil, validationError(err)
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

func (h *otaRPC) AdminSendEmail(ctx context.Context, req *connect.Request[identityv1.AdminSendEmailRequest]) (*connect.Response[emptypb.Empty], error) {
	userID, err := targetID(req.Msg.GetUserId())
	if err != nil {
		return nil, err
	}
	u, err := h.service.users.GetByID(ctx, userID)
	if err != nil {
		return nil, rpcError(err)
	}
	raw, err := h.service.mint(ctx, userID)
	if err != nil {
		return nil, rpcerr.Internal("failed to create token")
	}
	if err := h.service.sendAccessEmail(ctx, u, raw, ""); err != nil {
		return nil, rpcerr.Internal("failed to queue email")
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

func (h *otaRPC) AdminIssueToken(ctx context.Context, req *connect.Request[identityv1.AdminIssueTokenRequest]) (*connect.Response[identityv1.AdminIssueTokenResponse], error) {
	userID, err := targetID(req.Msg.GetUserId())
	if err != nil {
		return nil, err
	}
	if _, lookupErr := h.service.users.GetByID(ctx, userID); lookupErr != nil {
		return nil, rpcError(lookupErr)
	}
	raw, err := h.service.mint(ctx, userID)
	if err != nil {
		return nil, rpcerr.Internal("failed to create token")
	}
	h.service.record(ctx, "one_time_access.token_created", userID.String())
	return connect.NewResponse(&identityv1.AdminIssueTokenResponse{Token: raw}), nil
}

// targetID parses a path user ID; TypeID-only, unknown shapes 404.
func targetID(raw string) (user.UserID, error) {
	id, err := identity.ParseID[user.UserID](raw)
	if err != nil {
		return user.UserID{}, rpcerr.NotFound("user not found")
	}
	return id, nil
}

// rpcError maps one-time access sentinels onto Connect codes.
func rpcError(err error) error {
	var cerr *connect.Error
	if errors.As(err, &cerr) {
		return cerr
	}
	if errors.Is(err, ErrNotFound) || errors.Is(err, user.ErrNotFound) {
		return rpcerr.NotFound("user not found")
	}
	return rpcerr.Internal("internal error")
}

// validationError maps ozzo field errors onto invalid_argument.
func validationError(verr error) error {
	parts := make([]string, 0, 4)
	for _, fe := range validate.FieldErrors(verr) {
		parts = append(parts, fe.Field+": "+fe.Message)
	}
	return rpcerr.InvalidArgument("validation failed: " + strings.Join(parts, "; "))
}
