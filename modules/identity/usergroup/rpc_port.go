package usergroup

import (
	"context"
	"time"

	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/modules/identity/user"
)

// UserRPCPort adapts the group stores onto the neutral binding port
// the user package's Connect surface consumes; the direction keeps
// the user package free of a usergroup import cycle.
type UserRPCPort struct {
	service *Service
}

// NewUserRPCPort builds the port on the group service.
func NewUserRPCPort(service *Service) UserRPCPort {
	return UserRPCPort{service: service}
}

// GroupsForUser lists the groups the user belongs to.
func (p UserRPCPort) GroupsForUser(ctx context.Context, userID user.UserID) ([]user.GroupBinding, error) {
	groups, err := p.service.GroupsForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]user.GroupBinding, 0, len(groups))
	for _, g := range groups {
		binding := user.GroupBinding{
			ID:          g.ID.String(),
			Name:        g.Name,
			DisplayName: g.DisplayName,
			CreatedAt:   g.CreatedAt.UTC().Format(time.RFC3339),
		}
		if g.UpdatedAt != nil {
			at := g.UpdatedAt.UTC().Format(time.RFC3339)
			binding.UpdatedAt = &at
		}
		out = append(out, binding)
	}
	return out, nil
}

// ReplaceGroupsForUser atomically rebinds the user to exactly the
// named group IDs; unknown group IDs surface the store's error.
func (p UserRPCPort) ReplaceGroupsForUser(ctx context.Context, userID user.UserID, groupIDs []string) error {
	ids := make([]UserGroupID, 0, len(groupIDs))
	for _, raw := range groupIDs {
		id, err := identity.ParseID[UserGroupID](raw)
		if err != nil {
			return ErrNotFound
		}
		ids = append(ids, id)
	}
	return p.service.store.ReplaceGroupsForUser(ctx, userID, ids)
}
