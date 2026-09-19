package customclaim

import (
	"context"
	"errors"
	"net/http"
	"time"

	"connectrpc.com/connect"
	identityv1 "github.com/riipandi/tango/codegen/proto/go/tango/identity/v1"
	identityv1connect "github.com/riipandi/tango/codegen/proto/go/tango/identity/v1/identityv1connect"
	"github.com/riipandi/tango/internal/rpcerr"
	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/modules/identity/usergroup"
	"go.jetify.com/typeid"
	"google.golang.org/protobuf/types/known/emptypb"
)

// claimRPC adapts the claim service to the generated Connect
// contract. The whole surface is admin-only, so the composition root
// wraps the mount with the admin guard.
type claimRPC struct {
	service *Service
}

// RPCService returns the Connect registration for the custom claim
// surface.
func (s *Service) RPCService() (string, http.Handler) {
	prefix, handler := identityv1connect.NewCustomClaimServiceHandler(&claimRPC{service: s}, rpcerr.RecoverOption())
	return prefix, handler
}

func (h *claimRPC) Suggest(ctx context.Context, req *connect.Request[identityv1.SuggestRequest]) (*connect.Response[identityv1.SuggestResponse], error) {
	keys, err := h.service.SuggestedKeys(ctx)
	if err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(&identityv1.SuggestResponse{Keys: keys}), nil
}

func (h *claimRPC) ListUserClaims(ctx context.Context, req *connect.Request[identityv1.ListUserClaimsRequest]) (*connect.Response[identityv1.ListClaimsResponse], error) {
	userID, err := identity.ParseID[user.UserID](req.Msg.GetUserId())
	if err != nil {
		return nil, rpcerr.NotFound("user not found")
	}
	claims, err := h.service.ListByUser(ctx, userID)
	if err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(&identityv1.ListClaimsResponse{Claims: claimsProto(claims)}), nil
}

func (h *claimRPC) CreateUserClaim(ctx context.Context, req *connect.Request[identityv1.CreateUserClaimRequest]) (*connect.Response[identityv1.CustomClaim], error) {
	userID, err := identity.ParseID[user.UserID](req.Msg.GetUserId())
	if err != nil {
		return nil, rpcerr.NotFound("user not found")
	}
	params := UpsertParams{Key: req.Msg.GetKey(), Value: req.Msg.GetValue()}
	if verr := params.Validate(); verr != nil {
		return nil, validationError(verr)
	}
	claim, err := h.service.CreateForUser(ctx, userID, params)
	if err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(claimProto(claim)), nil
}

func (h *claimRPC) UpdateUserClaim(ctx context.Context, req *connect.Request[identityv1.UpdateClaimRequest]) (*connect.Response[identityv1.CustomClaim], error) {
	claimID, err := parseClaimID(req.Msg.GetClaimId())
	if err != nil {
		return nil, rpcerr.NotFound("claim not found")
	}
	claim, err := h.service.UpdateValue(ctx, claimID, req.Msg.GetValue())
	if err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(claimProto(claim)), nil
}

func (h *claimRPC) DeleteUserClaim(ctx context.Context, req *connect.Request[identityv1.DeleteClaimRequest]) (*connect.Response[emptypb.Empty], error) {
	claimID, err := parseClaimID(req.Msg.GetClaimId())
	if err != nil {
		return nil, rpcerr.NotFound("claim not found")
	}
	if err := h.service.Delete(ctx, claimID); err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

func (h *claimRPC) ListGroupClaims(ctx context.Context, req *connect.Request[identityv1.ListGroupClaimsRequest]) (*connect.Response[identityv1.ListClaimsResponse], error) {
	groupID, err := identity.ParseID[usergroup.UserGroupID](req.Msg.GetUserGroupId())
	if err != nil {
		return nil, rpcerr.NotFound("user group not found")
	}
	claims, err := h.service.ListByGroup(ctx, groupID)
	if err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(&identityv1.ListClaimsResponse{Claims: claimsProto(claims)}), nil
}

func (h *claimRPC) CreateGroupClaim(ctx context.Context, req *connect.Request[identityv1.CreateGroupClaimRequest]) (*connect.Response[identityv1.CustomClaim], error) {
	groupID, err := identity.ParseID[usergroup.UserGroupID](req.Msg.GetUserGroupId())
	if err != nil {
		return nil, rpcerr.NotFound("user group not found")
	}
	params := UpsertParams{Key: req.Msg.GetKey(), Value: req.Msg.GetValue()}
	if verr := params.Validate(); verr != nil {
		return nil, validationError(verr)
	}
	claim, err := h.service.CreateForGroup(ctx, groupID, params)
	if err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(claimProto(claim)), nil
}

func (h *claimRPC) UpdateGroupClaim(ctx context.Context, req *connect.Request[identityv1.UpdateGroupClaimRequest]) (*connect.Response[identityv1.CustomClaim], error) {
	claimID, err := parseClaimID(req.Msg.GetClaimId())
	if err != nil {
		return nil, rpcerr.NotFound("claim not found")
	}
	claim, err := h.service.UpdateValue(ctx, claimID, req.Msg.GetValue())
	if err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(claimProto(claim)), nil
}

func (h *claimRPC) DeleteGroupClaim(ctx context.Context, req *connect.Request[identityv1.DeleteGroupClaimRequest]) (*connect.Response[emptypb.Empty], error) {
	claimID, err := parseClaimID(req.Msg.GetClaimId())
	if err != nil {
		return nil, rpcerr.NotFound("claim not found")
	}
	if err := h.service.Delete(ctx, claimID); err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

// rpcError maps custom-claim domain sentinels onto Connect codes with
// the same wording the REST handler emits.
func rpcError(err error) error {
	switch {
	case errors.Is(err, ErrNotFound):
		return rpcerr.NotFound("custom claim not found")
	case errors.Is(err, ErrDuplicate):
		return rpcerr.AlreadyExists(err.Error())
	default:
		return rpcerr.Internal("internal error")
	}
}

// validationError renders pkg/validate failures as one invalid
// argument error.
func validationError(verr error) error {
	return rpcerr.InvalidArgument("validation failed: " + verr.Error())
}

func parseClaimID(raw string) (CustomClaimID, error) {
	return identity.ParseID[CustomClaimID](raw)
}

func claimsProto(claims []CustomClaim) []*identityv1.CustomClaim {
	out := make([]*identityv1.CustomClaim, 0, len(claims))
	for _, c := range claims {
		out = append(out, claimProto(c))
	}
	return out
}

func claimProto(c CustomClaim) *identityv1.CustomClaim {
	out := &identityv1.CustomClaim{
		Id:        c.ID.String(),
		Key:       c.Key,
		Value:     c.Value,
		CreatedAt: c.CreatedAt.UTC().Format(time.RFC3339),
	}
	// Owner columns store UUIDs; the wire carries the TypeID form.
	if c.UserID != nil {
		if id, err := typeid.FromUUIDWithPrefix("user", *c.UserID); err == nil {
			wire := id.String()
			out.UserId = &wire
		} else {
			out.UserId = c.UserID
		}
	}
	if c.UserGroupID != nil {
		if id, err := typeid.FromUUIDWithPrefix("user_group", *c.UserGroupID); err == nil {
			wire := id.String()
			out.UserGroupId = &wire
		} else {
			out.UserGroupId = c.UserGroupID
		}
	}
	return out
}
