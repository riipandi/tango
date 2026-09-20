package signup

// service.go owns the signup business rules: token-gated account
// creation with group assignment, the initial-admin setup flow, and
// signup-token management. HTTP shells live in handler.go.

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/modules/identity/session"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/modules/identity/usergroup"
)

// Service implements the signup flows.
type Service struct {
	store    Store
	users    user.Store
	groups   usergroup.Store
	sessions *session.Service
	recorder identity.Recorder
}

// NewService builds the signup feature.
func NewService(store Store, users user.Store, groups usergroup.Store, sessions *session.Service, recorder identity.Recorder) *Service {
	return &Service{store: store, users: users, groups: groups, sessions: sessions, recorder: recorder}
}

// Name names the feature for logs.
func (s *Service) Name() string { return "signup" }

// namePattern mirrors the admin-user rules for signup names.
var namePattern = regexp.MustCompile(`^[A-Za-z0-9_]{3,32}$`)

// SignUpRequest is the POST /signup + /signup/setup payload.
type SignUpRequest struct {
	Username  string
	Email     string
	FirstName string
	LastName  string
	Token     string
}

// Validate enforces the same rules the user store applies.
func (r SignUpRequest) Validate() error {
	if !namePattern.MatchString(r.Username) {
		return user.ErrInvalidUsername
	}
	params := user.CreateParams{Username: r.Username, Email: r.Email}
	if _, err := params.Validate(); err != nil {
		return err
	}
	return nil
}

// Result couples the created account with its issued session.
type Result struct {
	User    user.User
	Token   string
	Session session.Session
}

// ErrSetupCompleted rejects a second initial-admin setup.
var ErrSetupCompleted = errors.New("initial setup has already been completed")

// SetupAvailable reports whether the initial-admin setup can run:
// any existing user — not only an admin — means the instance is
// already initialized.
func (s *Service) SetupAvailable(ctx context.Context) (bool, error) {
	exists, err := s.users.HasAnyUser(ctx)
	if err != nil {
		return false, err
	}
	return !exists, nil
}

// SignUp creates the account behind a signup token (when the policy
// requires one) and issues the session. Setup (first admin) runs
// without a token and only before the first user exists.
func (s *Service) SignUp(ctx context.Context, req SignUpRequest, isSetup bool) (*Result, error) {
	if isSetup {
		exists, err := s.users.HasAnyUser(ctx)
		if err != nil {
			return nil, err
		}
		if exists {
			return nil, ErrSetupCompleted
		}
	} else {
		if err := s.consumeToken(ctx, req.Token); err != nil {
			return nil, err
		}
	}

	createParams := user.CreateParams{
		Username:  strings.ToLower(strings.TrimSpace(req.Username)),
		Email:     strings.TrimSpace(req.Email),
		FirstName: strings.TrimSpace(req.FirstName),
		LastName:  strings.TrimSpace(req.LastName),
		IsAdmin:   isSetup,
	}
	displayName, err := createParams.Validate()
	if err != nil {
		return nil, err
	}
	createParams.DisplayName = displayName

	u, err := s.users.Create(ctx, createParams)
	if err != nil {
		return nil, err
	}

	if !isSetup {
		if assignErr := s.assignTokenGroups(ctx, req.Token, u.ID); assignErr != nil {
			return nil, assignErr
		}
	}

	token, se, issueErr := s.sessions.IssueForUser(ctx, u.ID, "signup", session.Meta{})
	if issueErr != nil {
		return nil, issueErr
	}
	if s.recorder != nil {
		action := "user.signed_up"
		if isSetup {
			action = "user.setup_completed"
		}
		s.recorder.Record(ctx, identity.AuditEvent{Action: action, Actor: u.ID.String()}, nil)
	}
	return &Result{User: u, Token: token, Session: se}, nil
}

// consumeToken validates the raw token without spending a use —
// spending happens after account creation succeeds.
func (s *Service) consumeToken(ctx context.Context, raw string) error {
	if raw == "" {
		return ErrNotFound
	}
	_, err := s.store.PeekValid(ctx, hashToken(raw))
	return err
}

// assignTokenGroups grants the token's groups to the new account.
func (s *Service) assignTokenGroups(ctx context.Context, raw string, userID user.UserID) error {
	if raw == "" {
		return nil
	}
	token, err := s.store.PeekValid(ctx, hashToken(raw))
	if err != nil {
		return err
	}
	for _, groupID := range token.GroupIDs {
		parsed, parseErr := identity.ParseID[usergroup.UserGroupID](groupID)
		if parseErr != nil {
			return ErrInvalidIDs
		}
		if setErr := s.groups.SetMembers(ctx, parsed, []user.UserID{userID}); setErr != nil {
			return fmt.Errorf("signup: assign group: %w", setErr)
		}
	}
	return s.store.ConsumeUse(ctx, token.ID)
}

// CreateToken mints a signup token (admin); returns the raw value.
func (s *Service) CreateToken(ctx context.Context, params CreateParams) (*SignupToken, string, error) {
	if params.UsageLimit <= 0 || params.UsageLimit > UsageLimitMax {
		params.UsageLimit = 1
	}
	if params.TTL <= 0 {
		params.TTL = TokenTTL
	}

	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return nil, "", err
	}
	raw := base64.RawURLEncoding.EncodeToString(buf)
	sum := sha256.Sum256([]byte(raw))

	token, err := s.store.Create(ctx, params, base64.RawURLEncoding.EncodeToString(sum[:]))
	if err != nil {
		return nil, "", err
	}
	return token, raw, nil
}

// ListTokens returns every signup token.
func (s *Service) ListTokens(ctx context.Context) ([]SignupToken, error) {
	return s.store.List(ctx)
}

// DeleteToken removes one signup token.
func (s *Service) DeleteToken(ctx context.Context, id SignupTokenID) error {
	return s.store.Delete(ctx, id)
}

// hashToken hashes a token for at-rest storage.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
