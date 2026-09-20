package apiaccess

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"connectrpc.com/connect"
	adminv1 "github.com/riipandi/tango/codegen/proto/go/tango/admin/v1"
	adminv1connect "github.com/riipandi/tango/codegen/proto/go/tango/admin/v1/adminv1connect"
	commonv1 "github.com/riipandi/tango/codegen/proto/go/tango/common/v1"
	federationv1 "github.com/riipandi/tango/codegen/proto/go/tango/federation/v1"
	"github.com/riipandi/tango/internal/rpcerr"
	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/pkg/responder"
	"github.com/riipandi/tango/pkg/validate"
	"google.golang.org/protobuf/types/known/emptypb"
)

// apiRPC adapts the domain service to the generated Connect contract.
// The whole surface is admin-only, so the composition root wraps the
// mount with the admin guard and the handler carries no principal
// logic of its own.
type apiRPC struct { //nolint:staticcheck // generated interface names
	service *Service
}

// RPCService returns the Connect registration for the API registry
// surface.
func (s *Service) RPCService() (string, http.Handler) {
	prefix, handler := adminv1connect.NewApiServiceHandler(&apiRPC{service: s}, rpcerr.Options()...)
	return prefix, handler
}

func (h *apiRPC) ListApis(ctx context.Context, req *connect.Request[commonv1.PageRequest]) (*connect.Response[adminv1.ListApisResponse], error) {
	page, limit := pageFrom(req.Msg)
	apis, total, err := h.service.List(ctx, ListParams{Query: req.Msg.GetQuery(), Page: Page{Page: page, Limit: limit}})
	if err != nil {
		return nil, rpcError(err)
	}
	out := make([]*adminv1.API, 0, len(apis))
	for _, a := range apis {
		out = append(out, apiProto(a))
	}
	return connect.NewResponse(&adminv1.ListApisResponse{
		Apis:     out,
		Metadata: rpcerr.ListMetadata(ctx, page, limit, total),
	}), nil
}

//nolint:staticcheck // generated interface name
func (h *apiRPC) CreateAPI(ctx context.Context, req *connect.Request[adminv1.CreateApiRequest]) (*connect.Response[adminv1.API], error) {
	params := CreateParams{Name: req.Msg.GetName(), Resource: req.Msg.GetResource()}
	if verr := params.Validate(); verr != nil {
		return nil, validationError(verr)
	}
	a, err := h.service.Create(ctx, params)
	if err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(apiProto(a)), nil
}

//nolint:staticcheck // generated interface name
func (h *apiRPC) GetAPI(ctx context.Context, req *connect.Request[adminv1.GetApiRequest]) (*connect.Response[adminv1.API], error) {
	id, err := parseAPIID(req.Msg.GetId())
	if err != nil {
		return nil, rpcerr.NotFound("api not found")
	}
	a, err := h.service.GetByID(ctx, id)
	if err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(apiProto(a)), nil
}

//nolint:staticcheck // generated interface name
func (h *apiRPC) UpdateAPI(ctx context.Context, req *connect.Request[adminv1.UpdateApiRequest]) (*connect.Response[adminv1.API], error) {
	id, err := parseAPIID(req.Msg.GetId())
	if err != nil {
		return nil, rpcerr.NotFound("api not found")
	}
	params := UpdateParams{Name: req.Msg.GetName()}
	if verr := params.Validate(); verr != nil {
		return nil, validationError(verr)
	}
	a, err := h.service.Update(ctx, id, params)
	if err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(apiProto(a)), nil
}

//nolint:staticcheck // generated interface name
func (h *apiRPC) DeleteAPI(ctx context.Context, req *connect.Request[adminv1.DeleteApiRequest]) (*connect.Response[emptypb.Empty], error) {
	id, err := parseAPIID(req.Msg.GetId())
	if err != nil {
		return nil, rpcerr.NotFound("api not found")
	}
	if err := h.service.Delete(ctx, id); err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

func (h *apiRPC) SetPermissions(ctx context.Context, req *connect.Request[adminv1.SetPermissionsRequest]) (*connect.Response[emptypb.Empty], error) {
	id, err := parseAPIID(req.Msg.GetId())
	if err != nil {
		return nil, rpcerr.NotFound("api not found")
	}
	perms := make([]PermissionInput, 0, len(req.Msg.GetPermissionIds()))
	for _, p := range req.Msg.GetPermissionIds() {
		perm := PermissionInput{Key: p, Name: p}
		if err := perm.Validate(); err != nil {
			return nil, validationError(err)
		}
		perms = append(perms, perm)
	}
	if _, err := h.service.SetPermissions(ctx, id, perms); err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

func (h *apiRPC) SetCimdAccess(ctx context.Context, req *connect.Request[adminv1.SetCimdAccessRequest]) (*connect.Response[emptypb.Empty], error) {
	id, err := parseAPIID(req.Msg.GetId())
	if err != nil {
		return nil, rpcerr.NotFound("api not found")
	}
	if _, err := h.service.SetCIMDAccess(ctx, id, req.Msg.GetAllowedForCimdClients(), req.Msg.GetPermissionIds()); err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

func (h *apiRPC) ListAssignableClients(ctx context.Context, req *connect.Request[adminv1.ListApiClientsRequest]) (*connect.Response[adminv1.ListClientRefsResponse], error) {
	id, err := parseAPIID(req.Msg.GetApiId())
	if err != nil {
		return nil, rpcerr.NotFound("api not found")
	}
	return h.listClients(ctx, id, req, false)
}

func (h *apiRPC) ListClients(ctx context.Context, req *connect.Request[adminv1.ListApiClientsRequest]) (*connect.Response[adminv1.ListClientRefsResponse], error) {
	id, err := parseAPIID(req.Msg.GetApiId())
	if err != nil {
		return nil, rpcerr.NotFound("api not found")
	}
	return h.listClients(ctx, id, req, true)
}

func (h *apiRPC) listClients(ctx context.Context, id APIID, req *connect.Request[adminv1.ListApiClientsRequest], granted bool) (*connect.Response[adminv1.ListClientRefsResponse], error) {
	page, limit := rpcerr.NormalizePage(int(req.Msg.GetPage().GetPage()), int(req.Msg.GetPage().GetLimit()))
	params := ListParams{Page: Page{Page: page, Limit: limit}}
	var clients []ClientRef
	var total int
	var err error
	if granted {
		clients, total, err = h.service.ClientsWithAccess(ctx, id, params)
	} else {
		clients, total, err = h.service.AssignableClients(ctx, id, params)
	}
	if err != nil {
		return nil, rpcError(err)
	}
	refs := make([]*federationv1.ClientRef, 0, len(clients))
	for _, c := range clients {
		refs = append(refs, &federationv1.ClientRef{
			Id:         c.ID,
			Name:       c.Name,
			ClientType: c.ClientType,
			IsPublic:   c.IsPublic,
			HasLogo:    c.HasLogo,
		})
	}
	return connect.NewResponse(&adminv1.ListClientRefsResponse{
		Clients:  refs,
		Metadata: rpcerr.ListMetadata(ctx, page, limit, total),
	}), nil
}

func (h *apiRPC) GrantClient(ctx context.Context, req *connect.Request[adminv1.GrantClientRequest]) (*connect.Response[emptypb.Empty], error) {
	id, err := parseAPIID(req.Msg.GetApiId())
	if err != nil {
		return nil, rpcerr.NotFound("api not found")
	}
	if _, err := h.service.UpsertGrant(ctx, id, req.Msg.GetClientId(), GrantParams{
		UserDelegatedAccess:        req.Msg.GetUserDelegatedAccess(),
		UserDelegatedPermissionIDs: req.Msg.GetUserDelegatedPermissionIds(),
		ClientAccess:               req.Msg.GetClientAccess(),
		ClientPermissionIDs:        req.Msg.GetClientPermissionIds(),
	}); err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

func (h *apiRPC) RevokeClient(ctx context.Context, req *connect.Request[adminv1.RevokeClientRequest]) (*connect.Response[emptypb.Empty], error) {
	id, err := parseAPIID(req.Msg.GetApiId())
	if err != nil {
		return nil, rpcerr.NotFound("api not found")
	}
	if err := h.service.DeleteGrant(ctx, id, req.Msg.GetClientId()); err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

func (h *apiRPC) ListApisForClient(ctx context.Context, req *connect.Request[adminv1.ListClientApisRequest]) (*connect.Response[adminv1.ListClientApiGrantsResponse], error) {
	page, limit := rpcerr.NormalizePage(int(req.Msg.GetPage().GetPage()), int(req.Msg.GetPage().GetLimit()))
	grants, err := h.service.GrantsForClient(ctx, req.Msg.GetClientId())
	if err != nil {
		return nil, rpcError(err)
	}
	total := len(grants)
	if start := responder.Offset(page, limit); start > 0 || !responder.All(page, limit) {
		if start >= total {
			grants = nil
		} else {
			grants = grants[start:min(start+limit, total)]
		}
	}
	out := make([]*adminv1.ClientApiGrant, 0, len(grants))
	for _, g := range grants {
		apiID, parseErr := identity.ParseID[APIID](g.APIID)
		if parseErr != nil {
			continue
		}
		a, getErr := h.service.GetByID(ctx, apiID)
		if getErr != nil {
			continue
		}
		cimdAccess, cimdPerms := cimdAccessFor(a, g)
		out = append(out, &adminv1.ClientApiGrant{
			Api:                        apiProto(a),
			ClientAccess:               g.ClientAccess,
			ClientPermissionIds:        g.ClientPermissionIDs,
			UserDelegatedAccess:        g.UserDelegatedAccess,
			UserDelegatedPermissionIds: g.UserDelegatedPermissionIDs,
			CimdGrantedAccess:          cimdAccess,
			CimdGrantedPermissionIds:   cimdPerms,
		})
	}
	return connect.NewResponse(&adminv1.ListClientApiGrantsResponse{
		Grants:   out,
		Metadata: rpcerr.ListMetadata(ctx, page, limit, total),
	}), nil
}

func (h *apiRPC) ListAssignableApisForClient(ctx context.Context, req *connect.Request[adminv1.ListClientApisRequest]) (*connect.Response[adminv1.ListApisResponse], error) {
	page, limit := rpcerr.NormalizePage(int(req.Msg.GetPage().GetPage()), int(req.Msg.GetPage().GetLimit()))
	apis, total, err := h.service.AssignableAPIs(ctx, req.Msg.GetClientId(), ListParams{Page: Page{Page: page, Limit: limit}})
	if err != nil {
		return nil, rpcError(err)
	}
	out := make([]*adminv1.API, 0, len(apis))
	for _, a := range apis {
		out = append(out, apiProto(a))
	}
	return connect.NewResponse(&adminv1.ListApisResponse{
		Apis:     out,
		Metadata: rpcerr.ListMetadata(ctx, page, limit, total),
	}), nil
}

// cimdAccessFor computes the CIMD access split for one grant: the
// API flag gates access; the allowlist gates each permission.
func cimdAccessFor(a API, g Grant) (bool, []string) {
	if !a.AllowCIMDClients {
		return false, []string{}
	}
	allowed := map[string]bool{}
	for _, p := range a.Permissions {
		if p.AllowedForCIMDClients {
			allowed[p.ID] = true
		}
	}
	ids := make([]string, 0, len(g.UserDelegatedPermissionIDs))
	for _, pid := range g.UserDelegatedPermissionIDs {
		if allowed[pid] {
			ids = append(ids, pid)
		}
	}
	return true, ids
}

// rpcError maps apiaccess domain sentinels onto Connect codes with
// the same wording the REST handler emits.
func rpcError(err error) error {
	switch {
	case errors.Is(err, ErrNotFound):
		return rpcerr.NotFound("api not found")
	case errors.Is(err, ErrDuplicate):
		return rpcerr.AlreadyExists(err.Error())
	case errors.Is(err, ErrUnknownClient), errors.Is(err, ErrUnknownPerms), errors.Is(err, ErrInvalidSubject):
		return rpcerr.InvalidArgument(err.Error())
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

func apiProto(a API) *adminv1.API {
	out := &adminv1.API{
		Id:               a.ID.String(),
		Name:             a.Name,
		Resource:         a.Resource,
		AllowCimdClients: a.AllowCIMDClients,
		CreatedAt:        a.CreatedAt.UTC().Format(time.RFC3339),
	}
	if a.UpdatedAt != nil {
		v := a.UpdatedAt.UTC().Format(time.RFC3339)
		out.UpdatedAt = &v
	}
	out.Permissions = make([]*adminv1.Permission, 0, len(a.Permissions))
	for _, p := range a.Permissions {
		out.Permissions = append(out.Permissions, permissionProto(p))
	}
	return out
}

func permissionProto(p Permission) *adminv1.Permission {
	allowed := p.AllowedForCIMDClients
	return &adminv1.Permission{
		Id:                    p.ID,
		Key:                   p.Key,
		Name:                  p.Name,
		Description:           p.Description,
		AllowedForCimdClients: &allowed,
	}
}

// pageFrom converts the wire page into the store window, applying the
// shared pagination rules so an unset limit never reaches the store as
// "no LIMIT".
func pageFrom(msg *commonv1.PageRequest) (int, int) {
	return rpcerr.NormalizePage(int(msg.GetPage()), int(msg.GetLimit()))
}

// parseAPIID accepts only the TypeID form (api_...); raw UUIDs 404.
func parseAPIID(raw string) (APIID, error) {
	return identity.ParseID[APIID](raw)
}
