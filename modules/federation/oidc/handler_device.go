package oidc

// handler_device.go is the HTTP surface of the device grant: the
// device-facing authorization endpoint renders bare OAuth errors,
// the browser-facing verify/info endpoints render the envelope.

import (
	"net/http"

	"github.com/riipandi/tango/pkg/responder"
)

// HandleDeviceAuthorize implements POST /api/oidc/device/authorize
// (client-authenticated, form-encoded). Bare OAuth errors: the
// caller is a device, not a browser.
func (s *Service) HandleDeviceAuthorize(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		tokenError(w, r, "invalid_request", http.StatusBadRequest)
		return
	}

	client, ok := s.authenticatedClient(w, r)
	if !ok {
		return // response already written
	}

	response, failure := s.createDeviceAuthorization(r.Context(), client, r.PostFormValue("scope"), r.PostFormValue("resource"), r.PostFormValue("nonce"))
	if failure != nil {
		writeTokenFailure(w, r, failure)
		return
	}
	responder.WriteJSON(w, http.StatusOK, response)
}

// HandleDeviceVerify implements POST /api/oidc/device/verify: the
// browser session approves or denies the pending authorization. 204
// on success; the SPA re-checks /device/info for the consent state.
func (s *Service) HandleDeviceVerify(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		responder.Fail(w, r, http.StatusBadRequest, "invalid request")
		return
	}
	userCode := r.PostFormValue("code")
	if userCode == "" {
		responder.Fail(w, r, http.StatusBadRequest, "missing code")
		return
	}

	principal, authenticated := s.principal(r)
	if !authenticated {
		responder.Fail(w, r, http.StatusUnauthorized, "authentication required")
		return
	}

	if _, failure := s.decideDevice(r.Context(), principal, userCode, r.PostFormValue("action")); failure != nil {
		responder.Fail(w, r, failure.Status, failure.Message)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// HandleDeviceInfo implements GET /api/oidc/device/info: what the
// consent page shows for a user code.
func (s *Service) HandleDeviceInfo(w http.ResponseWriter, r *http.Request) {
	userCode := r.URL.Query().Get("code")
	if userCode == "" {
		responder.Fail(w, r, http.StatusBadRequest, "missing code")
		return
	}

	info, failure := s.deviceInfo(r.Context(), userCode)
	if failure != nil {
		responder.Fail(w, r, failure.Status, failure.Message)
		return
	}
	responder.WriteJSON(w, http.StatusOK, info)
}
