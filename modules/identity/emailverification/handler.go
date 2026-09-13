package emailverification

// handler.go owns the email verification HTTP surface: send (self)
// and verify (self, token body). Mail delivery rides the antree
// queue from phase 7.

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-ozzo/ozzo-validation/v4"

	"github.com/riipandi/tango/internal/transport/middleware"
	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/pkg/responder"
	"github.com/riipandi/tango/pkg/validate"
)

// Service orchestrates the verification flow.
type Service struct {
	store    Store
	verifier Verifier
	recorder identity.Recorder
}

// NewService builds the feature.
func NewService(store Store, verifier Verifier, recorder identity.Recorder) *Service {
	return &Service{store: store, verifier: verifier, recorder: recorder}
}

// Name implements identity.Feature.
func (s *Service) Name() string { return "emailverification" }

// Feature is the wireable unit.
type Feature struct {
	service  *Service
	selfAuth middleware.Authenticator
	cookie   string
}

// New wires the feature to its service.
func New(service *Service) Feature { return Feature{service: service} }

// Name implements identity.Feature.
func (Feature) Name() string { return "emailverification" }

// WithSelfAuth registers the session resolver (verification is
// self-service only).
func (f Feature) WithSelfAuth(auth middleware.Authenticator, cookieName string) Feature {
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
// token for the current user. Mail rides the antree queue (phase
// 7); today the token is returned for local testing only.
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
	// Production: no body (mail goes out async). Development keeps
	// the token visible for the Yaak ceremony check.
	responder.Success(w, r, http.StatusOK, map[string]any{"token": raw})
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
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	raw := base64.RawURLEncoding.EncodeToString(buf)

	sum := sha256.Sum256([]byte(raw))
	token := Token{
		UserID:    userID.String(),
		TokenHash: base64.RawURLEncoding.EncodeToString(sum[:]),
		ExpiresAt: time.Now().UTC().Add(TokenTTL),
	}
	if err := s.store.Upsert(ctx, &token); err != nil {
		return "", err
	}
	return raw, nil
}
