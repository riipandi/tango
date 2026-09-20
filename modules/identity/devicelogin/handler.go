package devicelogin

// handler.go owns the device login HTTP surface: the anonymous
// create/exchange pair (QR device side). Approval serves ConnectRPC
// (handler_rpc.go); the exchange returns the session token in the
// body, and the polling device holds its own token.

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

	// The polling device keeps the token secret and presents it on
	// exchange through the dedicated header.
	responder.Success(w, r, http.StatusCreated, map[string]any{
		"request":      created,
		"device_token": deviceToken,
	})
}

// handleExchange serves POST /device-login/requests/{id}/exchange:
// long-poll until approved; returns the session token in the body on
// success.
func (s *Service) handleExchange(w http.ResponseWriter, r *http.Request) {
	deviceToken := deviceTokenFrom(r)
	if deviceToken == "" {
		responder.Fail(w, r, http.StatusUnauthorized, "device token required")
		return
	}

	requestID := chi.URLParam(r, "id")
	u, token, err := s.Exchange(r.Context(), requestID, deviceToken)
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

	responder.Success(w, r, http.StatusOK, map[string]any{
		"user":          u,
		"session_token": token,
	})
}

// baseURLFrom derives the public base URL for verification URIs.
func baseURLFrom(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// deviceTokenFrom reads the pairing token the polling device
// presents on its dedicated header.
func deviceTokenFrom(r *http.Request) string {
	return r.Header.Get("X-Device-Token")
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
