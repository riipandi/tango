package onetimeaccess

// handler_rest.go owns the one-time access HTTP surface: the
// email-link token→session exchange. Admin minting and the email
// request serve ConnectRPC (handler_rpc.go); mail delivery uses the
// mailer contract.

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
		s.recorder.Record(ctx, identity.AuditEvent{Action: action, Actor: actor}, nil)
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

// Name names the feature for logs.
func (s *Service) Name() string { return "onetimeaccess" }

// Feature is the wireable unit.
type Feature struct {
	service *Service
	access  kernel.AccessAuthenticator
}

// New wires the feature to its service.
func New(service *Service) Feature { return Feature{service: service} }

// Name names the feature for logs.
func (Feature) Name() string { return "onetimeaccess" }

// WithAccessAuthenticator wires the bearer resolver the admin
// procedures guard with.
func (f Feature) WithAccessAuthenticator(access kernel.AccessAuthenticator) Feature {
	f.access = access
	return f
}

// RPCService returns the Connect registration for the one-time
// access surface.
func (f Feature) RPCService() (string, http.Handler) {
	return f.service.RPCService(f.access)
}

// APIRoutes mounts the retained one-time access endpoints relative
// to the /api group: only the email-link token exchange. The email
// request and the admin surface serve ConnectRPC below /rpc.
func (f Feature) APIRoutes(r chi.Router, _ identity.RouteGroups) {
	r.Post("/one-time-access-token/{token}", f.service.handleExchange)
}

// handleExchange serves POST /one-time-access-token/{token}
// (anonymous): consumes the token and starts the session.
func (s *Service) handleExchange(w http.ResponseWriter, r *http.Request) {
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

	responder.Success(w, r, http.StatusOK, map[string]any{
		"user":          u,
		"session_token": token,
	})
}

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

	token, _, issueErr := s.sessions.IssueForUser(ctx, userID, "one_time_access", session.Meta{})
	if issueErr != nil {
		return user.User{}, "", issueErr
	}
	s.record(ctx, "user.signed_in", userID.String())
	return u, token, nil
}

// parseUserIDParam resolves the {id} path segment.
// validateEmail checks the address shape (mirrors user module).
func validateEmail(email string) error {
	if email == "" {
		return errors.New("email is required")
	}
	return nil
}
