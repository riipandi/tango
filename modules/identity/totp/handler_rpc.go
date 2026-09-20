package totp

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"connectrpc.com/connect"
	identityv1 "github.com/riipandi/tango/codegen/proto/go/tango/identity/v1"
	identityv1connect "github.com/riipandi/tango/codegen/proto/go/tango/identity/v1/identityv1connect"
	"github.com/riipandi/tango/internal/kernel"
	"github.com/riipandi/tango/internal/rpcerr"
	"github.com/riipandi/tango/internal/transport/middleware"
	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/modules/identity/session"
	"github.com/riipandi/tango/modules/identity/user"
	"google.golang.org/protobuf/types/known/emptypb"
)

// RPCService returns the Connect registration for the MFA surface:
// the procedure prefix and the handler. VerifyPending is anonymous
// (the pending-auth cookie is its credential); every other procedure
// is self-service and resolves the bearer, so the guard is per
// procedure. The access authenticator comes from the composition
// root.
func (s *Service) RPCService(access kernel.AccessAuthenticator) (string, http.Handler) {
	self := map[string]bool{
		identityv1connect.MfaServiceEnrollTotpProcedure:          true,
		identityv1connect.MfaServiceConfirmTotpProcedure:         true,
		identityv1connect.MfaServiceGetTotpStatusProcedure:       true,
		identityv1connect.MfaServiceRotateRecoveryCodesProcedure: true,
		identityv1connect.MfaServiceDisableTotpProcedure:         true,
	}
	opts := append(rpcerr.Options(), connect.WithInterceptors(middleware.RPCPrincipalGuard(access, nil, self)))
	prefix, handler := identityv1connect.NewMfaServiceHandler(&mfaRPC{service: s}, opts...)
	return prefix, handler
}

type mfaRPC struct {
	service *Service
}

func (h *mfaRPC) EnrollTotp(ctx context.Context, _ *connect.Request[emptypb.Empty]) (*connect.Response[identityv1.EnrollTotpResponse], error) {
	u, err := h.currentUser(ctx)
	if err != nil {
		return nil, err
	}
	secret, uri, err := h.service.Enroll(ctx, u)
	if err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(&identityv1.EnrollTotpResponse{Secret: secret, ProvisioningUri: uri}), nil
}

func (h *mfaRPC) ConfirmTotp(ctx context.Context, req *connect.Request[identityv1.ConfirmTotpRequest]) (*connect.Response[identityv1.ConfirmTotpResponse], error) {
	u, err := h.currentUser(ctx)
	if err != nil {
		return nil, err
	}
	if verr := validateCode(req.Msg.GetCode()); verr != nil {
		return nil, verr
	}
	codes, err := h.service.Confirm(ctx, u, req.Msg.GetCode())
	if err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(&identityv1.ConfirmTotpResponse{RecoveryCodes: codes}), nil
}

func (h *mfaRPC) GetTotpStatus(ctx context.Context, _ *connect.Request[emptypb.Empty]) (*connect.Response[identityv1.GetTotpStatusResponse], error) {
	u, err := h.currentUser(ctx)
	if err != nil {
		return nil, err
	}
	confirmed, remaining, err := h.service.Status(ctx, u)
	if err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(&identityv1.GetTotpStatusResponse{
		Confirmed:              confirmed,
		RecoveryCodesRemaining: rpcerr.ToInt32(remaining),
	}), nil
}

// VerifyPending completes a pending sign-in: the pending token issued
// by the sign-in response is the credential, and the answer carries
// the session and access tokens.
func (h *mfaRPC) VerifyPending(ctx context.Context, req *connect.Request[identityv1.VerifyPendingRequest]) (*connect.Response[identityv1.SignedIn], error) {
	if err := validateCode(req.Msg.GetCode()); err != nil {
		return nil, err
	}
	pending := req.Msg.GetPendingToken()
	if pending == "" {
		return nil, rpcerr.Unauthenticated("verification required")
	}

	u, token, se, remember, err := h.service.VerifyPending(ctx, pending, req.Msg.GetCode())
	if err != nil {
		return nil, rpcError(err)
	}

	resp := connect.NewResponse(signedInProto(u, remember))
	resp.Msg.SessionToken = token
	resp.Msg.SessionId = se.ID
	if access, _, err := h.service.IssueAccess(ctx, principalForUser(u, se)); err == nil {
		resp.Msg.AccessToken = access
	}
	return resp, nil
}

func (h *mfaRPC) RotateRecoveryCodes(ctx context.Context, req *connect.Request[identityv1.ConfirmTotpRequest]) (*connect.Response[identityv1.ConfirmTotpResponse], error) {
	u, err := h.currentUser(ctx)
	if err != nil {
		return nil, err
	}
	if verr := validateCode(req.Msg.GetCode()); verr != nil {
		return nil, verr
	}
	codes, err := h.service.RotateRecoveryCodes(ctx, u, req.Msg.GetCode())
	if err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(&identityv1.ConfirmTotpResponse{RecoveryCodes: codes}), nil
}

func (h *mfaRPC) DisableTotp(ctx context.Context, req *connect.Request[identityv1.DisableTotpRequest]) (*connect.Response[emptypb.Empty], error) {
	u, err := h.currentUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.service.Disable(ctx, u, req.Msg.GetPassword()); err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

// currentUser resolves the principal to the account row.
func (h *mfaRPC) currentUser(ctx context.Context) (user.User, error) {
	principal, ok := middleware.PrincipalFromContext(ctx)
	if !ok {
		return user.User{}, rpcerr.Unauthenticated("authentication required")
	}
	parsed, err := identity.ParseID[user.UserID](principal.UserID)
	if err != nil {
		return user.User{}, rpcerr.Unauthenticated("authentication required")
	}
	u, err := h.service.users.GetByID(ctx, parsed)
	if err != nil {
		return user.User{}, rpcerr.Unauthenticated("authentication required")
	}
	return u, nil
}

// validateCode enforces the non-empty code rule.
func validateCode(code string) error {
	if strings.TrimSpace(code) == "" {
		return rpcerr.InvalidArgument("validation failed: code: cannot be blank")
	}
	return nil
}

// rpcError maps MFA domain sentinels onto Connect codes; wrong codes
// stay enumeration-safe.
func rpcError(err error) error {
	var cerr *connect.Error
	if errors.As(err, &cerr) {
		return cerr
	}
	switch {
	case errors.Is(err, ErrAlreadyConfirmed):
		return rpcerr.AlreadyExists(err.Error())
	case errors.Is(err, ErrInvalidCode), errors.Is(err, ErrNoEnrollment), errors.Is(err, ErrNotConfirmed):
		return rpcerr.Unauthenticated("verification failed")
	default:
		return rpcerr.Internal("internal error")
	}
}

// signedInProto maps the completed pending sign-in onto the shared
// message. The caller fills in the session and access tokens.
func signedInProto(u user.User, remember bool) *identityv1.SignedIn {
	return &identityv1.SignedIn{User: user.ProtoView(u), Pending: false, Remember: remember}
}

// principalForUser rebuilds the principal for the access-token mint.
func principalForUser(u user.User, se session.Session) kernel.Principal {
	return kernel.Principal{
		SessionID: se.ID,
		UserID:    u.ID.String(),
		Username:  u.Username,
		Email:     u.Email,
		Provider:  se.Provider,
		IsAdmin:   u.IsAdmin,
	}
}
