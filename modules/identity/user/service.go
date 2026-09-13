package user

import (
	"context"
	"net/http"
	"strings"

	"github.com/riipandi/tango/internal/transport/middleware"
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
	selfAuth middleware.Authenticator
	cookie   string
}

var _ identity.APIFeature = (*Service)(nil)

// ServiceOption configures the user core.
type ServiceOption func(*Service)

// WithAdminGuard protects the admin API routes; without it the
// routes stay open (tests, isolated tooling).
func WithAdminGuard(g RouteGuard) ServiceOption {
	return func(s *Service) { s.guard = g }
}

// WithSelfAuth wires the session resolver for the self-service
// endpoints (/users/me).
func WithSelfAuth(auth middleware.Authenticator, cookieName string) ServiceOption {
	return func(s *Service) { s.selfAuth, s.cookie = auth, cookieName }
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

// List returns matching users, newest first, with pagination
// metadata support.
func (s *Service) List(ctx context.Context, params ListParams) ([]User, int, error) {
	return s.store.List(ctx, params)
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

// Update patches administrative fields and records the change.
func (s *Service) Update(ctx context.Context, id UserID, params AdminUpdateParams) (User, error) {
	if params.Email != nil {
		trimmed := strings.TrimSpace(*params.Email)
		if !emailPattern.MatchString(trimmed) {
			return User{}, ErrInvalidEmail
		}
		params.Email = &trimmed
	}

	u, err := s.store.UpdateAdmin(ctx, id, params)
	if err != nil {
		return User{}, err
	}
	if s.recorder != nil {
		s.recorder(ctx, identity.AuditEvent{Action: "user.updated", Actor: u.ID.String(), Target: u.ID.String()})
	}
	return u, nil
}

// Delete removes the account (the DB trigger archives it) and
// records the event.
func (s *Service) Delete(ctx context.Context, id UserID) error {
	if err := s.store.Delete(ctx, id); err != nil {
		return err
	}
	if s.recorder != nil {
		s.recorder(ctx, identity.AuditEvent{Action: "user.deleted", Actor: id.String(), Target: id.String()})
	}
	return nil
}
