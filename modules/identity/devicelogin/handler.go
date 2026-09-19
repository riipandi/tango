package devicelogin

// handler.go owns the device login HTTP surface: the anonymous
// create/exchange pair (QR device side). Approval serves ConnectRPC
// (handler_rpc.go); the exchange sets the session cookie via the
// shared helper contract.

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/pkg/responder"
)

// Feature is the wireable device login unit.
type Feature struct {
	service *Service
}

// New wires the feature to its service.
func New(service *Service) Feature { return Feature{service: service} }

// Name names the feature for logs.
func (Feature) Name() string { return "devicelogin" }

// APIRoutes mounts the retained device-side endpoints relative to
// the /api group (RFC 8628 integration surface). The approval UI's
// inspect/decision pair serves ConnectRPC exclusively — see
// handler_rpc.go.
func (f Feature) APIRoutes(r chi.Router, _ identity.RouteGroups) {
	r.Post("/device-login/requests", f.service.handleCreate)
	r.Post("/device-login/requests/{id}/exchange", f.service.handleExchange)
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
		Secure:   s.cookieSecure,
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
		responder.Fail(w, r, http.StatusNotFound, "device login request is invalid or expired")
		return
	}

	setSessionCookie(w, s.cookieName, token, s.cookieSecure)
	responder.Success(w, r, http.StatusOK, u)
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
