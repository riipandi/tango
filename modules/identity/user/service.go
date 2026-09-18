package user

import (
	"context"
	"strings"

	"github.com/riipandi/tango/modules/identity"
)

// (stdlib shape, so this package stays transport-agnostic).

// Service holds the user business rules and HTTP surface. It is the
// identity module's mandatory core feature. Guards are not stored:
// the mount call receives the route groups.
type Service struct {
	store    Store
	recorder identity.Recorder

	images         ImageStore
	defaultPicture DefaultPictureFunc

	// groupPort and credentialPort back the cross-feature sections
	// of the Connect surface; wired by the composition root.
	groupPort      GroupBindingPort
	credentialPort CredentialAdminPort
}

// ServiceOption configures the user core.
type ServiceOption func(*Service)

// NewService builds the user core on top of the given store. The
// optional recorder captures audit events.
func NewService(store Store, recorder identity.Recorder, opts ...ServiceOption) *Service {
	s := &Service{store: store, recorder: recorder}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Name names the feature for logs.
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
		s.recorder.Record(ctx, identity.AuditEvent{Action: "user.created", Actor: user.ID.String(), Target: user.ID.String()}, nil)
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
		s.recorder.Record(ctx, identity.AuditEvent{Action: "user.updated", Actor: u.ID.String(), Target: u.ID.String()}, nil)
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
		s.recorder.Record(ctx, identity.AuditEvent{Action: "user.deleted", Actor: id.String(), Target: id.String()}, nil)
	}
	return nil
}
