package usergroup

import (
	"context"

	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/modules/identity/user"
)

// Service holds the group business rules and HTTP surface.
type Service struct {
	store    Store
	recorder identity.Recorder
	// users backs the member projection on the Connect surface; the
	// composition root wires it.
	users user.Store
}

// ServiceOption configures the group feature.
type ServiceOption func(*Service)

// WithUserStore wires the member projection for the Connect surface.
func WithUserStore(users user.Store) ServiceOption {
	return func(s *Service) { s.users = users }
}

// NewService builds the group feature on the given store.
func NewService(store Store, recorder identity.Recorder, opts ...ServiceOption) *Service {
	s := &Service{store: store, recorder: recorder}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Name names the feature for logs.
func (s *Service) Name() string { return "usergroup" }

// Create validates and persists a new group.
func (s *Service) Create(ctx context.Context, params CreateParams) (UserGroup, error) {
	if err := params.Validate(); err != nil {
		return UserGroup{}, err
	}

	g, err := s.store.Create(ctx, params)
	if err != nil {
		return UserGroup{}, err
	}
	if s.recorder != nil {
		s.recorder.Record(ctx, identity.AuditEvent{Action: "user_group.created", Actor: g.ID.String(), Target: g.ID.String()}, nil)
	}
	return g, nil
}

// GetByID resolves one group.
func (s *Service) GetByID(ctx context.Context, id UserGroupID) (UserGroup, error) {
	return s.store.GetByID(ctx, id)
}

// List returns matching groups with pagination.
func (s *Service) List(ctx context.Context, params ListParams) ([]UserGroup, int, error) {
	return s.store.List(ctx, params)
}

// Update patches a group.
func (s *Service) Update(ctx context.Context, id UserGroupID, params UpdateParams) (UserGroup, error) {
	g, err := s.store.Update(ctx, id, params)
	if err != nil {
		return UserGroup{}, err
	}
	if s.recorder != nil {
		s.recorder.Record(ctx, identity.AuditEvent{Action: "user_group.updated", Actor: g.ID.String(), Target: g.ID.String()}, nil)
	}
	return g, nil
}

// Delete removes a group; memberships cascade.
func (s *Service) Delete(ctx context.Context, id UserGroupID) error {
	if err := s.store.Delete(ctx, id); err != nil {
		return err
	}
	if s.recorder != nil {
		s.recorder.Record(ctx, identity.AuditEvent{Action: "user_group.deleted", Actor: id.String(), Target: id.String()}, nil)
	}
	return nil
}

// SetMembers atomically replaces the group membership.
func (s *Service) SetMembers(ctx context.Context, id UserGroupID, memberIDs []user.UserID) error {
	return s.store.SetMembers(ctx, id, memberIDs)
}

// MemberIDs lists the user IDs of one group.
func (s *Service) MemberIDs(ctx context.Context, id UserGroupID) ([]user.UserID, error) {
	return s.store.MemberIDs(ctx, id)
}

// GroupsForUser lists the groups a user belongs to.
func (s *Service) GroupsForUser(ctx context.Context, id user.UserID) ([]UserGroup, error) {
	return s.store.GroupIDsForUser(ctx, id)
}

// ReplaceAllowedClients swaps the group's OIDC client allowlist and
// writes the audit entry inside the same transaction: the event
// commits exactly when the domain write does.
func (s *Service) ReplaceAllowedClients(ctx context.Context, id UserGroupID, clientIDs []string) error {
	return s.store.WithTx(ctx, func(tx datastore.Executor) error {
		if err := s.store.ReplaceAllowedClientsTx(ctx, tx, id, clientIDs); err != nil {
			return err
		}
		if s.recorder != nil {
			s.recorder.Record(ctx, identity.AuditEvent{Action: "user_group.allowed_clients_updated", Actor: id.String(), Target: id.String()}, tx)
		}
		return nil
	})
}

// AllowedClientIDs lists the group's allowlisted OIDC client ids.
func (s *Service) AllowedClientIDs(ctx context.Context, id UserGroupID) ([]string, error) {
	return s.store.AllowedClientIDs(ctx, id)
}
