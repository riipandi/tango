package emailverification

// handler.go owns the email verification HTTP surface: the verify
// email link. SendEmail serves ConnectRPC (handler_rpc.go); mail
// delivery uses the built-in queue.

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

// Name names the feature for logs.
func (s *Service) Name() string { return "emailverification" }

// Feature is the wireable unit.
type Feature struct {
	service *Service
	access  kernel.AccessAuthenticator
}

// New wires the feature to its service.
func New(service *Service) Feature { return Feature{service: service} }

// Name names the feature for logs.
func (Feature) Name() string { return "emailverification" }

// WithAccessAuthenticator wires the bearer resolver the self
// procedures guard with.
func (f Feature) WithAccessAuthenticator(access kernel.AccessAuthenticator) Feature {
	f.access = access
	return f
}

// RPCService returns the Connect registration for the email
// verification surface.
func (f Feature) RPCService() (string, http.Handler) {
	return f.service.RPCService(f.access)
}

// APIRoutes mounts the retained endpoints relative to the /api
// group: only the email-link verify step. The send action serves
// ConnectRPC below /rpc. Without a self group nothing mounts (fail
// closed: verification is self-service only).
func (f Feature) APIRoutes(r chi.Router, g identity.RouteGroups) {
	if g.Self == nil {
		return
	}
	self := r.With(g.Self)
	self.Post("/users/me/verify-email", f.service.handleVerify)
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
		s.recorder.Record(r.Context(), identity.AuditEvent{Action: "email.verified", Actor: userID.String()}, nil)
	}
	w.WriteHeader(http.StatusNoContent)
}

// sendVerificationEmail queues the verification mail for the user;
// the token travels by email only.
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

// verifyRequest is the POST /users/me/verify-email body.
type verifyRequest struct {
	Token string `json:"token"`
}

func (r verifyRequest) Validate() error {
	return validation.ValidateStruct(&r,
		validation.Field(&r.Token, validation.Required),
	)
}

// displayNameOf prefers the display name, falling back to username.
func displayNameOf(u user.User) string {
	if u.DisplayName != "" {
		return u.DisplayName
	}
	return u.Username
}
