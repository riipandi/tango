package devicelogin

// handler.go owns the device login HTTP surface: the anonymous
// create/exchange pair (QR device side) and the authenticated
// inspect/decision pair (approving device side). Sign-in exchange
// sets the session cookie via the shared helper contract.

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-ozzo/ozzo-validation/v4"

	"github.com/riipandi/tango/internal/transport/middleware"
	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/pkg/responder"
	"github.com/riipandi/tango/pkg/validate"
)

// Feature is the wireable device login unit.
type Feature struct {
	service      *Service
	selfAuth     middleware.Authenticator
	cookieName   string
	cookieSecure bool
}

var _ identity.APIFeature = (*Feature)(nil)

// New wires the feature to its service.
func New(service *Service) Feature { return Feature{service: service} }

// Name implements identity.Feature.
func (Feature) Name() string { return "devicelogin" }

// WithSelfAuth registers the session resolver + cookie settings for
// the inspect/decision/exchange surfaces.
func (f Feature) WithSelfAuth(auth middleware.Authenticator, cookieName string, secure bool) Feature {
	f.selfAuth, f.cookieName, f.cookieSecure = auth, cookieName, secure
	return f
}

// APIRoutes mounts the device login endpoints relative to the /api
// group. Without a self authenticator the approving surfaces are
// skipped (fail closed).
func (f Feature) APIRoutes(r chi.Router) {
	r.Post("/device-login/requests", f.service.handleCreate)
	r.Post("/device-login/requests/{id}/exchange", f.service.handleExchange)

	if f.selfAuth == nil {
		return
	}
	self := r.With(middleware.RequireAuth(f.selfAuth, f.cookieName))
	self.Post("/device-login/verification", f.service.handleInspect)
	self.Post("/device-login/verification/decision", f.service.handleDecision)
}

// handleCreate serves POST /device-login/requests (anonymous): the
// QR payload + polling device token.
func (s *Service) handleCreate(w http.ResponseWriter, r *http.Request) {
	base := trimSlash(baseURLFrom(r))
	created, deviceToken, err := s.Create(r.Context(), base, requestIP(r), r.UserAgent())
	if err != nil {
		responder.Fail(w, r, http.StatusInternalServerError, "failed to create device login request")
		return
	}

	// The polling device keeps the token secret; it rides a
	// dedicated header on exchange.
	http.SetCookie(w, &http.Cookie{ // #nosec G124 -- short-lived pairing cookie (SameSite=Lax)
		Name:     "tango_device_login",
		Value:    deviceToken,
		Path:     "/",
		Expires:  created.ExpiresAt,
		HttpOnly: true,
		Secure:   false,
	})
	responder.Success(w, r, http.StatusCreated, created)
}

// setSessionCookie mirrors the session module cookie flags; the
// duplicate is intentional — this module must not import the
// session package (the registry wires the cookie name).
func setSessionCookie(w http.ResponseWriter, name, value string, secure bool) {
	http.SetCookie(w, &http.Cookie{ // #nosec G124 -- session cookie parity (SameSite=Lax, Secure off in dev)
		Name:     name,
		Value:    value,
		Path:     "/",
		MaxAge:   0,
		HttpOnly: true,
		Secure:   secure,
	})
}

// handleExchange serves POST /device-login/requests/{id}/exchange:
// long-poll until approved; sets the session cookie on success.
func (s *Service) handleExchange(w http.ResponseWriter, r *http.Request) {
	deviceToken := deviceTokenFrom(r)
	if deviceToken == "" {
		responder.Fail(w, r, http.StatusUnauthorized, "device token required")
		return
	}

	u, token, err := s.Exchange(r.Context(), chi.URLParam(r, "id"), deviceToken)
	switch {
	case err == nil:
	case isPending(err):
		responder.WriteJSON(w, http.StatusAccepted, map[string]any{
			"status":   "pending",
			"interval": PollingInterval,
		})
		return
	case err == ErrDenied:
		responder.Fail(w, r, http.StatusForbidden, "device login was denied")
		return
	default:
		responder.Fail(w, r, http.StatusNotFound, "device login request is invalid or expired: "+err.Error())
		return
	}

	setSessionCookie(w, s.cookieName, token, s.cookieSecure)
	responder.Success(w, r, http.StatusOK, u)
}

// handleInspect serves POST /device-login/verification: what the
// approving device will see for the code.
func (s *Service) handleInspect(w http.ResponseWriter, r *http.Request) {
	var req inspectRequest
	if verr := validate.Request(r.Body, &req); verr != nil {
		responder.Fail(w, r, http.StatusUnprocessableEntity, "validation failed",
			responder.WithError(validate.FieldErrors(verr)))
		return
	}

	info, err := s.Inspect(r.Context(), req.Code)
	if err != nil {
		responder.Fail(w, r, http.StatusNotFound, "device login request is invalid or expired: "+err.Error())
		return
	}
	responder.Success(w, r, http.StatusOK, info)
}

// inspectRequest is the POST verification payload.
type inspectRequest struct {
	Code string `json:"code"`
}

func (r inspectRequest) Validate() error {
	if normalizeCode(r.Code) == "" {
		return validation.Errors{"code": validation.NewError("validation", "cannot be blank")}
	}
	return nil
}

// decisionRequest is the POST decision payload.
type decisionRequest struct {
	Code     string `json:"code"`
	Decision string `json:"decision"`
}

func (r decisionRequest) Validate() error {
	if normalizeCode(r.Code) == "" {
		return validation.Errors{"code": validation.NewError("validation", "cannot be blank")}
	}
	if r.Decision != "approve" && r.Decision != "deny" {
		return validation.Errors{"decision": validation.NewError("validation", "must be approve or deny")}
	}
	return nil
}

// handleDecision serves POST /device-login/verification/decision.
func (s *Service) handleDecision(w http.ResponseWriter, r *http.Request) {
	principal, ok := middleware.PrincipalFromContext(r.Context())
	if !ok {
		responder.Fail(w, r, http.StatusUnauthorized, "authentication required")
		return
	}

	var req decisionRequest
	if verr := validate.Request(r.Body, &req); verr != nil {
		responder.Fail(w, r, http.StatusUnprocessableEntity, "validation failed",
			responder.WithError(validate.FieldErrors(verr)))
		return
	}

	if err := s.Decide(r.Context(), req.Code, req.Decision, principal); err != nil {
		responder.Fail(w, r, http.StatusNotFound, "device login request is invalid or expired")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// deviceTokenFrom reads the pairing token: cookie first, then the
// dedicated header.
func deviceTokenFrom(r *http.Request) string {
	if cookie, err := r.Cookie("tango_device_login"); err == nil && cookie.Value != "" {
		return cookie.Value
	}
	return r.Header.Get("X-Device-Token")
}

// baseURLFrom derives the public base URL for verification URIs.
func baseURLFrom(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// requestIP prefers the proxy-injected address.
func requestIP(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		return forwarded
	}
	return r.RemoteAddr
}

// isPending reports the typed wait signal (deadline reached).
func isPending(err error) bool {
	return err == ErrPending
}
