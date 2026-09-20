package recovery

// handler.go is the HTTP surface of the recovery flow: it parses
// requests, calls the use case, and maps its sentinel/typed errors
// onto the response envelope.

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-ozzo/ozzo-validation/v4"

	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/modules/identity/password"
	"github.com/riipandi/tango/pkg/responder"
	"github.com/riipandi/tango/pkg/validate"
)

// Feature is the wireable recovery HTTP unit (anonymous endpoints).
type Feature struct {
	recovery *Recovery
}

// NewFeature wires the recovery flow into the HTTP surface.
func NewFeature(recovery *Recovery) Feature {
	return Feature{recovery: recovery}
}

// Name names the feature for logs.
func (f Feature) Name() string { return "password-recovery" }

// APIRoutes mounts the anonymous recovery endpoints relative to the
// shared /api group.
func (f Feature) APIRoutes(r chi.Router, _ identity.RouteGroups) {
	r.Post("/auth/forgot-password", f.handleForgot)
	r.Post("/auth/reset-password", f.handleReset)
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
func (f Feature) handleForgot(w http.ResponseWriter, req *http.Request) {
	var body forgotRequest
	if verr := validate.Request(req.Body, &body); verr != nil {
		responder.Fail(w, req, http.StatusUnprocessableEntity, "validation failed",
			responder.WithError(validate.FieldErrors(verr)))
		return
	}
	if err := f.recovery.ForgotPassword(req.Context(), body.Identity); err != nil {
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

// handleReset serves POST /auth/reset-password: consumes the token,
// starts the fresh session, and returns the session token in the body.
func (f Feature) handleReset(w http.ResponseWriter, req *http.Request) {
	var body resetRequest
	if verr := validate.Request(req.Body, &body); verr != nil {
		responder.Fail(w, req, http.StatusUnprocessableEntity, "validation failed",
			responder.WithError(validate.FieldErrors(verr)))
		return
	}

	u, sessionToken, err := f.recovery.ResetPassword(req.Context(), body.Token, body.NewPassword)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			responder.Fail(w, req, http.StatusNotFound, "reset request is invalid or expired")
			return
		}
		if errors.Is(err, password.ErrWeakPassword) || errors.Is(err, password.ErrOversizeSecret) {
			responder.Fail(w, req, http.StatusUnprocessableEntity, err.Error())
			return
		}
		responder.Fail(w, req, http.StatusInternalServerError, "internal error")
		return
	}

	responder.Success(w, req, http.StatusOK, map[string]any{
		"user":          u,
		"session_token": sessionToken,
	})
}
