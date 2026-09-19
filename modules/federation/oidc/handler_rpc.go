package oidc

import (
	"context"
	"encoding/json/v2"
	"errors"
	"net/http"
	"strings"
	"time"

	"connectrpc.com/connect"
	commonv1 "github.com/riipandi/tango/codegen/proto/go/tango/common/v1"
	federationv1 "github.com/riipandi/tango/codegen/proto/go/tango/federation/v1"
	federationv1connect "github.com/riipandi/tango/codegen/proto/go/tango/federation/v1/federationv1connect"
	"github.com/riipandi/tango/internal/rpcerr"
	"github.com/riipandi/tango/internal/transport/middleware"
	"github.com/riipandi/tango/pkg/responder"
	"go.jetify.com/typeid"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/structpb"
)

// ScimBinding is the per-client SCIM provider projection the
// GetScimProvider procedure serves; the scimsync feature owns the
// data and adapts its store onto the lookup function.
type ScimBinding struct {
	ID           string
	Endpoint     string
	OIDCClientID string
	LastSyncedAt *time.Time
	CreatedAt    time.Time
}

// clientRPC adapts the client administration service to the generated
// Connect contract. Every procedure is admin-only; the composition
// root wraps the mount with the admin guard.
type clientRPC struct {
	service *Service
}

// consentRPC adapts the consent surface: self-service listing and
// revocation plus the admin-wide views — guarded per procedure.
type consentRPC struct {
	service *Service
}

// RPCService returns the client administration registration; the
// composition root wraps it with the admin guard.
func (f Feature) RPCService() (string, http.Handler) {
	prefix, handler := federationv1connect.NewOidcClientServiceHandler(&clientRPC{service: f.service}, rpcerr.RecoverOption())
	return prefix, handler
}

// ConsentRPCService returns the consent registration; the surface
// mixes self-service procedures with the admin-wide views, so the
// per-procedure guard rides the handler (the composition root mounts
// it bare).
func (f Feature) ConsentRPCService() (string, http.Handler) {
	admin := map[string]bool{
		federationv1connect.OidcConsentServiceListUserAuthorizedClientsProcedure: true,
		federationv1connect.OidcConsentServiceListAllAuthorizedClientsProcedure:  true,
	}
	self := map[string]bool{
		federationv1connect.OidcConsentServiceListMyAuthorizedClientsProcedure:  true,
		federationv1connect.OidcConsentServiceRevokeMyAuthorizedClientProcedure: true,
		federationv1connect.OidcConsentServiceListMyClientsProcedure:            true,
	}
	opts := []connect.HandlerOption{rpcerr.RecoverOption()}
	if f.service.access != nil {
		opts = append(opts, connect.WithInterceptors(middleware.RPCPrincipalGuard(f.service.access, admin, self)))
	}
	prefix, handler := federationv1connect.NewOidcConsentServiceHandler(&consentRPC{service: f.service}, opts...)
	return prefix, handler
}

func (h *clientRPC) ListClients(ctx context.Context, _ *connect.Request[commonv1.PageRequest]) (*connect.Response[federationv1.ListOidcClientsResponse], error) {
	clients, err := h.service.store.ListClients(ctx)
	if err != nil {
		return nil, rpcerr.Internal("failed to list clients")
	}
	out := make([]*federationv1.OidcClient, 0, len(clients))
	for _, c := range clients {
		out = append(out, clientProto(c))
	}
	return connect.NewResponse(&federationv1.ListOidcClientsResponse{Clients: out}), nil
}

func (h *clientRPC) CreateClient(ctx context.Context, req *connect.Request[federationv1.CreateOidcClientRequest]) (*connect.Response[federationv1.CreateOidcClientResponse], error) {
	created, rawSecret, err := h.service.createClient(ctx, clientRequestFromCreate(req.Msg))
	if err != nil {
		if errors.Is(err, errNotCIMD) || isMetadataError(err) {
			return nil, rpcerr.InvalidArgument(err.Error())
		}
		return nil, rpcerr.Internal("failed to create client")
	}
	return connect.NewResponse(&federationv1.CreateOidcClientResponse{
		Client:       clientProto(created),
		ClientSecret: rawSecret,
	}), nil
}

func (h *clientRPC) GetClient(ctx context.Context, req *connect.Request[federationv1.GetOidcClientRequest]) (*connect.Response[federationv1.OidcClient], error) {
	client, err := h.service.clientByID(ctx, req.Msg.GetClientId())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(clientProto(client)), nil
}

func (h *clientRPC) UpdateClient(ctx context.Context, req *connect.Request[federationv1.UpdateOidcClientRequest]) (*connect.Response[federationv1.OidcClient], error) {
	id, err := parseClientID(req.Msg.GetClientId())
	if err != nil {
		return nil, rpcerr.InvalidArgument("invalid client id")
	}

	// Validation depends on the client type: a CIMD client's
	// document-owned fields (name, redirect URIs) are optional.
	existing, err := h.service.store.GetClient(ctx, id)
	if err != nil {
		return nil, clientError(err)
	}
	request := clientRequestFromUpdate(req.Msg)
	if verr := request.validateWith(existing.ClientType == "cimd"); verr != nil {
		return nil, rpcerr.InvalidArgument("validation failed")
	}

	client, err := h.service.updateClient(ctx, id, request)
	if err != nil {
		return nil, clientError(err)
	}
	return connect.NewResponse(clientProto(client)), nil
}

func (h *clientRPC) DeleteClient(ctx context.Context, req *connect.Request[federationv1.DeleteOidcClientRequest]) (*connect.Response[emptypb.Empty], error) {
	id, err := parseClientID(req.Msg.GetClientId())
	if err != nil {
		return nil, rpcerr.InvalidArgument("invalid client id")
	}
	if err := h.service.deleteClient(ctx, id); err != nil {
		return nil, clientError(err)
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

func (h *clientRPC) UpdateAllowedUserGroups(ctx context.Context, req *connect.Request[federationv1.UpdateAllowedUserGroupsRequest]) (*connect.Response[emptypb.Empty], error) {
	id, err := parseClientID(req.Msg.GetClientId())
	if err != nil {
		return nil, rpcerr.InvalidArgument("invalid client id")
	}
	if err := h.service.store.SetClientGroups(ctx, id, req.Msg.GetGroupIds()); err != nil {
		return nil, rpcerr.Internal("internal error")
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

func (h *clientRPC) GetClientMeta(ctx context.Context, req *connect.Request[federationv1.GetOidcClientRequest]) (*connect.Response[federationv1.OidcClientMeta], error) {
	client, err := h.service.clientByID(ctx, req.Msg.GetClientId())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(clientMetaProto(client)), nil
}

func (h *clientRPC) PreviewClient(ctx context.Context, req *connect.Request[federationv1.PreviewClientRequest]) (*connect.Response[federationv1.OidcTokenPreview], error) {
	client, err := h.service.clientByID(ctx, req.Msg.GetClientId())
	if err != nil {
		return nil, err
	}
	if _, lookupErr := h.service.store.UserByID(ctx, req.Msg.GetUserId()); lookupErr != nil {
		return nil, rpcerr.NotFound("user not found")
	}

	claims, err := h.service.claimsFor(ctx, req.Msg.GetUserId(), "", "", time.Now().UTC())
	if err != nil {
		return nil, rpcerr.Internal("internal error")
	}
	now := time.Now().UTC()
	userInfo, err := structpb.NewStruct(profileClaimsMap("", claims))
	if err != nil {
		return nil, rpcerr.Internal("internal error")
	}
	return connect.NewResponse(&federationv1.OidcTokenPreview{
		IdToken:     claimsToJSON(h.service.idTokenClaims(client, claims, now, AccessTokenTTL)),
		AccessToken: claimsToJSON(h.service.accessTokenClaims(client, claims, "", now, AccessTokenTTL)),
		UserInfo:    userInfo,
	}), nil
}

func (h *clientRPC) RefreshClient(ctx context.Context, req *connect.Request[federationv1.GetOidcClientRequest]) (*connect.Response[federationv1.OidcClient], error) {
	id, err := parseClientID(req.Msg.GetClientId())
	if err != nil {
		return nil, rpcerr.InvalidArgument("invalid client id")
	}
	client, err := h.service.store.GetClient(ctx, id)
	if err != nil {
		return nil, clientError(err)
	}
	refreshed, err := h.service.refreshClientMetadata(ctx, client)
	if err != nil {
		if errors.Is(err, errNotCIMD) || isMetadataError(err) {
			return nil, rpcerr.InvalidArgument(err.Error())
		}
		return nil, rpcerr.Internal("failed to refresh client metadata")
	}
	return connect.NewResponse(clientProto(refreshed)), nil
}

func (h *clientRPC) UploadLogo(ctx context.Context, req *connect.Request[federationv1.UploadLogoRequest]) (*connect.Response[emptypb.Empty], error) {
	id, err := parseClientID(req.Msg.GetClientId())
	if err != nil {
		return nil, rpcerr.InvalidArgument("invalid client id")
	}
	client, err := h.service.store.GetClient(ctx, id)
	if err != nil {
		return nil, clientError(err)
	}
	image := req.Msg.GetImage()
	if len(image) == 0 {
		return nil, rpcerr.InvalidArgument("image is required")
	}
	if len(image) > maxLogoUpload {
		return nil, rpcerr.InvalidArgument("file too large")
	}
	ext := logoExtFromBytes(image)
	if ext == "" {
		return nil, rpcerr.InvalidArgument("unsupported_file_type")
	}

	logoPath := "client-logos/" + client.ID.String() + ext
	if err := h.service.images.Save(ctx, logoPath, strings.NewReader(string(image))); err != nil {
		return nil, rpcerr.Internal("internal error")
	}
	if err := h.service.store.SetClientLogoPath(ctx, client.ID, &logoPath); err != nil {
		_ = h.service.images.Delete(ctx, logoPath)
		return nil, clientError(err)
	}
	// The replaced blob may carry a different extension — remove it.
	if client.LogoPath != nil && *client.LogoPath != logoPath {
		_ = h.service.images.Delete(ctx, *client.LogoPath)
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

func (h *clientRPC) DeleteLogo(ctx context.Context, req *connect.Request[federationv1.DeleteLogoRequest]) (*connect.Response[emptypb.Empty], error) {
	id, err := parseClientID(req.Msg.GetClientId())
	if err != nil {
		return nil, rpcerr.InvalidArgument("invalid client id")
	}
	client, err := h.service.store.GetClient(ctx, id)
	if err != nil {
		return nil, clientError(err)
	}
	if client.LogoPath != nil {
		if err := h.service.store.SetClientLogoPath(ctx, client.ID, nil); err != nil {
			return nil, clientError(err)
		}
		// A missing blob stays a success.
		_ = h.service.images.Delete(ctx, *client.LogoPath)
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

func (h *clientRPC) ListSecrets(ctx context.Context, req *connect.Request[federationv1.ListSecretsRequest]) (*connect.Response[federationv1.ListSecretsResponse], error) {
	client, err := h.service.clientByID(ctx, req.Msg.GetClientId())
	if err != nil {
		return nil, err
	}
	out := make([]*federationv1.OidcSecretEntry, 0, len(client.Secrets))
	for _, s := range client.Secrets {
		out = append(out, secretProto(s))
	}
	return connect.NewResponse(&federationv1.ListSecretsResponse{Secrets: out}), nil
}

func (h *clientRPC) CreateSecret(ctx context.Context, req *connect.Request[federationv1.CreateSecretRequest]) (*connect.Response[federationv1.CreateSecretResponse], error) {
	id, err := parseClientID(req.Msg.GetClientId())
	if err != nil {
		return nil, rpcerr.InvalidArgument("invalid client id")
	}
	// The wire contract generates secrets server-side only (no BYO
	// field on CreateSecretRequest).
	request := createSecretRequest{}
	if raw := req.Msg.GetExpiresAt(); raw != "" {
		expiresAt, parseErr := time.Parse(time.RFC3339, raw)
		if parseErr != nil {
			return nil, rpcerr.InvalidArgument("invalid expires_at")
		}
		request.ExpiresAt = &expiresAt
	}
	created, err := h.service.addSecret(ctx, id, request)
	if err != nil {
		return nil, clientError(err)
	}
	entry := &federationv1.OidcSecretEntry{}
	if v, ok := created["id"].(string); ok {
		entry.Id = v
	}
	if v, ok := created["is_active"].(bool); ok {
		entry.IsActive = v
	}
	if v, ok := created["created_at"].(time.Time); ok {
		entry.CreatedAt = v.UTC().Format(time.RFC3339)
	}
	if v, ok := created["expires_at"].(*time.Time); ok && v != nil {
		text := v.UTC().Format(time.RFC3339)
		entry.ExpiresAt = &text
	}
	raw, _ := created["secret"].(string)
	return connect.NewResponse(&federationv1.CreateSecretResponse{
		Secret:       entry,
		ClientSecret: raw,
	}), nil
}

func (h *clientRPC) DeleteSecret(ctx context.Context, req *connect.Request[federationv1.DeleteSecretRequest]) (*connect.Response[emptypb.Empty], error) {
	id, err := parseClientID(req.Msg.GetClientId())
	if err != nil {
		return nil, rpcerr.InvalidArgument("invalid client id")
	}
	if err := h.service.store.DeleteClientSecret(ctx, id, req.Msg.GetSecretId()); err != nil {
		return nil, clientError(err)
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

func (h *clientRPC) GetScimProvider(ctx context.Context, req *connect.Request[federationv1.GetOidcClientRequest]) (*connect.Response[federationv1.ScimProvider], error) {
	if h.service.scimBinding == nil {
		return nil, rpcerr.Internal("scim lookup is not wired")
	}
	if _, err := h.service.clientByID(ctx, req.Msg.GetClientId()); err != nil {
		return nil, err
	}
	binding, err := h.service.scimBinding(ctx, req.Msg.GetClientId())
	if err != nil {
		return nil, err
	}
	out := &federationv1.ScimProvider{
		Id:           binding.ID,
		Endpoint:     binding.Endpoint,
		OidcClientId: binding.OIDCClientID,
		CreatedAt:    binding.CreatedAt.UTC().Format(time.RFC3339),
	}
	if binding.LastSyncedAt != nil {
		v := binding.LastSyncedAt.UTC().Format(time.RFC3339)
		out.LastSyncedAt = &v
	}
	return connect.NewResponse(out), nil
}

func (h *consentRPC) ListMyAuthorizedClients(ctx context.Context, req *connect.Request[commonv1.PageRequest]) (*connect.Response[federationv1.ListAuthorizedClientsResponse], error) {
	p, ok := middleware.PrincipalFromContext(ctx)
	if !ok || p.UserID == "" {
		return nil, rpcerr.Unauthenticated("bearer token required")
	}
	return h.listAuthorized(ctx, &p.UserID, req.Msg)
}

func (h *consentRPC) RevokeMyAuthorizedClient(ctx context.Context, req *connect.Request[federationv1.RevokeMyAuthorizedClientRequest]) (*connect.Response[emptypb.Empty], error) {
	p, ok := middleware.PrincipalFromContext(ctx)
	if !ok || p.UserID == "" {
		return nil, rpcerr.Unauthenticated("bearer token required")
	}
	if err := h.service.store.DeleteAuthorization(ctx, p.UserID, req.Msg.GetClientId()); err != nil {
		return nil, rpcerr.Internal("internal error")
	}
	if err := h.service.store.RevokeClientTokens(ctx, req.Msg.GetClientId(), p.UserID); err != nil {
		return nil, rpcerr.Internal("internal error")
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

func (h *consentRPC) ListMyClients(ctx context.Context, req *connect.Request[commonv1.PageRequest]) (*connect.Response[federationv1.ListMyClientsResponse], error) {
	p, ok := middleware.PrincipalFromContext(ctx)
	if !ok || p.UserID == "" {
		return nil, rpcerr.Unauthenticated("bearer token required")
	}
	page, limit := rpcerr.NormalizePage(int(req.Msg.GetPage()), int(req.Msg.GetLimit()))
	clients, err := h.service.store.AccessibleClients(ctx, p.UserID)
	if err != nil {
		return nil, rpcerr.Internal("internal error")
	}
	total := len(clients)
	if start := responder.Offset(page, limit); start > 0 || !responder.All(page, limit) {
		if start >= total {
			clients = nil
		} else {
			clients = clients[start:min(start+limit, total)]
		}
	}
	out := make([]*federationv1.OidcClient, 0, len(clients))
	for _, c := range clients {
		out = append(out, clientProto(c))
	}
	return connect.NewResponse(&federationv1.ListMyClientsResponse{
		Clients:  out,
		Metadata: rpcerr.ListMetadata(ctx, page, limit, total),
	}), nil
}

func (h *consentRPC) ListUserAuthorizedClients(ctx context.Context, req *connect.Request[federationv1.ListUserAuthorizedClientsRequest]) (*connect.Response[federationv1.ListAuthorizedClientsResponse], error) {
	// The user id is validated and resolved before the listing: an id
	// that is not a TypeID, or that names no user, is a not-found. Letting
	// either reach the query surfaces as a uuid cast failure and a 500.
	if _, err := typeid.FromString(req.Msg.GetUserId()); err != nil {
		return nil, rpcerr.NotFound("user not found")
	}
	if _, err := h.service.store.UserByID(ctx, req.Msg.GetUserId()); err != nil {
		if errors.Is(err, ErrInvalidGrant) {
			return nil, rpcerr.NotFound("user not found")
		}
		return nil, rpcerr.Internal("internal error")
	}
	userID := req.Msg.GetUserId()
	return h.listAuthorized(ctx, &userID, req.Msg.GetPage())
}

func (h *consentRPC) ListAllAuthorizedClients(ctx context.Context, req *connect.Request[commonv1.PageRequest]) (*connect.Response[federationv1.ListAuthorizedClientsResponse], error) {
	return h.listAuthorized(ctx, nil, req.Msg)
}

// listAuthorized serves both consent listings; the store returns the
// full set and the handler pages in memory (matching the unpaged REST
// contract while the wire carries metadata).
func (h *consentRPC) listAuthorized(ctx context.Context, userID *string, page *commonv1.PageRequest) (*connect.Response[federationv1.ListAuthorizedClientsResponse], error) {
	records, err := h.service.store.AuthorizedClients(ctx, userID)
	if err != nil {
		return nil, rpcerr.Internal("failed to list authorized clients")
	}
	out := make([]*federationv1.AuthorizedClient, 0, len(records))
	for _, r := range records {
		out = append(out, &federationv1.AuthorizedClient{
			UserId:     r.UserID,
			ClientId:   r.ClientID,
			Scopes:     r.Scopes,
			LastUsedAt: r.LastUsedAt.UTC().Format(time.RFC3339),
		})
	}
	pn, limit := rpcerr.NormalizePage(int(page.GetPage()), int(page.GetLimit()))
	total := len(out)
	if start := responder.Offset(pn, limit); start > 0 || !responder.All(pn, limit) {
		if start >= total {
			out = nil
		} else {
			out = out[start:min(start+limit, total)]
		}
	}
	return connect.NewResponse(&federationv1.ListAuthorizedClientsResponse{
		AuthorizedClients: out,
		Metadata:          rpcerr.ListMetadata(ctx, pn, limit, total),
	}), nil
}

// clientByID resolves a client or maps the miss onto not_found.
func (s *Service) clientByID(ctx context.Context, raw string) (Client, error) {
	id, err := parseClientID(raw)
	if err != nil {
		return Client{}, rpcerr.InvalidArgument("invalid client id")
	}
	client, err := s.store.GetClient(ctx, id)
	if err != nil {
		return Client{}, clientError(err)
	}
	return client, nil
}

// clientError maps the client store sentinels onto Connect codes.
func clientError(err error) error {
	if errors.Is(err, ErrNotFound) {
		return rpcerr.NotFound("client not found")
	}
	return rpcerr.Internal("internal error")
}

func parseClientID(raw string) (OIDCClientID, error) {
	return OIDCParseClientID(raw)
}

// logoExtFromBytes sniffs the image type against the logo allowlist;
// DetectContentType reports SVG as text/plain, so the XML prolog
// decides.
func logoExtFromBytes(image []byte) string {
	mime := http.DetectContentType(image)
	for ext, known := range logoMime {
		if known == mime {
			return ext
		}
	}
	head := image[:min(len(image), 512)]
	if mime == "text/plain; charset=utf-8" && strings.Contains(strings.TrimSpace(string(head)), "<svg") {
		return ".svg"
	}
	return ""
}

// claimsToJSON renders a claim map as the compact JSON string the
// token-preview fields carry.
func claimsToJSON(claims map[string]any) string {
	raw, err := json.Marshal(claims)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func clientProto(c Client) *federationv1.OidcClient {
	out := &federationv1.OidcClient{
		Id:                          c.ID.String(),
		Name:                        c.Name,
		HasSecret:                   hasUsableSecret(c),
		CallbackUrls:                c.CallbackURLs,
		LogoutCallbackUrls:          c.LogoutCallbackURLs,
		IsPublic:                    protoBool(c.IsPublic),
		PkceEnabled:                 protoBool(c.PKCEEnabled),
		PkceSupported:               protoBool(c.PKCESupported),
		SkipConsent:                 protoBool(c.SkipConsent),
		IsGroupRestricted:           protoBool(c.IsGroupRestricted),
		AccessTokenDurationMinutes:  protoI32(c.AccessTokenDurationMinutes),
		RefreshTokenDurationMinutes: protoI32(c.RefreshTokenDurationMinutes),
		AllowedUserGroupIds:         c.AllowedGroupIDs,
	}
	if c.Description != "" {
		out.Description = &c.Description
	}
	if c.LaunchURL != "" {
		out.LaunchUrl = &c.LaunchURL
	}
	if c.ClientType != "" {
		out.ClientType = &c.ClientType
	}
	if c.MetadataURL != nil {
		out.MetadataUrl = c.MetadataURL
	}
	return out
}

func clientMetaProto(c Client) *federationv1.OidcClientMeta {
	out := &federationv1.OidcClientMeta{
		Id:      c.ID.String(),
		Name:    c.Name,
		HasLogo: c.LogoPath != nil,
	}
	if c.Description != "" {
		out.Description = &c.Description
	}
	if c.LaunchURL != "" {
		out.LaunchUrl = &c.LaunchURL
	}
	if c.RequiresReauthentication {
		out.RequiresReauthentication = protoBool(true)
	}
	if c.ClientType != "" {
		out.ClientType = &c.ClientType
	}
	return out
}

func secretProto(s ClientSecret) *federationv1.OidcSecretEntry {
	out := &federationv1.OidcSecretEntry{
		Id:        s.ID,
		CreatedAt: s.CreatedAt.UTC().Format(time.RFC3339),
		IsActive:  s.IsActive,
	}
	if s.ExpiresAt != nil {
		v := s.ExpiresAt.UTC().Format(time.RFC3339)
		out.ExpiresAt = &v
	}
	return out
}

// clientRequestFromCreate maps the wire request onto the service
// payload; the plain-string fields mirror the REST contract.
func clientRequestFromCreate(msg *federationv1.CreateOidcClientRequest) clientRequest {
	return clientRequest{
		Name:                        msg.GetName(),
		Description:                 msg.GetDescription(),
		Secret:                      msg.Secret,
		CallbackURLs:                msg.GetCallbackUrls(),
		LogoutCallbackURLs:          msg.GetLogoutCallbackUrls(),
		LaunchURL:                   msg.GetLaunchUrl(),
		IsPublic:                    msg.GetIsPublic(),
		PKCEEnabled:                 msg.GetPkceEnabled(),
		PKCESupported:               msg.GetPkceSupported(),
		SkipConsent:                 msg.GetSkipConsent(),
		IsGroupRestricted:           msg.GetIsGroupRestricted(),
		AccessTokenDurationMinutes:  int64(msg.GetAccessTokenDurationMinutes()),
		RefreshTokenDurationMinutes: int64(msg.GetRefreshTokenDurationMinutes()),
		AllowedGroupIDs:             msg.GetAllowedUserGroupIds(),
		MetadataURL:                 msg.GetMetadataUrl(),
	}
}

// clientRequestFromUpdate maps the update wire request; only the
// fields the proto carries ride along.
func clientRequestFromUpdate(msg *federationv1.UpdateOidcClientRequest) clientRequest {
	return clientRequest{
		Name:                        msg.GetName(),
		Description:                 msg.GetDescription(),
		CallbackURLs:                msg.GetCallbackUrls(),
		LogoutCallbackURLs:          msg.GetLogoutCallbackUrls(),
		LaunchURL:                   msg.GetLaunchUrl(),
		IsPublic:                    msg.GetIsPublic(),
		PKCEEnabled:                 msg.GetPkceEnabled(),
		PKCESupported:               msg.GetPkceSupported(),
		SkipConsent:                 msg.GetSkipConsent(),
		IsGroupRestricted:           msg.GetIsGroupRestricted(),
		AccessTokenDurationMinutes:  int64(msg.GetAccessTokenDurationMinutes()),
		RefreshTokenDurationMinutes: int64(msg.GetRefreshTokenDurationMinutes()),
		AllowedGroupIDs:             msg.GetAllowedUserGroupIds(),
	}
}

func protoBool(v bool) *bool { return &v }

func protoI32(v int64) *int32 {
	out := rpcerr.ToInt32(int(v))
	return &out
}
