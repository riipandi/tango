package emailverification

import (
	"context"
	"net/http"

	"connectrpc.com/connect"
	identityv1connect "github.com/riipandi/tango/gen/proto/go/tango/identity/v1/identityv1connect"
	"github.com/riipandi/tango/internal/kernel"
	"github.com/riipandi/tango/internal/rpcerr"
	"github.com/riipandi/tango/internal/transport/middleware"
	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/modules/identity/user"
	"google.golang.org/protobuf/types/known/emptypb"
)

// RPCService returns the Connect registration for the email
// verification surface: every procedure is self-service and resolves
// the bearer. The verify step stays the email-link REST endpoint.
func (s *Service) RPCService(access kernel.AccessAuthenticator) (string, http.Handler) {
	self := map[string]bool{
		identityv1connect.EmailVerificationServiceSendEmailProcedure: true,
	}
	prefix, handler := identityv1connect.NewEmailVerificationServiceHandler(&emailvRPC{service: s},
		connect.WithInterceptors(middleware.RPCPrincipalGuard(access, nil, self)),
		rpcerr.RecoverOption(),
	)
	return prefix, handler
}

type emailvRPC struct {
	service *Service
}

func (h *emailvRPC) SendEmail(ctx context.Context, _ *connect.Request[emptypb.Empty]) (*connect.Response[emptypb.Empty], error) {
	principal, ok := middleware.PrincipalFromContext(ctx)
	if !ok {
		return nil, rpcerr.Unauthenticated("authentication required")
	}
	userID, err := identity.ParseID[user.UserID](principal.UserID)
	if err != nil {
		return nil, rpcerr.Unauthenticated("authentication required")
	}

	raw, err := h.service.mint(ctx, userID)
	if err != nil {
		return nil, rpcerr.Internal("failed to create verification token")
	}
	if err := h.service.sendVerificationEmail(ctx, userID, raw); err != nil {
		return nil, rpcerr.Internal("failed to queue email")
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}
