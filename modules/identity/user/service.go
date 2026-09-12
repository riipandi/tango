package user

import (
	"context"

	"github.com/riipandi/tango/modules/identity"
)

// Service holds the user business rules and HTTP surface. It is the
// identity module's mandatory core feature.
type Service struct {
	store    Store
	recorder identity.Recorder
}

var _ identity.APIFeature = (*Service)(nil)

// NewService builds the user core on top of the given store. The
// optional recorder captures audit events.
func NewService(store Store, recorder identity.Recorder) *Service {
	return &Service{store: store, recorder: recorder}
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
