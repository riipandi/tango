package password

import (
	"context"
	"errors"
	"fmt"

	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/pkg/crypto"
)

// ErrDisabled is returned when the account exists but is disabled.
var ErrDisabled = errors.New("password: account is disabled")

// Service verifies credentials and owns password lifecycle rules.
// Sign-in session creation happens in the session feature; this
// service only answers "is this secret valid".
type Service struct {
	store    Store
	hasher   *crypto.PasswordHasher
	recorder identity.Recorder
}

// NewService builds the credential feature on the given store and
// hasher. The optional recorder captures credential changes.
func NewService(store Store, hasher *crypto.PasswordHasher, recorder identity.Recorder) *Service {
	return &Service{store: store, hasher: hasher, recorder: recorder}
}

// Name names the feature for logs. The feature is headless: its
// routes (sign-in, sign-out) live in the session feature.
func (s *Service) Name() string { return "password" }

// SetPassword assigns (or rotates) the credential for the user.
func (s *Service) SetPassword(ctx context.Context, userID user.UserID, secret string) error {
	if len(secret) < 8 {
		return ErrWeakPassword
	}

	hash, err := s.hasher.Hash(secret)
	if err != nil {
		return fmt.Errorf("password: hash: %w", err)
	}

	if err := s.store.Upsert(ctx, userID, hash); err != nil {
		return err
	}

	if s.recorder != nil {
		s.recorder.Record(ctx, identity.AuditEvent{Action: "password.changed", Actor: userID.String(), Target: userID.String()}, nil)
	}
	return nil
}

// VerifyIdentity checks a username/email + secret pair and returns
// the account on success. Disabled accounts fail closed.
func (s *Service) VerifyIdentity(ctx context.Context, identityText, secret string) (user.User, error) {
	hash, u, err := s.store.HashByIdentity(ctx, identityText)
	if err != nil {
		return user.User{}, ErrInvalidCredentials
	}

	ok, err := s.hasher.Verify(secret, hash)
	if err != nil || !ok {
		return user.User{}, ErrInvalidCredentials
	}
	if u.Disabled {
		return user.User{}, ErrDisabled
	}
	return u, nil
}

// VerifyForUser checks the current secret of a known user, used
// before sensitive operations (password change).
func (s *Service) VerifyForUser(ctx context.Context, userID user.UserID, secret string) error {
	hash, err := s.store.HashByUserID(ctx, userID)
	if err != nil {
		return ErrInvalidCredentials
	}

	ok, err := s.hasher.Verify(secret, hash)
	if err != nil || !ok {
		return ErrInvalidCredentials
	}
	return nil
}
