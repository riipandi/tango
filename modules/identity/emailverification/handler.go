package emailverification

// handler.go owns the email verification HTTP surface: send (self)
// and verify (self, token body). Mail delivery rides the antree
// queue from phase 7.

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-ozzo/ozzo-validation/v4"

	"github.com/riipandi/tango/internal/kernel"
	"github.com/riipandi/tango/internal/mailer"
	"github.com/riipandi/tango/internal/transport/middleware"
	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/modules/identity/token"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/pkg/responder"
	"github.com/riipandi/tango/pkg/validate"
)

// Service orchestrates the verification flow.
type Service struct {
	store    token.Store
	verifier Verifier
	users    user.Store
	recorder identity.Recorder

	// sender queues the verification mail; nil leaves the feature
	// without delivery (tests, isolated tooling).
	sender identity.MailSender
	appURL string
}

// ServiceOption configures the feature.
type ServiceOption func(*Service)

// WithMail wires the queued email sender and the base URL behind the
// verification link.
func WithMail(sender identity.MailSender, users user.Store, appURL string) ServiceOption {
	return func(s *Service) {
		s.sender = sender
		s.users = users
		s.appURL = strings.TrimRight(appURL, "/")
	}
}

// NewService builds the feature.
func NewService(store token.Store, verifier Verifier, recorder identity.Recorder, opts ...ServiceOption) *Service {
	s := &Service{store: store, verifier: verifier, recorder: recorder}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Name implements identity.Feature.
func (s *Service) Name() string { return "emailverification" }

// Feature is the wireable unit.
type Feature struct {
	service  *Service
	selfAuth kernel.Authenticator
	cookie   string
}

// New wires the feature to its service.
func New(service *Service) Feature { return Feature{service: service} }

// Name implements identity.Feature.
func (Feature) Name() string { return "emailverification" }

// WithSelfAuth registers the session resolver (verification is
// self-service only).
func (f Feature) WithSelfAuth(auth kernel.Authenticator, cookieName string) Feature {
	f.selfAuth, f.cookie = auth, cookieName
	return f
}

// APIRoutes mounts the endpoints relative to the /api group.
func (f Feature) APIRoutes(r chi.Router) {
	if f.selfAuth == nil {
		return // fail closed: verification is self-service
	}
	self := r.With(middleware.RequireAuth(f.selfAuth, f.cookie))
	self.Post("/users/me/send-email-verification", f.service.handleSend)
	self.Post("/users/me/verify-email", f.service.handleVerify)
}

// handleSend serves POST /users/me/send-email-verification: mints a
// token for the current user and queues the verification email.
// Upstream parity: 204 with no body — the token travels by email only.
func (s *Service) handleSend(w http.ResponseWriter, r *http.Request) {
	principal, ok := middleware.PrincipalFromContext(r.Context())
	if !ok {
		responder.Fail(w, r, http.StatusUnauthorized, "authentication required")
		return
	}
	userID, err := identity.ParseID[user.UserID](principal.UserID)
	if err != nil {
		responder.Fail(w, r, http.StatusUnauthorized, "authentication required")
		return
	}

	raw, err := s.mint(r.Context(), userID)
	if err != nil {
		responder.Fail(w, r, http.StatusInternalServerError, "failed to create verification token")
		return
	}
	if err := s.sendVerificationEmail(r.Context(), userID, raw); err != nil {
		responder.Fail(w, r, http.StatusInternalServerError, "failed to queue email")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// sendVerificationEmail queues the verification mail for the user. A
// queued send means the SMTP transaction happens on a worker; a
// failure here only reports that the task row was not written.
func (s *Service) sendVerificationEmail(ctx context.Context, userID user.UserID, token string) error {
	if s.sender == nil {
		return nil
	}

	u, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return err
	}

	return s.sender.EnqueueEmail(ctx, mailer.Message{
		To:       u.Email,
		Subject:  "Verify your email address",
		Template: "email-verification",
		Data: map[string]any{
			"UserFullName":     displayNameOf(u),
			"VerificationLink": s.appURL + "/verify-email?token=" + token,
		},
	})
}

// displayNameOf prefers the account's display name for the greeting.
func displayNameOf(u user.User) string {
	if u.DisplayName != "" {
		return u.DisplayName
	}
	return u.Username
}

// verifyRequest is the POST verify-email body.
type verifyRequest struct {
	Token string `json:"token"`
}

func (r verifyRequest) Validate() error {
	return validation.ValidateStruct(&r,
		validation.Field(&r.Token, validation.Required),
	)
}

// handleVerify serves POST /users/me/verify-email: consumes the
// token and marks the email verified.
func (s *Service) handleVerify(w http.ResponseWriter, r *http.Request) {
	principal, ok := middleware.PrincipalFromContext(r.Context())
	if !ok {
		responder.Fail(w, r, http.StatusUnauthorized, "authentication required")
		return
	}
	userID, err := identity.ParseID[user.UserID](principal.UserID)
	if err != nil {
		responder.Fail(w, r, http.StatusUnauthorized, "authentication required")
		return
	}

	var req verifyRequest
	if verr := validate.Request(r.Body, &req); verr != nil {
		responder.Fail(w, r, http.StatusUnprocessableEntity, "validation failed",
			responder.WithError(validate.FieldErrors(verr)))
		return
	}

	sum := sha256.Sum256([]byte(req.Token))
	consumed, err := s.store.Consume(r.Context(), base64.RawURLEncoding.EncodeToString(sum[:]))
	if err != nil {
		responder.Fail(w, r, http.StatusNotFound, ErrNotFound.Error())
		return
	}
	// consumed.UserID is the bare UUID column form; compare in the
	// same shape (typeid String() would never match).
	if consumed.UserID != userID.UUID() {
		// Tokens are single-user: a mismatched owner is invalid.
		responder.Fail(w, r, http.StatusNotFound, ErrNotFound.Error())
		return
	}

	if err := s.verifier.MarkEmailVerified(r.Context(), userID.String()); err != nil {
		responder.Fail(w, r, http.StatusInternalServerError, "internal error")
		return
	}
	if s.recorder != nil {
		s.recorder(r.Context(), identity.AuditEvent{Action: "email.verified", Actor: userID.String()})
	}
	w.WriteHeader(http.StatusNoContent)
}

// mint creates a fresh verification token; returns the raw value.
func (s *Service) mint(ctx context.Context, userID user.UserID) (string, error) {
	raw, err := token.NewRaw()
	if err != nil {
		return "", err
	}

	tok := token.Token{
		UserID:    userID.UUID(),
		TokenHash: token.Hash(raw),
		ExpiresAt: time.Now().UTC().Add(TokenTTL),
	}
	if err := s.store.Upsert(ctx, &tok); err != nil {
		return "", err
	}
	return raw, nil
}
