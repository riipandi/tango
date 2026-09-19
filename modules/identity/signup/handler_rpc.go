package signup

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"connectrpc.com/connect"
	commonv1 "github.com/riipandi/tango/codegen/proto/go/tango/common/v1"
	identityv1 "github.com/riipandi/tango/codegen/proto/go/tango/identity/v1"
	identityv1connect "github.com/riipandi/tango/codegen/proto/go/tango/identity/v1/identityv1connect"
	"github.com/riipandi/tango/internal/kernel"
	"github.com/riipandi/tango/internal/rpcerr"
	"github.com/riipandi/tango/internal/transport/middleware"
	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/modules/identity/session"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/pkg/validate"
	"google.golang.org/protobuf/types/known/emptypb"
)

// RPCService returns the Connect registration for the signup surface:
// signup and the initial-admin setup are anonymous; the signup-token
// administration resolves the bearer and demands an admin, guarded
// per procedure because one service mixes the two.
func (s *Service) RPCService(secure bool, access kernel.AccessAuthenticator) (string, http.Handler) {
	admin := map[string]bool{
		identityv1connect.SignupServiceListSignupTokensProcedure:  true,
		identityv1connect.SignupServiceCreateSignupTokenProcedure: true,
		identityv1connect.SignupServiceDeleteSignupTokenProcedure: true,
	}
	prefix, handler := identityv1connect.NewSignupServiceHandler(&signupRPC{service: s, secure: secure},
		connect.WithInterceptors(middleware.RPCPrincipalGuard(access, admin, nil)),
		rpcerr.RecoverOption(),
	)
	return prefix, handler
}

type signupRPC struct {
	service *Service
	secure  bool
}

func (h *signupRPC) Signup(ctx context.Context, req *connect.Request[identityv1.SignupRequest]) (*connect.Response[identityv1.SignedIn], error) {
	params := SignUpRequest{
		Username:  req.Msg.GetUsername(),
		Email:     req.Msg.GetEmail(),
		FirstName: req.Msg.GetFirstName(),
		LastName:  req.Msg.GetLastName(),
		Token:     req.Msg.GetToken(),
	}
	if err := params.Validate(); err != nil {
		return nil, validationError(err)
	}

	result, err := h.service.SignUp(ctx, params, false)
	if err != nil {
		return nil, rpcError(err)
	}
	return h.signedIn(ctx, result), nil
}

func (h *signupRPC) GetSetupAvailability(_ context.Context, _ *connect.Request[emptypb.Empty]) (*connect.Response[identityv1.GetSetupAvailabilityResponse], error) {
	available, err := h.service.SetupAvailable(context.Background())
	if err != nil {
		return nil, rpcerr.Internal("internal error")
	}
	return connect.NewResponse(&identityv1.GetSetupAvailabilityResponse{Available: available}), nil
}

func (h *signupRPC) SetupInitialAdmin(ctx context.Context, req *connect.Request[identityv1.SetupInitialAdminRequest]) (*connect.Response[identityv1.SignedIn], error) {
	params := SignUpRequest{
		Username: req.Msg.GetUsername(),
		Email:    req.Msg.GetEmail(),
	}
	if err := params.Validate(); err != nil {
		return nil, validationError(err)
	}

	result, err := h.service.SignUp(ctx, params, true)
	if err != nil {
		return nil, rpcError(err)
	}
	return h.signedIn(ctx, result), nil
}

func (h *signupRPC) ListSignupTokens(ctx context.Context, req *connect.Request[commonv1.PageRequest]) (*connect.Response[identityv1.ListSignupTokensResponse], error) {
	tokens, err := h.service.ListTokens(ctx)
	if err != nil {
		return nil, rpcerr.Internal("internal error")
	}
	page, limit := int(req.Msg.GetPage()), int(req.Msg.GetLimit())
	total := len(tokens)
	if page >= 1 && limit >= 1 {
		start := (page - 1) * limit
		if start >= total {
			tokens = nil
		} else {
			end := start + limit
			if end > total {
				end = total
			}
			tokens = tokens[start:end]
		}
	}
	out := make([]*identityv1.SignupToken, 0, len(tokens))
	for _, t := range tokens {
		out = append(out, tokenProto(t))
	}
	var metadata *commonv1.PageMetadata
	if page >= 1 && limit >= 1 {
		metadata = rpcerr.PageMetadata(page, limit, total)
	}
	return connect.NewResponse(&identityv1.ListSignupTokensResponse{Tokens: out, Metadata: metadata}), nil
}

func (h *signupRPC) CreateSignupToken(ctx context.Context, req *connect.Request[identityv1.CreateSignupTokenRequest]) (*connect.Response[identityv1.SignupTokenSecret], error) {
	var ttl time.Duration
	if req.Msg.Ttl != nil && *req.Msg.Ttl != "" {
		parsed, err := time.ParseDuration(req.Msg.GetTtl())
		if err != nil {
			return nil, rpcerr.InvalidArgument("ttl must be a duration (e.g. 24h)")
		}
		ttl = parsed
	}

	token, raw, err := h.service.CreateToken(ctx, CreateParams{
		TTL:        ttl,
		UsageLimit: int(req.Msg.GetUsageLimit()),
		GroupIDs:   req.Msg.GetUserGroupIds(),
	})
	if err != nil {
		return nil, rpcerr.Internal("failed to create token")
	}
	return connect.NewResponse(&identityv1.SignupTokenSecret{TokenInfo: tokenProto(*token), Token: raw}), nil
}

func (h *signupRPC) DeleteSignupToken(ctx context.Context, req *connect.Request[identityv1.DeleteSignupTokenRequest]) (*connect.Response[emptypb.Empty], error) {
	id, err := identity.ParseID[SignupTokenID](req.Msg.GetTokenId())
	if err != nil {
		return nil, rpcerr.NotFound("token not found")
	}
	if err := h.service.DeleteToken(ctx, id); err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, rpcerr.NotFound("token not found")
		}
		return nil, rpcerr.Internal("internal error")
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

// signedIn maps the signup result onto the shared response and rides
// the session cookies on the Connect response headers.
func (h *signupRPC) signedIn(ctx context.Context, result *Result) *connect.Response[identityv1.SignedIn] {
	resp := connect.NewResponse(signedInProto(result))
	secure := h.secure
	if u, se, err := h.service.sessions.Resolve(ctx, result.Token); err == nil {
		addCookie(resp, sessionCookie(result.Token, se.ExpiresAt, secure))
		if access, expiresAt, err := h.service.sessions.IssueAccess(ctx, principalFromResolve(u, se)); err == nil {
			addCookie(resp, accessCookie(access, expiresAt, secure))
		}
	}
	return resp
}

// sessionCookie builds the refresh-token cookie (HttpOnly, Lax,
// Secure per run mode).
func sessionCookie(token string, expires time.Time, secure bool) *http.Cookie {
	// #nosec G124 -- Secure mirrors the run mode (SameSite=Lax)
	return &http.Cookie{
		Name:     session.CookieName,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	}
}

// accessCookie mirrors the access token in its bridge-scoped cookie.
func accessCookie(token string, expires time.Time, secure bool) *http.Cookie {
	// #nosec G124 -- Secure mirrors the run mode (SameSite=Lax)
	return &http.Cookie{
		Name:     session.AccessTokenCookieName,
		Value:    token,
		Path:     session.AccessTokenPath,
		Expires:  expires,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	}
}

// addCookie attaches one cookie to the Connect response headers.
func addCookie[M any](resp *connect.Response[M], c *http.Cookie) {
	resp.Header().Add("Set-Cookie", c.String())
}

// signedInProto maps the domain result onto the shared message.
func signedInProto(result *Result) *identityv1.SignedIn {
	return &identityv1.SignedIn{
		User:    user.ProtoView(result.User),
		Pending: false,
	}
}

// principalFromResolve rebuilds the principal from a Resolve call.
func principalFromResolve(u user.User, se session.Session) kernel.Principal {
	return kernel.Principal{
		SessionID: se.ID,
		UserID:    u.ID.String(),
		Username:  u.Username,
		Email:     u.Email,
		Provider:  se.Provider,
		IsAdmin:   u.IsAdmin,
	}
}

// tokenView maps the token row onto the wire message; the hash never
// leaves the store.
func tokenProto(t SignupToken) *identityv1.SignupToken {
	return &identityv1.SignupToken{
		Id:         t.ID.String(),
		UsageLimit: rpcerr.ToInt32(t.UsageLimit),
		UsageCount: rpcerr.ToInt32(t.UsageCount),
		CreatedAt:  t.CreatedAt.UTC().Format(time.RFC3339),
		ExpiresAt:  t.ExpiresAt.UTC().Format(time.RFC3339),
		UserGroups: t.GroupIDs,
	}
}

// rpcError maps signup domain sentinels onto Connect codes.
func rpcError(err error) error {
	var cerr *connect.Error
	if errors.As(err, &cerr) {
		return cerr
	}
	switch {
	case errors.Is(err, ErrSetupCompleted):
		return rpcerr.AlreadyExists("initial setup has already been completed")
	case errors.Is(err, user.ErrDuplicate):
		return rpcerr.AlreadyExists(err.Error())
	case errors.Is(err, user.ErrInvalidUsername), errors.Is(err, user.ErrInvalidEmail):
		return rpcerr.InvalidArgument(err.Error())
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
