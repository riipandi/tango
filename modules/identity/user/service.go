package user

import (
	"context"
	"net/http"

	"github.com/riipandi/tango/modules/identity"
)

// RouteGuard wraps a handler with authentication middleware
// (stdlib shape, so this package stays transport-agnostic).
type RouteGuard func(http.Handler) http.Handler

// Service holds the user business rules and HTTP surface. It is the
// identity module's mandatory core feature.
type Service struct {
	store    Store
	recorder identity.Recorder
	guard    RouteGuard
}

var _ identity.APIFeature = (*Service)(nil)

// ServiceOption configures the user core.
type ServiceOption func(*Service)

// WithAdminGuard protects the admin API routes; without it the
// routes stay open (tests, isolated tooling).
func WithAdminGuard(g RouteGuard) ServiceOption {
	return func(s *Service) { s.guard = g }
}

// NewService builds the user core on top of the given store. The
// optional recorder captures audit events.
func NewService(store Store, recorder identity.Recorder, opts ...ServiceOption) *Service {
	s := &Service{store: store, recorder: recorder}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Name implements identity.Feature.
func (s *Service) Name() string { return "user" }

// List returns all users, newest first.
func (s *Service) List(ctx context.Context) []User {
	return s.store.List(ctx)
}

// GetByID resolves one user; unknown IDs surface ErrNotFound.
func (s *Service) GetByID(ctx context.Context, id UserID) (User, error) {
	return s.store.GetByID(ctx, id)
}

// Create validates the payload, persists the user, and records an
// audit event when a recorder is wired.
func (s *Service) Create(ctx context.Context, params CreateParams) (User, error) {
	displayName, err := params.Validate()
	if err != nil {
		return User{}, err
	}
	params.DisplayName = displayName

	user, err := s.store.Create(ctx, params)
	if err != nil {
		return User{}, err
	}

	if s.recorder != nil {
		s.recorder(ctx, identity.AuditEvent{Action: "user.created", Actor: user.ID.String(), Target: user.ID.String()})
	}
	return user, nil
}
