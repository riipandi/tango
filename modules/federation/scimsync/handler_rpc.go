package scimsync

import (
	"context"
	"errors"
	"net/http"
	"time"

	"connectrpc.com/connect"
	federationv1 "github.com/riipandi/tango/codegen/proto/go/tango/federation/v1"
	federationv1connect "github.com/riipandi/tango/codegen/proto/go/tango/federation/v1/federationv1connect"
	"github.com/riipandi/tango/internal/rpcerr"
	"go.jetify.com/typeid"
	"google.golang.org/protobuf/types/known/emptypb"
)

// parseID converts a wire ID into the typed ID.
func parseID(raw string) (SCIMServiceProviderID, error) {
	return typeid.Parse[SCIMServiceProviderID](raw)
}

// providerRPC adapts the provider configuration service to the
// generated Connect contract. Every procedure is admin-only; the
// composition root wraps the mount with the admin guard.
type providerRPC struct {
	service *Service
}

// RPCService returns the Connect registration for the SCIM provider
// surface.
func (f *APIFeature) RPCService() (string, http.Handler) {
	prefix, handler := federationv1connect.NewScimProviderServiceHandler(&providerRPC{service: f.service}, rpcerr.Options()...)
	return prefix, handler
}

func (h *providerRPC) Upsert(ctx context.Context, req *connect.Request[federationv1.UpsertScimProviderRequest]) (*connect.Response[federationv1.ScimProvider], error) {
	params := UpsertParams{
		Endpoint:     req.Msg.GetEndpoint(),
		Token:        req.Msg.GetToken(),
		OIDCClientID: req.Msg.GetOidcClientId(),
	}
	if verr := params.Validate(); verr != nil {
		return nil, rpcerr.InvalidArgument(verr.Error())
	}
	provider, err := h.service.store.Create(ctx, params)
	if err != nil {
		return nil, storeError(err)
	}
	return connect.NewResponse(providerProto(provider, true)), nil
}

func (h *providerRPC) Update(ctx context.Context, req *connect.Request[federationv1.UpdateScimProviderRequest]) (*connect.Response[federationv1.ScimProvider], error) {
	id, err := parseID(req.Msg.GetId())
	if err != nil {
		return nil, rpcerr.NotFound("service provider not found")
	}

	// Absent fields keep the current value (the wire contract notes
	// this for the token; the endpoint follows the same rule).
	current, err := h.service.store.GetByID(ctx, id)
	if err != nil {
		return nil, storeError(err)
	}
	params := UpsertParams{
		Endpoint:     current.Endpoint,
		Token:        current.Token,
		OIDCClientID: current.OIDCClientID,
	}
	if req.Msg.Endpoint != nil {
		params.Endpoint = req.Msg.GetEndpoint()
	}
	if req.Msg.Token != nil {
		params.Token = req.Msg.GetToken()
	}
	if verr := params.Validate(); verr != nil {
		return nil, rpcerr.InvalidArgument(verr.Error())
	}

	provider, err := h.service.store.Update(ctx, id, params)
	if err != nil {
		return nil, storeError(err)
	}
	return connect.NewResponse(providerProto(provider, true)), nil
}

func (h *providerRPC) Delete(ctx context.Context, req *connect.Request[federationv1.DeleteScimProviderRequest]) (*connect.Response[emptypb.Empty], error) {
	id, err := parseID(req.Msg.GetId())
	if err != nil {
		return nil, rpcerr.NotFound("service provider not found")
	}
	if err := h.service.store.Delete(ctx, id); err != nil {
		return nil, storeError(err)
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

func (h *providerRPC) Sync(ctx context.Context, req *connect.Request[federationv1.SyncScimProviderRequest]) (*connect.Response[emptypb.Empty], error) {
	id, err := parseID(req.Msg.GetId())
	if err != nil {
		return nil, rpcerr.NotFound("service provider not found")
	}
	provider, err := h.service.store.GetByID(ctx, id)
	if err != nil {
		return nil, storeError(err)
	}
	if err := h.service.SyncProvider(ctx, provider); err != nil {
		// The sync failure may carry provider/network internals —
		// log-bound only, the caller sees the fixed error.
		return nil, rpcerr.Internal("scim sync failed")
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

// storeError maps the provider store sentinels onto Connect codes.
func storeError(err error) error {
	switch {
	case errors.Is(err, ErrNotFound):
		return rpcerr.NotFound("service provider not found")
	case errors.Is(err, ErrUnknownClient):
		return rpcerr.InvalidArgument("unknown oidc client")
	case errors.Is(err, ErrDuplicate):
		return rpcerr.AlreadyExists("client already has a scim service provider")
	default:
		return rpcerr.Internal("internal error")
	}
}

// providerProto renders the stored provider; the token appears only
// on write responses (withToken), never on reads.
func providerProto(p ServiceProvider, withToken bool) *federationv1.ScimProvider {
	out := &federationv1.ScimProvider{
		Id:           p.ID.String(),
		Endpoint:     p.Endpoint,
		OidcClientId: p.OIDCClientID,
		CreatedAt:    p.CreatedAt.UTC().Format(time.RFC3339),
	}
	if withToken && p.Token != "" {
		out.Token = &p.Token
	}
	if p.LastSyncedAt != nil {
		v := p.LastSyncedAt.UTC().Format(time.RFC3339)
		out.LastSyncedAt = &v
	}
	return out
}
