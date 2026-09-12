package user

import (
	"context"
	"errors"

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

func (s *Service) List(ctx context.Context) []identity.User {
	return s.store.List(ctx)
}

func (s *Service) GetByID(ctx context.Context, id identity.UserID) (identity.User, error) {
	user, ok := s.store.GetByID(ctx, id)
	if !ok {
		return identity.User{}, errors.New("user not found")
	}
	return user, nil
}

func (s *Service) Create(ctx context.Context, name string) (identity.User, error) {
	if name == "" {
		return identity.User{}, ErrInvalidName
	}

	user, err := s.store.Create(ctx, name)
	if err != nil {
		return identity.User{}, err
	}

	if s.recorder != nil {
		s.recorder(identity.AuditEvent{Action: "user.created", Actor: user.ID.String(), Target: user.ID.String()})
	}
	return user, nil
}
