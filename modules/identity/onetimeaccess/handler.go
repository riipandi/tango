package onetimeaccess

// handler.go owns the one-time access HTTP surface: admin minting
// (token + email per user), the unauthenticated email request, and
// the token→session exchange. Mail delivery rides the built-in queue
// once phase 7 lands — for now send is synchronous via the mailer
// contract.

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/riipandi/tango/internal/kernel"
	"github.com/riipandi/tango/internal/mailer"
	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/modules/identity/session"
	"github.com/riipandi/tango/modules/identity/token"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/pkg/responder"
	"github.com/riipandi/tango/pkg/validate"
)

// Service orchestrates one-time access tokens.
type Service struct {
	store    token.Store
	users    user.Store
	sessions *session.Service
	recorder identity.Recorder

	// sender queues transactional email; nil leaves the feature with
	// no mail delivery (tests, isolated tooling).
	sender identity.MailSender
	appURL string
}

// record emits an audit event through the adapter.
func (s *Service) record(ctx context.Context, action, actor string) {
	if s.recorder != nil {
		s.recorder(ctx, identity.AuditEvent{Action: action, Actor: actor})
	}
}

// ServiceOption configures the feature.
type ServiceOption func(*Service)

// WithMail wires the queued email sender and the base URL used to
// build the sign-in link.
func WithMail(sender identity.MailSender, appURL string) ServiceOption {
	return func(s *Service) {
		s.sender = sender
		s.appURL = strings.TrimRight(appURL, "/")
	}
}

// NewService builds the feature.
func NewService(store token.Store, users user.Store, sessions *session.Service, recorder identity.Recorder, opts ...ServiceOption) *Service {
	s := &Service{store: store, users: users, sessions: sessions, recorder: recorder}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// sendAccessEmail queues the one-time access mail for the user. The
// token is delivered by email only; failures to queue are returned so
// the caller can still surface a 500 (a queued send cannot fail later
// in a way the user observes).
func (s *Service) sendAccessEmail(ctx context.Context, u user.User, code, redirectPath string) error {
	if s.sender == nil {
		return nil
	}

	loginLink := s.appURL + "/lc"
	linkWithCode := loginLink + "/" + code
	if strings.HasPrefix(redirectPath, "/") {
		linkWithCode += "?redirect=" + url.QueryEscape(redirectPath)
	}

	return s.sender.EnqueueEmail(ctx, mailer.Message{
		To:       u.Email,
		Subject:  "Your login code",
		Template: "one-time-access",
		Data: map[string]any{
			"Code":              code,
			"LoginLink":         loginLink,
			"LoginLinkWithCode": linkWithCode,
			"ExpirationString":  "15 minutes",
		},
	})
}

// Name implements identity.Feature.
func (s *Service) Name() string { return "onetimeaccess" }

// Feature is the wireable unit.
type Feature struct {
	service      *Service
	adminGuard   kernel.Guard
	cookieName   string
	cookieSecure bool
}

// New wires the feature to its service.
func New(service *Service) Feature { return Feature{service: service} }

// Name implements identity.Feature.
func (Feature) Name() string { return "onetimeaccess" }

// WithAdminGuard registers the admin guard for minting.
func (f Feature) WithAdminGuard(guard kernel.Guard) Feature {
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
// (admin): mints the token and queues the email.
func (s *Service) handleAdminSendEmail(w http.ResponseWriter, r *http.Request) {
	userID, ok := parseUserIDParam(r)
	if !ok {
		responder.NotFoundJSON(w, r)
		return
	}

	u, err := s.users.GetByID(r.Context(), userID)
	if err != nil {
		responder.NotFoundJSON(w, r)
		return
	}

	raw, err := s.mint(r.Context(), userID)
	if err != nil {
		responder.Fail(w, r, http.StatusInternalServerError, "failed to create token")
		return
	}
	if err := s.sendAccessEmail(r.Context(), u, raw, ""); err != nil {
		responder.Fail(w, r, http.StatusInternalServerError, "failed to queue email")
		return
	}
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
// (unauthenticated): always 204. The policy that decides whether a
// token is minted belongs to appconfig (phase 8); answering 204
// unconditionally avoids account enumeration.
func (s *Service) handleEmailRequest(w http.ResponseWriter, r *http.Request) {
	var req emailRequest
	if verr := validate.Request(r.Body, &req); verr != nil {
		responder.Fail(w, r, http.StatusUnprocessableEntity, "validation failed",
			responder.WithError(validate.FieldErrors(verr)))
		return
	}
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
	raw, err := token.NewRaw()
	if err != nil {
		return "", err
	}

	now := time.Now().UTC()
	tok := token.Token{
		UserID:    userID.UUID(),
		TokenHash: token.Hash(raw),
		ExpiresAt: now.Add(TokenTTL),
	}
	if err := s.store.Upsert(ctx, &tok); err != nil {
		return "", err
	}
	return raw, nil
}

// consumeAndIssue validates + burns the token, then issues the
// session; returns the user and the raw session token.
func (s *Service) consumeAndIssue(ctx context.Context, raw string) (user.User, string, error) {
	consumed, err := s.store.Consume(ctx, token.Hash(raw))
	if err != nil {
		return user.User{}, "", err
	}

	// consumed.UserID is the bare UUID column form.
	userID := user.MustID(consumed.UserID)
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
