package usergroup

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"connectrpc.com/connect"
	commonv1 "github.com/riipandi/tango/gen/proto/go/tango/common/v1"
	identityv1 "github.com/riipandi/tango/gen/proto/go/tango/identity/v1"
	identityv1connect "github.com/riipandi/tango/gen/proto/go/tango/identity/v1/identityv1connect"
	"github.com/riipandi/tango/internal/rpcerr"
	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/pkg/validate"
	"google.golang.org/protobuf/types/known/emptypb"
)

// RPCService returns the Connect registration for the group surface:
// the whole service is admin-only, so the composition root's admin
// guard wraps it.
func (s *Service) RPCService() (string, http.Handler) {
	prefix, handler := identityv1connect.NewUserGroupServiceHandler(&groupRPC{service: s}, rpcerr.RecoverOption())
	return prefix, handler
}

type groupRPC struct {
	service *Service
}

func (h *groupRPC) ListGroups(ctx context.Context, req *connect.Request[commonv1.PageRequest]) (*connect.Response[identityv1.ListGroupsResponse], error) {
	page, limit := int(req.Msg.GetPage()), int(req.Msg.GetLimit())
	groups, total, err := h.service.List(ctx, ListParams{
		Query: req.Msg.GetQuery(),
		Page:  Page{Page: page, Limit: limit},
	})
	if err != nil {
		return nil, rpcError(err)
	}
	out := make([]*identityv1.UserGroup, 0, len(groups))
	for _, g := range groups {
		out = append(out, groupView(g))
	}
	var metadata *commonv1.PageMetadata
	if page >= 1 && limit >= 1 {
		metadata = rpcerr.PageMetadata(page, limit, total)
	}
	return connect.NewResponse(&identityv1.ListGroupsResponse{Groups: out, Metadata: metadata}), nil
}

func (h *groupRPC) GetGroup(ctx context.Context, req *connect.Request[identityv1.GetUserGroupRequest]) (*connect.Response[identityv1.UserGroup], error) {
	id, err := groupID(req.Msg.GetGroupId())
	if err != nil {
		return nil, err
	}
	g, err := h.service.GetByID(ctx, id)
	if err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(groupView(g)), nil
}

func (h *groupRPC) CreateGroup(ctx context.Context, req *connect.Request[identityv1.CreateUserGroupRequest]) (*connect.Response[identityv1.UserGroup], error) {
	params := CreateParams{Name: req.Msg.GetName(), DisplayName: req.Msg.GetDisplayName()}
	if err := params.Validate(); err != nil {
		return nil, validationError(err)
	}
	g, err := h.service.Create(ctx, params)
	if err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(groupView(g)), nil
}

func (h *groupRPC) UpdateGroup(ctx context.Context, req *connect.Request[identityv1.UpdateUserGroupRequest]) (*connect.Response[identityv1.UserGroup], error) {
	id, err := groupID(req.Msg.GetGroupId())
	if err != nil {
		return nil, err
	}
	if req.Msg.DisplayName != nil && *req.Msg.DisplayName == "" {
		return nil, rpcerr.InvalidArgument("validation failed: display_name: must not be empty")
	}
	params := UpdateParams{Name: req.Msg.Name, DisplayName: req.Msg.DisplayName}
	g, err := h.service.Update(ctx, id, params)
	if err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(groupView(g)), nil
}

func (h *groupRPC) DeleteGroup(ctx context.Context, req *connect.Request[identityv1.DeleteUserGroupRequest]) (*connect.Response[emptypb.Empty], error) {
	id, err := groupID(req.Msg.GetGroupId())
	if err != nil {
		return nil, err
	}
	if err := h.service.Delete(ctx, id); err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

func (h *groupRPC) ListGroupUsers(ctx context.Context, req *connect.Request[identityv1.ListGroupUsersRequest]) (*connect.Response[identityv1.ListGroupUsersResponse], error) {
	id, err := groupID(req.Msg.GetGroupId())
	if err != nil {
		return nil, err
	}
	page, limit := int(req.Msg.GetPage().GetPage()), int(req.Msg.GetPage().GetLimit())
	members, err := h.service.MemberIDs(ctx, id)
	if err != nil {
		return nil, rpcError(err)
	}

	// The store keeps full membership; the page slices it in place.
	total := len(members)
	if page >= 1 && limit >= 1 {
		start := (page - 1) * limit
		if start >= total {
			members = nil
		} else {
			end := start + limit
			if end > total {
				end = total
			}
			members = members[start:end]
		}
	}
	out := make([]*identityv1.User, 0, len(members))
	for _, memberID := range members {
		if h.service.users == nil {
			return nil, rpcerr.Unimplemented("member projection is not wired")
		}
		member, err := h.service.users.GetByID(ctx, memberID)
		if err != nil {
			return nil, rpcError(err)
		}
		out = append(out, user.ProtoView(member))
	}
	var metadata *commonv1.PageMetadata
	if page >= 1 && limit >= 1 {
		metadata = rpcerr.PageMetadata(page, limit, total)
	}
	return connect.NewResponse(&identityv1.ListGroupUsersResponse{Users: out, Metadata: metadata}), nil
}

func (h *groupRPC) ReplaceGroupUsers(ctx context.Context, req *connect.Request[identityv1.ReplaceGroupUsersRequest]) (*connect.Response[emptypb.Empty], error) {
	id, err := groupID(req.Msg.GetGroupId())
	if err != nil {
		return nil, err
	}
	members := make([]user.UserID, 0, len(req.Msg.GetUserIds()))
	for _, raw := range req.Msg.GetUserIds() {
		memberID, err := identity.ParseID[user.UserID](raw)
		if err != nil {
			return nil, rpcerr.InvalidArgument("invalid user_id: " + raw)
		}
		members = append(members, memberID)
	}
	if err := h.service.SetMembers(ctx, id, members); err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

func (h *groupRPC) ReplaceAllowedOidcClients(ctx context.Context, req *connect.Request[identityv1.ReplaceAllowedOidcClientsRequest]) (*connect.Response[emptypb.Empty], error) {
	id, err := groupID(req.Msg.GetGroupId())
	if err != nil {
		return nil, err
	}
	if err := h.service.ReplaceAllowedClients(ctx, id, req.Msg.GetClientIds()); err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

// groupView maps the domain group onto the wire message.
func groupView(g UserGroup) *identityv1.UserGroup {
	out := &identityv1.UserGroup{
		Id:          g.ID.String(),
		Name:        g.Name,
		DisplayName: g.DisplayName,
		CreatedAt:   g.CreatedAt.UTC().Format(time.RFC3339),
	}
	if g.UpdatedAt != nil {
		at := g.UpdatedAt.UTC().Format(time.RFC3339)
		out.UpdatedAt = &at
	}
	return out
}

// groupID parses a path group ID; TypeID-only, unknown shapes 404.
func groupID(raw string) (UserGroupID, error) {
	id, err := identity.ParseID[UserGroupID](raw)
	if err != nil {
		return UserGroupID{}, rpcerr.NotFound("group not found")
	}
	return id, nil
}

// rpcError maps group domain sentinels onto Connect codes; connect
// errors raised inside the service pass through unchanged.
func rpcError(err error) error {
	var cerr *connect.Error
	if errors.As(err, &cerr) {
		return cerr
	}
	switch {
	case errors.Is(err, ErrNotFound):
		return rpcerr.NotFound("group not found")
	case errors.Is(err, ErrDuplicate):
		return rpcerr.AlreadyExists(err.Error())
	case errors.Is(err, ErrInvalidIDs):
		return rpcerr.InvalidArgument(err.Error())
	default:
		return rpcerr.Internal("internal error")
	}
}

// validationError maps ozzo field errors onto invalid_argument.
func validationError(verr error) error {
	parts := make([]string, 0, 4)
	for _, fe := range validate.FieldErrors(verr) {
		parts = append(parts, fmt.Sprintf("%s: %s", fe.Field, fe.Message))
	}
	return rpcerr.InvalidArgument("validation failed: " + strings.Join(parts, "; "))
}
