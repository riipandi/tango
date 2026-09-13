package usergroup

import (
	"context"
	"net/http"

	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/modules/identity/user"
)

// Service holds the group business rules and HTTP surface.
type Service struct {
	store    Store
	recorder identity.Recorder
	guard    RouteGuard
}

var _ identity.APIFeature = (*Service)(nil)

// RouteGuard wraps a handler with authentication middleware.
type RouteGuard func(next http.Handler) http.Handler

// ServiceOption configures the group feature.
type ServiceOption func(*Service)

// WithAdminGuard protects the routes; without it they stay open
// (tests, isolated tooling).
func WithAdminGuard(g RouteGuard) ServiceOption {
	return func(s *Service) { s.guard = g }
}

// NewService builds the group feature on the given store.
func NewService(store Store, recorder identity.Recorder, opts ...ServiceOption) *Service {
	s := &Service{store: store, recorder: recorder}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Name implements identity.Feature.
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
		s.recorder(ctx, identity.AuditEvent{Action: "user_group.created", Actor: g.ID.String(), Target: g.ID.String()})
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
		s.recorder(ctx, identity.AuditEvent{Action: "user_group.updated", Actor: g.ID.String(), Target: g.ID.String()})
	}
	return g, nil
}

// Delete removes a group; memberships cascade.
func (s *Service) Delete(ctx context.Context, id UserGroupID) error {
	if err := s.store.Delete(ctx, id); err != nil {
		return err
	}
	if s.recorder != nil {
		s.recorder(ctx, identity.AuditEvent{Action: "user_group.deleted", Actor: id.String(), Target: id.String()})
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
