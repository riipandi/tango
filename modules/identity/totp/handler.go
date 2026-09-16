package totp

// handler.go owns the TOTP HTTP surface: self-guarded lifecycle
// endpoints plus the anonymous pending-auth verification.

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-ozzo/ozzo-validation/v4"

	"github.com/riipandi/tango/internal/transport/middleware"
	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/modules/identity/session"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/pkg/responder"
	"github.com/riipandi/tango/pkg/validate"
)

// Feature is the wireable TOTP HTTP unit.
type Feature struct {
	service      *Service
	cookieSecure bool
}

// NewFeature wires the TOTP service into the HTTP surface.
func NewFeature(service *Service) Feature {
	return Feature{service: service}
}

// WithCookie mirrors the session cookie Secure flag for the pending
// cookie the verify endpoint rotates.
func (f Feature) WithCookie(secure bool) Feature {
	f.cookieSecure = secure
	return f
}

// Name names the feature for logs.
func (f Feature) Name() string { return "totp" }

// APIRoutes mounts the TOTP endpoints relative to the /api group.
// Without a self group only the anonymous verification mounts.
func (f Feature) APIRoutes(r chi.Router, g identity.RouteGroups) {
	r.Post("/mfa/totp/verify", f.service.handleVerify(f.cookieSecure))

	if g.Self == nil {
		return
	}
	self := r.With(g.Self)
	self.Post("/mfa/totp/enroll", f.service.handleEnroll)
	self.Post("/mfa/totp/confirm", f.service.handleConfirm)
	self.Get("/mfa/totp/status", f.service.handleStatus)
	self.Post("/mfa/totp/recovery-codes", f.service.handleRotate)
	self.Delete("/mfa/totp", f.service.handleDisable)
}

// currentUser resolves the principal to the account row.
func (s *Service) currentUser(w http.ResponseWriter, r *http.Request) (user.User, bool) {
	principal, ok := middleware.PrincipalFromContext(r.Context())
	if !ok {
		responder.Fail(w, r, http.StatusUnauthorized, "authentication required")
		return user.User{}, false
	}
	parsed, err := identity.ParseID[user.UserID](principal.UserID)
	if err != nil {
		responder.Fail(w, r, http.StatusUnauthorized, "authentication required")
		return user.User{}, false
	}
	u, err := s.users.GetByID(r.Context(), parsed)
	if err != nil {
		responder.Fail(w, r, http.StatusUnauthorized, "authentication required")
		return user.User{}, false
	}
	return u, true
}

// enrollResponse carries the enrollment secret and provisioning URI;
// the plaintext secret never appears in any later response.
type enrollResponse struct {
	Secret          string `json:"secret"`
	ProvisioningURI string `json:"provisioning_uri"`
}

// handleEnroll serves POST /mfa/totp/enroll.
func (s *Service) handleEnroll(w http.ResponseWriter, r *http.Request) {
	u, ok := s.currentUser(w, r)
	if !ok {
		return
	}
	secret, uri, err := s.Enroll(r.Context(), u)
	if err != nil {
		writeMFAError(w, r, err)
		return
	}
	responder.Success(w, r, http.StatusCreated, enrollResponse{
		Secret:          secret,
		ProvisioningURI: uri,
	})
}

// confirmRequest is the POST confirm payload.
type confirmRequest struct {
	Code string `json:"code"`
}

func (r confirmRequest) Validate() error {
	return validation.ValidateStruct(&r,
		validation.Field(&r.Code, validation.Required),
	)
}

// handleConfirm serves POST /mfa/totp/confirm: the recovery codes
// come back exactly once.
func (s *Service) handleConfirm(w http.ResponseWriter, r *http.Request) {
	u, ok := s.currentUser(w, r)
	if !ok {
		return
	}
	var body confirmRequest
	if verr := validate.Request(r.Body, &body); verr != nil {
		responder.Fail(w, r, http.StatusUnprocessableEntity, "validation failed",
			responder.WithError(validate.FieldErrors(verr)))
		return
	}
	codes, err := s.Confirm(r.Context(), u, body.Code)
	if err != nil {
		writeMFAError(w, r, err)
		return
	}
	responder.Success(w, r, http.StatusOK, map[string]any{"recovery_codes": codes})
}

// statusResponse is the GET status payload.
type statusResponse struct {
	Confirmed              bool `json:"confirmed"`
	RecoveryCodesRemaining int  `json:"recovery_codes_remaining"`
}

// handleStatus serves GET /mfa/totp/status.
func (s *Service) handleStatus(w http.ResponseWriter, r *http.Request) {
	u, ok := s.currentUser(w, r)
	if !ok {
		return
	}
	confirmed, remaining, err := s.Status(r.Context(), u)
	if err != nil {
		responder.Fail(w, r, http.StatusInternalServerError, "internal error")
		return
	}
	responder.Success(w, r, http.StatusOK, statusResponse{
		Confirmed:              confirmed,
		RecoveryCodesRemaining: remaining,
	})
}

// verifyRequest is the POST verify payload: a TOTP code or a
// recovery code, indistinguishable to the server.
type verifyRequest struct {
	Code string `json:"code"`
}

func (r verifyRequest) Validate() error {
	return validation.ValidateStruct(&r,
		validation.Field(&r.Code, validation.Required),
	)
}

// handleVerify serves POST /mfa/totp/verify: consumes the pending
// bridge and issues the only full session.
func (s *Service) handleVerify(secure bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body verifyRequest
		if verr := validate.Request(r.Body, &body); verr != nil {
			responder.Fail(w, r, http.StatusUnprocessableEntity, "validation failed",
				responder.WithError(validate.FieldErrors(verr)))
			return
		}

		pendingCookie, err := r.Cookie(identity.PendingCookieName)
		if err != nil || pendingCookie.Value == "" {
			responder.Fail(w, r, http.StatusUnauthorized, "verification required")
			return
		}

		u, token, err := s.VerifyPending(r.Context(), pendingCookie.Value, body.Code)
		if err != nil {
			writeMFAError(w, r, err)
			return
		}

		// Rotate: the pending cookie dies with the bridge; the fresh
		// session cookie takes over.
		http.SetCookie(w, &http.Cookie{ // #nosec G124 -- session cookie parity (SameSite=Lax, Secure off in dev)
			Name:     session.CookieName,
			Value:    token,
			Path:     "/",
			MaxAge:   0,
			HttpOnly: true,
			Secure:   secure,
		})
		http.SetCookie(w, &http.Cookie{ // #nosec G124 -- session cookie parity (SameSite=Lax, Secure off in dev)
			Name:     identity.PendingCookieName,
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			HttpOnly: true,
			Secure:   secure,
		})
		responder.Success(w, r, http.StatusOK, u)
	}
}

// rotateRequest is the POST recovery-codes payload.
type rotateRequest struct {
	Code string `json:"code"`
}

func (r rotateRequest) Validate() error {
	return validation.ValidateStruct(&r,
		validation.Field(&r.Code, validation.Required),
	)
}

// handleRotate serves POST /mfa/totp/recovery-codes.
func (s *Service) handleRotate(w http.ResponseWriter, r *http.Request) {
	u, ok := s.currentUser(w, r)
	if !ok {
		return
	}
	var body rotateRequest
	if verr := validate.Request(r.Body, &body); verr != nil {
		responder.Fail(w, r, http.StatusUnprocessableEntity, "validation failed",
			responder.WithError(validate.FieldErrors(verr)))
		return
	}
	codes, err := s.RotateRecoveryCodes(r.Context(), u, body.Code)
	if err != nil {
		writeMFAError(w, r, err)
		return
	}
	responder.Success(w, r, http.StatusOK, map[string]any{"recovery_codes": codes})
}

// disableRequest is the DELETE payload: the current password.
type disableRequest struct {
	Password string `json:"password"`
}

func (r disableRequest) Validate() error {
	return validation.ValidateStruct(&r,
		validation.Field(&r.Password, validation.Required),
	)
}

// handleDisable serves DELETE /mfa/totp.
func (s *Service) handleDisable(w http.ResponseWriter, r *http.Request) {
	u, ok := s.currentUser(w, r)
	if !ok {
		return
	}
	var body disableRequest
	if verr := validate.Request(r.Body, &body); verr != nil {
		responder.Fail(w, r, http.StatusUnprocessableEntity, "validation failed",
			responder.WithError(validate.FieldErrors(verr)))
		return
	}
	if err := s.Disable(r.Context(), u, body.Password); err != nil {
		writeMFAError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// writeMFAError maps service failures to statuses without leaking
// which check failed.
func writeMFAError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrAlreadyConfirmed):
		responder.Fail(w, r, http.StatusConflict, err.Error())
	case errors.Is(err, ErrInvalidCode), errors.Is(err, ErrNoEnrollment), errors.Is(err, ErrNotConfirmed):
		// Wrong code, unknown enrollment, and unconfirmed seeds all
		// answer the same generic failure.
		responder.Fail(w, r, http.StatusUnauthorized, "verification failed")
	default:
		responder.Fail(w, r, http.StatusInternalServerError, "internal error")
	}
}
