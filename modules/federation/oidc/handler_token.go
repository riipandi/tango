package oidc

// handler_token.go is the HTTP surface of the token, userinfo, and
// client-credential parsing: it renders the bare RFC 6749 §5.2
// error payloads and maps use-case failures onto them.

import (
	"net/http"
	"strings"
	"time"

	"github.com/riipandi/tango/pkg/responder"
)

// tokenError writes the RFC 6749 §5.2 error payload.
func tokenError(w http.ResponseWriter, r *http.Request, code string, status int) {
	responder.WriteJSON(w, status, map[string]any{
		"error":             code,
		"error_description": http.StatusText(status),
	})
}

// writeTokenFailure renders a use-case failure as the bare §5.2
// payload.
func writeTokenFailure(w http.ResponseWriter, r *http.Request, failure *tokenFailure) {
	if failure == nil {
		return
	}
	tokenError(w, r, failure.Code, failure.Status)
}

// presentedCredentials extracts the client credentials from Basic
// auth or the form body.
func presentedCredentials(r *http.Request) (string, string) {
	if id, secret, ok := r.BasicAuth(); ok {
		return id, secret
	}
	return r.PostFormValue("client_id"), r.PostFormValue("client_secret")
}

// authenticatedClient resolves and authenticates the client from
// the presented credentials, rendering the §5.2 failure itself.
func (s *Service) authenticatedClient(w http.ResponseWriter, r *http.Request) (Client, bool) {
	id, secret := presentedCredentials(r)
	client, failure := s.authenticateClient(r.Context(), id, secret)
	if failure != nil {
		writeTokenFailure(w, r, failure)
		return Client{}, false
	}
	return client, true
}

// handleToken implements POST /api/oidc/token (form-encoded).
func (s *Service) handleToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		tokenError(w, r, "invalid_request", http.StatusBadRequest)
		return
	}

	client, ok := s.authenticatedClient(w, r)
	if !ok {
		return // response already written
	}

	switch grant := r.PostFormValue("grant_type"); grant {
	case "authorization_code":
		response, failure := s.exchangeCode(r.Context(), client, r.PostFormValue("code"), r.PostFormValue("code_verifier"), r.PostFormValue("redirect_uri"))
		if failure != nil {
			writeTokenFailure(w, r, failure)
			return
		}
		responder.WriteJSON(w, http.StatusOK, response)
	case "refresh_token":
		response, failure := s.exchangeRefresh(r.Context(), client, r.PostFormValue("refresh_token"))
		if failure != nil {
			writeTokenFailure(w, r, failure)
			return
		}
		responder.WriteJSON(w, http.StatusOK, response)
	case GrantDeviceCode:
		response, failure := s.exchangeDevice(r.Context(), client, r.PostFormValue("device_code"))
		if failure != nil {
			writeTokenFailure(w, r, failure)
			return
		}
		responder.WriteJSON(w, http.StatusOK, response)
	default:
		tokenError(w, r, "unsupported_grant_type", http.StatusBadRequest)
	}
}

// handleUserInfo serves GET/POST /api/oidc/userinfo: introspect the
// bearer token, then assemble the user's scoped claims.
func (s *Service) handleUserInfo(w http.ResponseWriter, r *http.Request) {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || token == "" {
		userInfoError(w, r, http.StatusUnauthorized, "invalid_token", "bearer token required")
		return
	}

	introspected, scope, err := s.introspectAccessToken(r.Context(), token)
	if err != nil {
		userInfoError(w, r, http.StatusUnauthorized, "invalid_token", "token is invalid or expired")
		return
	}

	claims, err := s.claimsFor(r.Context(), introspected.Subject, scope, introspected.SID, time.Now().UTC())
	if err != nil {
		userInfoError(w, r, http.StatusUnauthorized, "invalid_token", "token subject no longer exists")
		return
	}

	responder.WriteJSON(w, http.StatusOK, profileClaimsMap(scope, claims))
}

// userInfoError writes the RFC 6750 §3 WWW-Authenticate error form
// with a bare RFC 6749 error body, matching upstream's fosite
// rendering — no responder envelope on protocol errors.
func userInfoError(w http.ResponseWriter, r *http.Request, status int, code, description string) {
	w.Header().Set("WWW-Authenticate", `Bearer error="`+code+`", error_description="`+description+`"`)
	responder.WriteJSON(w, status, map[string]any{
		"error":             code,
		"error_description": description,
	})
}
