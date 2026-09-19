package devicelogin

import (
	"context"
	"net/http"
	"time"

	"connectrpc.com/connect"
	identityv1 "github.com/riipandi/tango/codegen/proto/go/tango/identity/v1"
	identityv1connect "github.com/riipandi/tango/codegen/proto/go/tango/identity/v1/identityv1connect"
	"github.com/riipandi/tango/internal/rpcerr"
	"github.com/riipandi/tango/internal/transport/middleware"
	"google.golang.org/protobuf/types/known/emptypb"
)

// approvalRPC adapts the approval service to the generated Connect
// contract. Both procedures demand a signed-in principal — the
// composition root wraps the mount with the bearer middleware.
type approvalRPC struct {
	service *Service
}

// RPCService returns the Connect registration for the device approval
// surface.
func (f Feature) RPCService() (string, http.Handler) {
	prefix, handler := identityv1connect.NewDeviceApprovalServiceHandler(&approvalRPC{service: f.service}, rpcerr.Options()...)
	return prefix, handler
}

func (h *approvalRPC) GetPendingRequest(ctx context.Context, req *connect.Request[identityv1.GetPendingRequestRequest]) (*connect.Response[identityv1.VerificationInfo], error) {
	code := req.Msg.GetUserCode()
	if normalizeCode(code) == "" {
		return nil, rpcerr.InvalidArgument("user_code cannot be blank")
	}
	info, err := h.service.Inspect(ctx, code)
	if err != nil {
		return nil, rpcerr.NotFound("device login request is invalid or expired")
	}
	out := &identityv1.VerificationInfo{
		UserCode:  info.UserCode,
		Device:    info.Device,
		IpAddress: info.IPAddress,
		ExpiresAt: info.ExpiresAt.UTC().Format(time.RFC3339),
	}
	return connect.NewResponse(out), nil
}

func (h *approvalRPC) DecideRequest(ctx context.Context, req *connect.Request[identityv1.DecideRequestRequest]) (*connect.Response[emptypb.Empty], error) {
	principal, ok := middleware.PrincipalFromContext(ctx)
	if !ok || principal.UserID == "" {
		return nil, rpcerr.Unauthenticated("bearer token required")
	}
	code := req.Msg.GetUserCode()
	if normalizeCode(code) == "" {
		return nil, rpcerr.InvalidArgument("user_code cannot be blank")
	}
	decision := "deny"
	if req.Msg.GetApprove() {
		decision = "approve"
	}
	if err := h.service.Decide(ctx, code, decision, principal); err != nil {
		return nil, rpcerr.NotFound("device login request is invalid or expired")
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}
