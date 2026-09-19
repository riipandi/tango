package oidc

// handler_surfaces.go hosts token introspection, client listings,
// client secret management, and the end-session surface.

import (
	"net/http"

	"github.com/riipandi/tango/pkg/responder"
)

// handleIntrospect serves POST /api/oidc/introspect (RFC 7662):
// client-authenticated token check; inactive on any failure.
func (s *Service) handleIntrospect(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		tokenError(w, r, "invalid_request", http.StatusBadRequest)
		return
	}

	client, ok := s.authenticatedClient(w, r)
	if !ok {
		return
	}

	introspected, _, err := s.introspectAccessToken(r.Context(), r.PostFormValue("token"))
	inactive := err != nil
	if !inactive && introspected.ClientID != client.ID.String() {
		// Tokens minted for other clients introspect as inactive.
		inactive = true
	}
	if inactive {
		responder.WriteJSON(w, http.StatusOK, map[string]any{"active": false})
		return
	}

	responder.WriteJSON(w, http.StatusOK, map[string]any{
		"active":     true,
		"scope":      introspected.Scope,
		"client_id":  introspected.ClientID,
		"sub":        introspected.Subject,
		"sid":        introspected.SID,
		"iss":        s.issuer,
		"token_type": "Bearer",
	})
}

// handleEndSession serves GET/POST /api/oidc/end-session: verifies
// the ID-token hint, revokes the grant's token family, clears the
// sign-in cookie, and redirects to the registered logout callback —
// or the instance logout page when the contract fails.
func (s *Service) handleEndSession(w http.ResponseWriter, r *http.Request) {
	hint := r.FormValue("id_token_hint")
	clientID := r.FormValue("client_id")
	redirectURI := r.FormValue("post_logout_redirect_uri")
	state := r.FormValue("state")

	callback, err := s.EndSession(r.Context(), hint, clientID, redirectURI)

	// Clear the sign-in cookie; Secure mirrors the session module.
	http.SetCookie(w, &http.Cookie{ // #nosec G124 -- session cookie parity (SameSite=Lax, Secure off in dev)
		Name:     s.cookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.cookieSecure,
	})

	if err != nil || callback == "" {
		// Upstream falls back to the logout page instead of
		// reporting why the hint or callback failed.
		http.Redirect(w, r, s.issuer+"/logout", http.StatusFound) // #nosec G710 -- fixed relative target
		return
	}
	http.Redirect(w, r, appendStateToURL(callback, state), http.StatusFound) // #nosec G710 -- registered logout callback
}
