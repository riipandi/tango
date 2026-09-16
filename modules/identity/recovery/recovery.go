package recovery

// Package recovery implements the forgot/reset password flow: the
// anonymous request mints a hashed single-use token delivered by
// email only, and the reset consumes it atomically, applies the
// shared password policy, revokes every sign-in session, and issues
// a fresh one.

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-ozzo/ozzo-validation/v4"

	"github.com/riipandi/tango/internal/mailer"
	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/modules/identity/password"
	"github.com/riipandi/tango/modules/identity/session"
	"github.com/riipandi/tango/modules/identity/token"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/pkg/crypto"
	"github.com/riipandi/tango/pkg/responder"
	"github.com/riipandi/tango/pkg/validate"
)

// ResetTokenTTL bounds reset tokens.
const ResetTokenTTL = 15 * time.Minute

// ErrNotFound answers forgot/reset requests without revealing
// whether the address or token exists.
var ErrNotFound = errors.New("recovery: reset request is invalid or expired")

// Sessions are the session operations the reset flow needs. The
// interface keeps this package free of the session import from the
// password package's own tests' perspective — session depends on
// password, so recovery touches it through this narrow contract.
type Sessions interface {
	IssueForUser(ctx context.Context, userID user.UserID, provider string, meta session.Meta) (string, error)
	RevokeAllForUser(ctx context.Context, userID user.UserID, keepID string) error
}

// Recovery runs the forgot/reset password lifecycle.
type Recovery struct {
	tokens      token.Store
	users       user.Store
	credentials password.Store
	sessions    Sessions
	hasher      *crypto.PasswordHasher
	recorder    identity.Recorder

	// sender queues the recovery email; nil leaves the flow without
	// delivery (tests, isolated tooling).
	sender identity.MailSender
	appURL string
}

// RecoveryOption configures the recovery flow.
type RecoveryOption func(*Recovery)

// WithMail wires the queued email sender and the base URL behind the
// reset link.
func WithMail(sender identity.MailSender, appURL string) RecoveryOption {
	return func(r *Recovery) {
		r.sender = sender
		r.appURL = strings.TrimRight(appURL, "/")
	}
}

// New builds the recovery flow.
func New(tokens token.Store, users user.Store, credentials password.Store, sessions Sessions, hasher *crypto.PasswordHasher, recorder identity.Recorder, opts ...RecoveryOption) *Recovery {
	r := &Recovery{
		tokens:      tokens,
		users:       users,
		credentials: credentials,
		sessions:    sessions,
		hasher:      hasher,
		recorder:    recorder,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Name names the feature for logs.
func (r *Recovery) Name() string { return "password-recovery" }

// ForgotPassword resolves the identity, mints a single-use token,
// and queues the recovery email. A missing or credential-less
// address is indistinguishable from a delivered one.
func (r *Recovery) ForgotPassword(ctx context.Context, identityText string) error {
	_, u, err := r.credentials.HashByIdentity(ctx, identityText)
	if err != nil {
		return nil
	}
	if u.Disabled {
		return nil
	}

	raw, err := token.NewRaw()
	if err != nil {
		return err
	}
	sum := sha256.Sum256([]byte(raw))
	if err := r.tokens.Upsert(ctx, &token.Token{
		UserID:    u.ID.UUID(),
		TokenHash: base64.RawURLEncoding.EncodeToString(sum[:]),
		ExpiresAt: time.Now().UTC().Add(ResetTokenTTL),
	}); err != nil {
		return err
	}
	if r.recorder != nil {
		r.recorder.Record(ctx, identity.AuditEvent{Action: "password.reset_requested", Actor: u.ID.String()}, nil)
	}
	if r.sender == nil {
		return nil
	}
	return r.sender.EnqueueEmail(ctx, mailer.Message{
		To:       u.Email,
		Subject:  "Reset your password",
		Template: "password-reset",
		Data: map[string]any{
			"ResetLink": r.appURL + "/reset-password?token=" + raw,
		},
	})
}

// ResetPassword consumes the token atomically, applies the shared
// policy, revokes every sign-in session, and issues a fresh one.
// Invalid, expired, and spent tokens are indistinguishable.
func (r *Recovery) ResetPassword(ctx context.Context, raw, newPassword string) (user.User, string, error) {
	if err := password.ValidateSecret(newPassword); err != nil {
		return user.User{}, "", err
	}

	sum := sha256.Sum256([]byte(raw))
	consumed, err := r.tokens.Consume(ctx, base64.RawURLEncoding.EncodeToString(sum[:]))
	if err != nil {
		return user.User{}, "", ErrNotFound
	}

	// consumed.UserID is the bare UUID column form.
	userID := user.MustID(consumed.UserID)
	if applyErr := r.applyNewSecret(ctx, userID, newPassword); applyErr != nil {
		return user.User{}, "", applyErr
	}

	// Every existing session dies with the old secret; the requester
	// gets the only fresh one.
	if revokeErr := r.sessions.RevokeAllForUser(ctx, userID, ""); revokeErr != nil {
		return user.User{}, "", revokeErr
	}
	if r.recorder != nil {
		r.recorder.Record(ctx, identity.AuditEvent{Action: "password.reset_completed", Actor: userID.String()}, nil)
	}
	sessionToken, err := r.sessions.IssueForUser(ctx, userID, "password_reset", session.Meta{})
	if err != nil {
		return user.User{}, "", err
	}
	u, err := r.users.GetByID(ctx, userID)
	if err != nil {
		return user.User{}, "", err
	}
	return u, sessionToken, nil
}

// applyNewSecret hashes and stores the replacement credential.
func (r *Recovery) applyNewSecret(ctx context.Context, userID user.UserID, newPassword string) error {
	if err := password.ValidateSecret(newPassword); err != nil {
		return err
	}
	hash, err := r.hasher.Hash(newPassword)
	if err != nil {
		return fmt.Errorf("recovery: hash: %w", err)
	}
	if err := r.credentials.Upsert(ctx, userID, hash); err != nil {
		return err
	}
	return nil
}

// Feature is the wireable recovery HTTP unit (anonymous endpoints).
type Feature struct {
	recovery *Recovery
	cookie   string
	secure   bool
}

// NewFeature wires the recovery flow into the HTTP surface.
func NewFeature(recovery *Recovery) Feature {
	return Feature{recovery: recovery}
}

// WithCookie wires the session cookie settings for the fresh session.
func (f Feature) WithCookie(name string, secure bool) Feature {
	f.cookie, f.secure = name, secure
	return f
}

// Name names the feature for logs.
func (f Feature) Name() string { return "password-recovery" }

// APIRoutes mounts the anonymous recovery endpoints relative to the
// shared /api group.
func (f Feature) APIRoutes(r chi.Router, _ identity.RouteGroups) {
	r.Post("/auth/forgot-password", f.recovery.handleForgot)
	r.Post("/auth/reset-password", f.recovery.handleReset(f.cookie, f.secure))
}

// forgotRequest is the POST forgot-password payload.
type forgotRequest struct {
	Identity string `json:"identity"`
}

func (r forgotRequest) Validate() error {
	return validation.ValidateStruct(&r,
		validation.Field(&r.Identity, validation.Required),
	)
}

// handleForgot serves POST /auth/forgot-password: always 204.
func (r *Recovery) handleForgot(w http.ResponseWriter, req *http.Request) {
	var body forgotRequest
	if verr := validate.Request(req.Body, &body); verr != nil {
		responder.Fail(w, req, http.StatusUnprocessableEntity, "validation failed",
			responder.WithError(validate.FieldErrors(verr)))
		return
	}
	if err := r.ForgotPassword(req.Context(), body.Identity); err != nil {
		responder.Fail(w, req, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// resetRequest is the POST reset-password payload.
type resetRequest struct {
	Token       string `json:"token"`
	NewPassword string `json:"new_password"`
}

func (r resetRequest) Validate() error {
	return validation.ValidateStruct(&r,
		validation.Field(&r.Token, validation.Required),
		validation.Field(&r.NewPassword, validation.Required),
	)
}

// handleReset serves POST /auth/reset-password: consumes the token
// and starts the fresh session.
func (r *Recovery) handleReset(cookieName string, secure bool) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		var body resetRequest
		if verr := validate.Request(req.Body, &body); verr != nil {
			responder.Fail(w, req, http.StatusUnprocessableEntity, "validation failed",
				responder.WithError(validate.FieldErrors(verr)))
			return
		}

		u, sessionToken, err := r.ResetPassword(req.Context(), body.Token, body.NewPassword)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				responder.Fail(w, req, http.StatusNotFound, ErrNotFound.Error())
				return
			}
			if errors.Is(err, password.ErrWeakPassword) || errors.Is(err, password.ErrOversizeSecret) {
				responder.Fail(w, req, http.StatusUnprocessableEntity, err.Error())
				return
			}
			responder.Fail(w, req, http.StatusInternalServerError, "internal error")
			return
		}

		http.SetCookie(w, &http.Cookie{ // #nosec G124 -- session cookie parity (SameSite=Lax, Secure off in dev)
			Name:     cookieName,
			Value:    sessionToken,
			Path:     "/",
			MaxAge:   0,
			HttpOnly: true,
			Secure:   secure,
		})
		responder.Success(w, req, http.StatusOK, u)
	}
}
