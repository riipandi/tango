package apikey

import (
	"context"
	"net/http"

	"github.com/riipandi/tango/internal/kernel"
	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/modules/identity/session"
)

// Service holds the API key business rules and HTTP surface.
type Service struct {
	store    Store
	recorder identity.Recorder
	guard    kernel.Guard
	selfAuth kernel.Authenticator
}

// ServiceOption configures the API key feature.
type ServiceOption func(*Service)

// WithAdminGuard protects the routes; without it they stay open
// (tests, isolated tooling).
func WithAdminGuard(g kernel.Guard) ServiceOption {
	return func(s *Service) { s.guard = g }
}

// WithSelfAuth resolves the session cookie for the /api-keys
// surface, which is always scoped to the caller.
func WithSelfAuth(auth kernel.Authenticator) ServiceOption {
	return func(s *Service) { s.selfAuth = auth }
}

// NewService builds the feature on the given store.
func NewService(store Store, recorder identity.Recorder, opts ...ServiceOption) *Service {
	s := &Service{store: store, recorder: recorder}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Name names the feature for logs.
func (s *Service) Name() string { return "apikey" }

func (s *Service) record(action, actor, target string) {
	if s.recorder == nil {
		return
	}
	s.recorder(context.Background(), identity.AuditEvent{Action: action, Actor: actor, Target: target})
}

// Create mints a key for the current user and returns it with the
// one-time token.
func (s *Service) Create(ctx context.Context, userID string, params CreateParams) (APIKey, string, error) {
	if err := params.Validate(); err != nil {
		return APIKey{}, "", err
	}

	token, err := NewToken()
	if err != nil {
		return APIKey{}, "", err
	}
	k, err := s.store.Create(ctx, userID, HashToken(token), params)
	if err != nil {
		return APIKey{}, "", err
	}
	s.record("api_key.created", userID, k.ID.String())
	return k, token, nil
}

// List returns the caller's keys with pagination.
func (s *Service) List(ctx context.Context, userID string, params ListParams) ([]APIKey, int, error) {
	return s.store.ListForUser(ctx, userID, params)
}

// Revoke deletes one of the caller's keys.
func (s *Service) Revoke(ctx context.Context, userID string, id APIKeyID) error {
	if err := s.store.Revoke(ctx, userID, id); err != nil {
		return err
	}
	s.record("api_key.revoked", userID, id.String())
	return nil
}

// Renew rotates an expired key and returns the new one-time token.
func (s *Service) Renew(ctx context.Context, userID string, id APIKeyID, params RenewParams) (APIKey, string, error) {
	if err := params.Validate(); err != nil {
		return APIKey{}, "", err
	}

	token, err := NewToken()
	if err != nil {
		return APIKey{}, "", err
	}
	k, err := s.store.Renew(ctx, userID, id, HashToken(token), params.ExpiresAt)
	if err != nil {
		return APIKey{}, "", err
	}
	s.record("api_key.renewed", userID, k.ID.String())
	return k, token, nil
}

// Verify resolves an X-API-KEY value to a principal for the
// transport middleware. Disabled users are rejected here.
func (s *Service) Verify(ctx context.Context, rawKey string) (kernel.Principal, error) {
	if rawKey == "" {
		return kernel.Principal{}, ErrInvalidCreds
	}

	_, u, err := s.store.ValidByHash(ctx, HashToken(rawKey))
	if err != nil {
		return kernel.Principal{}, err
	}
	if u.Disabled {
		return kernel.Principal{}, ErrInvalidCreds
	}
	return kernel.Principal{
		UserID:   u.ID.String(),
		Username: u.Username,
		Email:    u.Email,
		Provider: "api_key",
		IsAdmin:  u.IsAdmin,
	}, nil
}

// currentSelf resolves the session principal for self-scoped routes.
func (s *Service) currentSelf(r *http.Request) (string, bool) {
	if s.selfAuth == nil {
		return "", false
	}
	cookie, err := r.Cookie(session.CookieName)
	if err != nil || cookie.Value == "" {
		return "", false
	}
	p, err := s.selfAuth.ResolveSession(r.Context(), cookie.Value)
	if err != nil {
		return "", false
	}
	return p.UserID, true
}
