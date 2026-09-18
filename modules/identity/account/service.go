package account

import (
	"context"

	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/modules/identity/password"
	"github.com/riipandi/tango/modules/identity/session"
	"github.com/riipandi/tango/modules/identity/user"
)

// Service wires self-service account operations: profile updates,
// password change, and session management for the signed-in user.
type Service struct {
	users     user.Store
	passwords *password.Service
	sessions  *session.Service
	recorder  identity.Recorder
}

// NewService builds the account feature on the core stores and the
// session/password features it composes.
func NewService(users user.Store, passwords *password.Service, sessions *session.Service, recorder identity.Recorder) *Service {
	return &Service{users: users, passwords: passwords, sessions: sessions, recorder: recorder}
}

// Name names the feature for logs.
func (s *Service) Name() string { return "account" }

// changePassword verifies the current secret, rotates the credential,
// and revokes every other session.
func (s *Service) changePassword(ctx context.Context, userID user.UserID, current, next, keepSessionID string) error {
	if err := s.passwords.VerifyForUser(ctx, userID, current); err != nil {
		return err
	}
	if err := s.passwords.SetPassword(ctx, userID, next); err != nil {
		return err
	}
	return s.sessions.RevokeAllForUser(ctx, userID, keepSessionID)
}
