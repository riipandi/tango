package apikey

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"connectrpc.com/connect"
	adminv1 "github.com/riipandi/tango/gen/proto/go/tango/admin/v1"
	adminv1connect "github.com/riipandi/tango/gen/proto/go/tango/admin/v1/adminv1connect"
	commonv1 "github.com/riipandi/tango/gen/proto/go/tango/common/v1"
	"github.com/riipandi/tango/internal/rpcerr"
	"github.com/riipandi/tango/internal/transport/middleware"
	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/pkg/validate"
	"google.golang.org/protobuf/types/known/emptypb"
)

// apiKeyRPC adapts the domain service to the generated Connect
// contract. It owns only transport mapping: principal resolution and
// error codes; every decision stays in the service.
type apiKeyRPC struct {
	service *Service
}

// RPCService returns the Connect registration for the API key
// surface: the procedure prefix and the handler. The surface is
// scoped to the caller — the same contract as the REST routes — so
// the composition root wraps it with the RPC authentication
// middleware. Minting and renewing demand a session principal: a
// machine credential must not extend itself.
func (s *Service) RPCService() (string, http.Handler) {
	sessionOnly := map[string]bool{
		adminv1connect.ApiKeyServiceCreateProcedure: true,
		adminv1connect.ApiKeyServiceRenewProcedure:  true,
	}
	prefix, handler := adminv1connect.NewApiKeyServiceHandler(&apiKeyRPC{service: s},
		connect.WithInterceptors(middleware.RPCMachineDenied(sessionOnly)),
		rpcerr.RecoverOption(),
	)
	return prefix, handler
}

func (h *apiKeyRPC) List(ctx context.Context, req *connect.Request[commonv1.PageRequest]) (*connect.Response[adminv1.ListApiKeysResponse], error) {
	userID, err := h.userID(ctx)
	if err != nil {
		return nil, err
	}
	page := Page{Page: int(req.Msg.GetPage()), Limit: int(req.Msg.GetLimit())}
	keys, total, err := h.service.List(ctx, userID, ListParams{Page: page})
	if err != nil {
		return nil, rpcError(err)
	}
	out := make([]*adminv1.APIKey, 0, len(keys))
	for _, k := range keys {
		out = append(out, apiKeyView(k))
	}
	return connect.NewResponse(&adminv1.ListApiKeysResponse{
		ApiKeys:  out,
		Metadata: metadataFrom(page, total),
	}), nil
}

func (h *apiKeyRPC) Create(ctx context.Context, req *connect.Request[adminv1.CreateApiKeyRequest]) (*connect.Response[adminv1.APIKeySecret], error) {
	userID, err := h.userID(ctx)
	if err != nil {
		return nil, err
	}
	expiresAt, perr := parseRPCTime(req.Msg.GetExpiresAt())
	if perr != nil {
		return nil, rpcerr.InvalidArgument("invalid expires_at")
	}
	params := CreateParams{
		Name:        req.Msg.GetName(),
		Description: req.Msg.Description,
		ExpiresAt:   expiresAt,
	}
	if verr := params.Validate(); verr != nil {
		return nil, validationError(verr)
	}
	key, token, err := h.service.Create(ctx, userID, params)
	if err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(&adminv1.APIKeySecret{ApiKey: apiKeyView(key), Token: token}), nil
}

func (h *apiKeyRPC) Renew(ctx context.Context, req *connect.Request[adminv1.RenewApiKeyRequest]) (*connect.Response[adminv1.APIKeySecret], error) {
	userID, err := h.userID(ctx)
	if err != nil {
		return nil, err
	}
	id, err := parseID(req.Msg.GetId())
	if err != nil {
		return nil, rpcerr.NotFound("api key not found")
	}
	expiresAt, perr := parseRPCTime(req.Msg.GetExpiresAt())
	if perr != nil {
		return nil, rpcerr.InvalidArgument("invalid expires_at")
	}
	params := RenewParams{ExpiresAt: expiresAt}
	if verr := params.Validate(); verr != nil {
		return nil, validationError(verr)
	}
	key, token, err := h.service.Renew(ctx, userID, id, params)
	if err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(&adminv1.APIKeySecret{ApiKey: apiKeyView(key), Token: token}), nil
}

func (h *apiKeyRPC) Delete(ctx context.Context, req *connect.Request[adminv1.DeleteApiKeyRequest]) (*connect.Response[emptypb.Empty], error) {
	userID, err := h.userID(ctx)
	if err != nil {
		return nil, err
	}
	id, err := parseID(req.Msg.GetId())
	if err != nil {
		return nil, rpcerr.NotFound("api key not found")
	}
	if err := h.service.Revoke(ctx, userID, id); err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

// userID resolves the bearer-authenticated principal; absent callers
// answer unauthenticated.
func (h *apiKeyRPC) userID(ctx context.Context) (string, error) {
	p, ok := middleware.PrincipalFromContext(ctx)
	if !ok || p.UserID == "" {
		return "", rpcerr.Unauthenticated("bearer token required")
	}
	return p.UserID, nil
}

// rpcError maps domain sentinels onto Connect codes with the same
// wording the REST handler emits.
func rpcError(err error) error {
	switch {
	case errors.Is(err, ErrNotFound):
		return rpcerr.NotFound("api key not found")
	case errors.Is(err, ErrDuplicate), errors.Is(err, ErrNotExpired), errors.Is(err, ErrInvalidCreds):
		return rpcerr.AlreadyExists(err.Error())
	default:
		return rpcerr.Internal("internal error")
	}
}

// validationError renders pkg/validate failures as one invalid
// argument error; field detail rides the message.
func validationError(verr error) error {
	parts := make([]string, 0, 4)
	for _, fe := range validate.FieldErrors(verr) {
		parts = append(parts, fmt.Sprintf("%s: %s", fe.Field, fe.Message))
	}
	return rpcerr.InvalidArgument("validation failed: " + strings.Join(parts, "; "))
}

func apiKeyView(k APIKey) *adminv1.APIKey {
	view := &adminv1.APIKey{
		Id:        k.ID.String(),
		Name:      k.Name,
		ExpiresAt: k.ExpiresAt.UTC().Format(time.RFC3339),
		CreatedAt: k.CreatedAt.UTC().Format(time.RFC3339),
	}
	if k.Description != nil {
		view.Description = k.Description
	}
	if k.LastUsedAt != nil {
		v := k.LastUsedAt.UTC().Format(time.RFC3339)
		view.LastUsedAt = &v
	}
	return view
}

// metadataFrom builds the shared pagination block. An unpaged or
// all-marker page returns nil, so only paged responses carry metadata.
func metadataFrom(page Page, total int) *commonv1.PageMetadata {
	if page.Page < 1 || page.Limit < 1 {
		return nil
	}
	return rpcerr.PageMetadata(page.Page, page.Limit, total)
}

// parseRPCTime parses an RFC 3339 timestamp from the wire.
func parseRPCTime(raw string) (time.Time, error) {
	return time.Parse(time.RFC3339, raw)
}

// parseID accepts only the TypeID form (api_key_...); raw UUIDs 404.
func parseID(raw string) (APIKeyID, error) {
	return identity.ParseID[APIKeyID](raw)
}
