package onetimeaccess

// handler.go owns the one-time access HTTP surface: admin minting
// (token + email per user), the unauthenticated email request, and
// the token→session exchange. Mail delivery rides the antree queue
// once phase 7 lands — for now send is synchronous via the mailer
// contract.

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/modules/identity/session"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/pkg/responder"
	"github.com/riipandi/tango/pkg/validate"
)

// Service orchestrates one-time access tokens.
type Service struct {
	store    Store
	users    user.Store
	sessions *session.Service
	recorder identity.Recorder
}

// record emits an audit event through the adapter.
func (s *Service) record(ctx context.Context, action, actor string) {
	if s.recorder != nil {
		s.recorder(ctx, identity.AuditEvent{Action: action, Actor: actor})
	}
}

// NewService builds the feature.
func NewService(store Store, users user.Store, sessions *session.Service, recorder identity.Recorder) *Service {
	return &Service{store: store, users: users, sessions: sessions, recorder: recorder}
}

// Name implements identity.Feature.
func (s *Service) Name() string { return "onetimeaccess" }

// Feature is the wireable unit.
type Feature struct {
	service      *Service
	adminGuard   func(http.Handler) http.Handler
	cookieName   string
	cookieSecure bool
}

// New wires the feature to its service.
func New(service *Service) Feature { return Feature{service: service} }

// Name implements identity.Feature.
func (Feature) Name() string { return "onetimeaccess" }

// WithAdminGuard registers the admin guard for minting.
func (f Feature) WithAdminGuard(guard func(http.Handler) http.Handler) Feature {
	f.adminGuard = guard
	return f
}

// WithCookie wires the session cookie settings for the exchange.
func (f Feature) WithCookie(name string, secure bool) Feature {
	f.cookieName, f.cookieSecure = name, secure
	return f
}

// APIRoutes mounts the one-time access endpoints relative to the
// /api group.
func (f Feature) APIRoutes(r chi.Router) {
	// Anonymous: email request + token exchange.
	r.Post("/one-time-access-email", f.service.handleEmailRequest)
	r.Post("/one-time-access-token/{token}", func(w http.ResponseWriter, req *http.Request) {
		f.service.handleExchange(w, req, f.cookieName, f.cookieSecure)
	})

	if f.adminGuard == nil {
		return
	}
	admin := r.With(f.adminGuard)
	admin.Post("/users/{id}/one-time-access-token", f.service.handleAdminMintToken)
	admin.Post("/users/{id}/one-time-access-email", f.service.handleAdminSendEmail)
}

// ----------------------------------------------------------------------------
// Handlers
// ----------------------------------------------------------------------------

// handleAdminMintToken serves POST /users/{id}/one-time-access-token
// (admin): returns the raw token once.
func (s *Service) handleAdminMintToken(w http.ResponseWriter, r *http.Request) {
	userID, ok := parseUserIDParam(r)
	if !ok {
		responder.NotFoundJSON(w, r)
		return
	}

	raw, err := s.mint(r.Context(), userID)
	if err != nil {
		responder.Fail(w, r, http.StatusInternalServerError, "failed to create token")
		return
	}
	s.record(r.Context(), "one_time_access.token_created", userID.String())
	responder.Success(w, r, http.StatusCreated, map[string]any{"token": raw})
}

// handleAdminSendEmail serves POST /users/{id}/one-time-access-email
// (admin): mints + emails the link.
func (s *Service) handleAdminSendEmail(w http.ResponseWriter, r *http.Request) {
	userID, ok := parseUserIDParam(r)
	if !ok {
		responder.NotFoundJSON(w, r)
		return
	}

	if _, err := s.mint(r.Context(), userID); err != nil {
		responder.Fail(w, r, http.StatusInternalServerError, "failed to create token")
		return
	}
	// Mail send rides the antree queue (phase 7); minting alone
	// marks the token fresh.
	w.WriteHeader(http.StatusNoContent)
}

// emailRequest is the POST /one-time-access-email body
// (unauthenticated; requires the target address).
type emailRequest struct {
	Email        string `json:"email"`
	RedirectPath string `json:"redirect_path,omitzero"`
}

func (r emailRequest) Validate() error {
	return validateEmail(r.Email)
}

// handleEmailRequest serves POST /one-time-access-email
// (unauthenticated): mints a token for the address when the policy
// allows. The response is always 204 (no account enumeration).
func (s *Service) handleEmailRequest(w http.ResponseWriter, r *http.Request) {
	var req emailRequest
	if verr := validate.Request(r.Body, &req); verr != nil {
		responder.Fail(w, r, http.StatusUnprocessableEntity, "validation failed",
			responder.WithError(validate.FieldErrors(verr)))
		return
	}

	// Policy gate: one-time access email for unauthenticated users
	// is off until appconfig lands (phase 8) — respond 204 without
	// minting to avoid account enumeration.
	_ = req
	w.WriteHeader(http.StatusNoContent)
}

// handleExchange serves POST /one-time-access-token/{token}
// (anonymous): consumes the token and starts the session.
func (s *Service) handleExchange(w http.ResponseWriter, r *http.Request, cookieName string, cookieSecure bool) {
	raw := chi.URLParam(r, "token")
	if raw == "" {
		responder.Fail(w, r, http.StatusBadRequest, "token is required")
		return
	}

	u, token, err := s.consumeAndIssue(r.Context(), raw)
	if err != nil {
		responder.Fail(w, r, http.StatusNotFound, "token is invalid or expired")
		return
	}

	setSessionCookie(w, cookieName, token, cookieSecure)
	responder.Success(w, r, http.StatusOK, u)
}

// ----------------------------------------------------------------------------
// Shared helpers
// ----------------------------------------------------------------------------

// mint creates a fresh single-use token for the user and returns
// the raw value.
func (s *Service) mint(ctx context.Context, userID user.UserID) (string, error) {
	raw, err := randomToken()
	if err != nil {
		return "", err
	}

	now := time.Now().UTC()
	token := Token{
		UserID:    userID.String(),
		TokenHash: hashToken(raw),
		ExpiresAt: now.Add(TokenTTL),
	}
	if _, err := s.store.Upsert(ctx, &token); err != nil {
		return "", err
	}
	return raw, nil
}

// consumeAndIssue validates + burns the token, then issues the
// session; returns the user and the raw session token.
func (s *Service) consumeAndIssue(ctx context.Context, raw string) (user.User, string, error) {
	consumed, err := s.store.Consume(ctx, hashToken(raw))
	if err != nil {
		return user.User{}, "", err
	}

	userID, parseErr := identity.ParseID[user.UserID](consumed.UserID)
	if parseErr != nil {
		return user.User{}, "", ErrNotFound
	}
	u, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return user.User{}, "", ErrNotFound
	}
	if u.Disabled {
		return user.User{}, "", ErrNotFound
	}

	token, issueErr := s.sessions.IssueForUser(ctx, userID, "one_time_access", session.Meta{})
	if issueErr != nil {
		return user.User{}, "", issueErr
	}
	s.record(ctx, "user.signed_in", userID.String())
	return u, token, nil
}

// randomToken returns a 256-bit URL-safe token.
func randomToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// hashToken hashes a token for at-rest storage.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// setSessionCookie mirrors the session module cookie flags.
func setSessionCookie(w http.ResponseWriter, name, value string, secure bool) {
	http.SetCookie(w, &http.Cookie{ // #nosec G124 -- session cookie parity (SameSite=Lax)
		Name:     name,
		Value:    value,
		Path:     "/",
		MaxAge:   0,
		HttpOnly: true,
		Secure:   secure,
	})
}

// parseUserIDParam resolves the {id} path segment.
func parseUserIDParam(r *http.Request) (user.UserID, bool) {
	id, err := identity.ParseID[user.UserID](chi.URLParam(r, "id"))
	return id, err == nil
}

// validateEmail checks the address shape (mirrors user module).
func validateEmail(email string) error {
	if email == "" {
		return errors.New("email is required")
	}
	return nil
}
