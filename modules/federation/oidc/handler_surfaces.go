package oidc

// handler_surfaces.go hosts token introspection, client listings,
// client secret management, and the end-session stub.

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-ozzo/ozzo-validation/v4"

	"github.com/riipandi/tango/internal/transport/middleware"
	"github.com/riipandi/tango/pkg/responder"
	"github.com/riipandi/tango/pkg/validate"
)

// handleIntrospect serves POST /api/oidc/introspect (RFC 7662):
// client-authenticated token check; inactive on any failure.
func (s *Service) handleIntrospect(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		tokenError(w, r, "invalid_request", http.StatusBadRequest)
		return
	}

	client, ok := s.authenticateClient(w, r)
	if !ok {
		return
	}

	introspected, _, err := s.introspectAccessToken(r, r.PostFormValue("token"))
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

// handleEndSession serves GET/POST /api/oidc/end-session: clears
// the sign-in cookie and redirects to the client's logout callback
// when one is registered.
func (s *Service) handleEndSession(w http.ResponseWriter, r *http.Request) {
	// Clear the sign-in cookie; Secure mirrors the session module.
	http.SetCookie(w, &http.Cookie{ // #nosec G124 -- session cookie parity (SameSite=Lax, Secure off in dev)
		Name:     s.cookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.cookieSecure,
	})

	if clientID := r.URL.Query().Get("client_id"); clientID != "" {
		if id, err := OIDCParseClientID(clientID); err == nil {
			if client, getErr := s.store.GetClient(r.Context(), id); getErr == nil && len(client.LogoutCallbackURLs) > 0 {
				target := client.LogoutCallbackURLs[0]
				if state := r.URL.Query().Get("state"); state != "" {
					sep := "?"
					if strings.Contains(target, "?") {
						sep = "&"
					}
					target += sep + "state=" + url.QueryEscape(state)
				}
				http.Redirect(w, r, target, http.StatusFound) // #nosec G710 -- registered logout callback
				return
			}
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleAccessibleClients serves GET /api/oidc/users/me/clients:
// clients the current user may reach.
func (s *Service) handleAccessibleClients(w http.ResponseWriter, r *http.Request) {
	principal, ok := middleware.PrincipalFromContext(r.Context())
	if !ok {
		responder.Fail(w, r, http.StatusUnauthorized, "authentication required")
		return
	}

	clients, err := s.store.AccessibleClients(r.Context(), principal.UserID)
	if err != nil {
		responder.Fail(w, r, http.StatusInternalServerError, "internal error")
		return
	}

	views := make([]map[string]any, 0, len(clients))
	for _, client := range clients {
		views = append(views, client.view())
	}
	responder.Success(w, r, http.StatusOK, views)
}

// handleMyAuthorizedClients serves GET
// /api/oidc/users/me/authorized-clients.
func (s *Service) handleMyAuthorizedClients(w http.ResponseWriter, r *http.Request) {
	principal, ok := middleware.PrincipalFromContext(r.Context())
	if !ok {
		responder.Fail(w, r, http.StatusUnauthorized, "authentication required")
		return
	}

	records, err := s.store.AuthorizedClients(r.Context(), &principal.UserID)
	if err != nil {
		responder.Fail(w, r, http.StatusInternalServerError, "internal error")
		return
	}
	responder.Success(w, r, http.StatusOK, records)
}

// handleRevokeMyAuthorization serves DELETE
// /api/oidc/users/me/authorized-clients/{clientId}: drops the
// consent and kills the user's active tokens for that client.
func (s *Service) handleRevokeMyAuthorization(w http.ResponseWriter, r *http.Request) {
	principal, ok := middleware.PrincipalFromContext(r.Context())
	if !ok {
		responder.Fail(w, r, http.StatusUnauthorized, "authentication required")
		return
	}

	clientID := chi.URLParam(r, "clientId")
	if err := s.store.DeleteAuthorization(r.Context(), principal.UserID, clientID); err != nil {
		responder.Fail(w, r, http.StatusInternalServerError, "internal error")
		return
	}
	if err := s.store.RevokeClientTokens(r.Context(), clientID, principal.UserID); err != nil {
		responder.Fail(w, r, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleListUserAuthorizedClients serves GET
// /api/oidc/users/{id}/authorized-clients (admin view).
func (s *Service) handleListUserAuthorizedClients(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "id")
	records, err := s.store.AuthorizedClients(r.Context(), &userID)
	if err != nil {
		responder.Fail(w, r, http.StatusInternalServerError, "internal error")
		return
	}
	responder.Success(w, r, http.StatusOK, records)
}

// handleUpdateAllowedGroups serves PUT
// /api/oidc/clients/{id}/allowed-user-groups.
func (s *Service) handleUpdateAllowedGroups(w http.ResponseWriter, r *http.Request) {
	id, err := OIDCParseClientID(chi.URLParam(r, "clientId"))
	if err != nil {
		responder.Fail(w, r, http.StatusBadRequest, "invalid client id")
		return
	}

	var req allowedGroupsRequest
	if verr := validate.Request(r.Body, &req); verr != nil {
		responder.Fail(w, r, http.StatusUnprocessableEntity, "validation failed",
			responder.WithError(validate.FieldErrors(verr)))
		return
	}

	if setErr := s.store.SetClientGroups(r.Context(), id, req.GroupIDs); setErr != nil {
		responder.Fail(w, r, http.StatusInternalServerError, "internal error")
		return
	}

	client, err := s.store.GetClient(r.Context(), id)
	if err != nil {
		responder.NotFoundJSON(w, r)
		return
	}
	responder.Success(w, r, http.StatusOK, client.view())
}

// allowedGroupsRequest is the PUT allowed-user-groups payload.
type allowedGroupsRequest struct {
	GroupIDs []string `json:"user_group_ids"`
}

func (r allowedGroupsRequest) Validate() error {
	return nil
}

// createSecretRequest is the POST /clients/{id}/secrets payload.
type createSecretRequest struct {
	Secret    string     `json:"secret,omitzero"`
	ExpiresAt *time.Time `json:"expires_at,omitzero"`
}

func (r createSecretRequest) Validate() error {
	if r.Secret != "" && len(r.Secret) < 16 {
		return validation.Errors{"secret": validation.NewError("validation", "must be at least 16 characters")}
	}
	return nil
}

// handleListSecrets serves GET /api/oidc/clients/{id}/secrets:
// metadata only (never the values).
func (s *Service) handleListSecrets(w http.ResponseWriter, r *http.Request) {
	id, err := OIDCParseClientID(chi.URLParam(r, "clientId"))
	if err != nil {
		responder.Fail(w, r, http.StatusBadRequest, "invalid client id")
		return
	}

	client, err := s.store.GetClient(r.Context(), id)
	if err != nil {
		responder.NotFoundJSON(w, r)
		return
	}

	views := make([]SecretEntryView, 0, len(client.Secrets))
	for _, secret := range client.Secrets {
		views = append(views, SecretEntryView{
			ID:        secret.ID,
			CreatedAt: secret.CreatedAt,
			ExpiresAt: secret.ExpiresAt,
			IsActive:  secret.IsActive,
		})
	}
	responder.Success(w, r, http.StatusOK, views)
}

// handleCreateSecret serves POST /api/oidc/clients/{id}/secrets:
// the raw value is returned exactly once.
func (s *Service) handleCreateSecret(w http.ResponseWriter, r *http.Request) {
	id, err := OIDCParseClientID(chi.URLParam(r, "clientId"))
	if err != nil {
		responder.Fail(w, r, http.StatusBadRequest, "invalid client id")
		return
	}

	var req createSecretRequest
	if verr := validate.Request(r.Body, &req); verr != nil {
		responder.Fail(w, r, http.StatusUnprocessableEntity, "validation failed",
			responder.WithError(validate.FieldErrors(verr)))
		return
	}

	created, err := s.addSecret(r.Context(), id, req)
	if err != nil {
		responder.NotFoundJSON(w, r)
		return
	}
	responder.Success(w, r, http.StatusCreated, created)
}

// handleDeleteSecret serves DELETE /api/oidc/clients/{id}/secrets/{secretId}.
func (s *Service) handleDeleteSecret(w http.ResponseWriter, r *http.Request) {
	id, err := OIDCParseClientID(chi.URLParam(r, "clientId"))
	if err != nil {
		responder.Fail(w, r, http.StatusBadRequest, "invalid client id")
		return
	}
	if err := s.store.DeleteClientSecret(r.Context(), id, chi.URLParam(r, "secretId")); err != nil {
		responder.NotFoundJSON(w, r)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
