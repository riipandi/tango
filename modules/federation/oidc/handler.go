package oidc

// handler.go owns every HTTP surface of the provider: the
// root-router /authorize endpoint, the /api/oidc token, userinfo,
// client CRUD, authorized-clients, and interaction endpoints —
// mirroring the user module split (handlers here, logic in the
// area files).

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-ozzo/ozzo-validation/v4"
	"github.com/go-ozzo/ozzo-validation/v4/is"

	"github.com/riipandi/tango/internal/transport/middleware"
	"github.com/riipandi/tango/pkg/responder"
	"github.com/riipandi/tango/pkg/validate"
)

// Routes mounts the root-router endpoints (/authorize) — implements
// federation.RootRoutableFeature.
func (f Feature) Routes(r chi.Router) {
	r.Get(AuthorizePath, f.service.handleAuthorize)
	r.Post(AuthorizePath, f.service.handleAuthorize)
}

// APIRoutes mounts /api/oidc endpoints — implements
// federation.APIFeature. Paths are RELATIVE to the /api group (the
// discovery document advertises the absolute /api/oidc/* URLs).
// Client management mounts only when an admin guard is wired
// (WithAdminGuard) — fail closed.
func (f Feature) APIRoutes(r chi.Router) {
	r.Post(tokenAPIPath, f.service.handleToken)
	r.Post(userinfoAPIPath, f.service.handleUserInfo)
	r.Get(userinfoAPIPath, f.service.handleUserInfo)

	if f.adminGuard != nil {
		clients := r.With(f.adminGuard)
		clients.Route(clientsAPIPrefix, func(cr chi.Router) {
			cr.Get("/", f.service.handleListClients)
			cr.Post("/", f.service.handleCreateClient)
			cr.Get("/{clientId}", f.service.handleGetClient)
			cr.Put("/{clientId}", f.service.handleUpdateClient)
			cr.Delete("/{clientId}", f.service.handleDeleteClient)
			cr.Put("/{clientId}/allowed-user-groups", f.service.handleUpdateAllowedGroups)
			cr.Get("/{clientId}/meta", f.service.handleClientMeta)
			cr.Get("/{clientId}/preview/{userId}", f.service.handleClientPreview)
			cr.Get("/{clientId}/secrets", f.service.handleListSecrets)
			cr.Post("/{clientId}/secrets", f.service.handleCreateSecret)
			cr.Delete("/{clientId}/secrets/{secretId}", f.service.handleDeleteSecret)
		})
		clients.Get(authorizedClientsAPIPrefix, f.service.handleListAuthorizedClients)
		clients.Get("/oidc/users/{id}/authorized-clients", f.service.handleListUserAuthorizedClients)
	}

	if f.selfAuth != nil {
		self := r.With(middleware.RequireAuth(f.selfAuth, f.cookieName))
		self.Get("/oidc/users/me/clients", f.service.handleAccessibleClients)
		self.Get("/oidc/users/me/authorized-clients", f.service.handleMyAuthorizedClients)
		self.Delete("/oidc/users/me/authorized-clients/{clientId}", f.service.handleRevokeMyAuthorization)
	}

	r.Route(interactionAPIPrefix, func(ir chi.Router) {
		ir.Get("/{id}", f.service.handleGetInteraction)
		ir.Post("/{id}/approve", f.service.handleApproveInteraction)
	})

	r.Post(introspectAPIPath, f.service.handleIntrospect)
	r.Post(endSessionAPIPath, f.service.handleEndSession)
	r.Get(endSessionAPIPath, f.service.handleEndSession)
}

// API mount prefixes (relative to the /api group).
const (
	clientsAPIPrefix           = "/oidc/clients"
	authorizedClientsAPIPrefix = "/oidc/authorized-clients"
	interactionAPIPrefix       = "/oidc/interaction"
	introspectAPIPath          = "/oidc/introspect"
	endSessionAPIPath          = "/oidc/end-session"

	tokenAPIPath    = "/oidc/token"
	userinfoAPIPath = "/oidc/userinfo"
)

// ----------------------------------------------------------------------------
// Client CRUD (admin)
// ----------------------------------------------------------------------------

// clientRequest is the POST/PUT /api/oidc/clients payload.
type clientRequest struct {
	Name                        string   `json:"name"`
	Description                 string   `json:"description"`
	Secret                      *string  `json:"secret,omitzero"`
	CallbackURLs                []string `json:"callback_urls"`
	LogoutCallbackURLs          []string `json:"logout_callback_urls"`
	LaunchURL                   string   `json:"launch_url"`
	IsPublic                    bool     `json:"is_public"`
	PKCEEnabled                 bool     `json:"pkce_enabled"`
	PKCESupported               bool     `json:"pkce_supported"`
	SkipConsent                 bool     `json:"skip_consent"`
	IsGroupRestricted           bool     `json:"is_group_restricted"`
	AccessTokenDurationMinutes  int64    `json:"access_token_duration_minutes"`
	RefreshTokenDurationMinutes int64    `json:"refresh_token_duration_minutes"`
	AllowedGroupIDs             []string `json:"allowed_user_group_ids"`
}

func (r clientRequest) Validate() error {
	return validation.ValidateStruct(&r,
		validation.Field(&r.Name, validation.Required),
		validation.Field(&r.CallbackURLs, validation.Required, validation.Each(is.URL)),
	)
}

// handleListClients serves GET /api/oidc/clients.
func (s *Service) handleListClients(w http.ResponseWriter, r *http.Request) {
	clients, err := s.store.ListClients(r.Context())
	if err != nil {
		responder.Fail(w, r, http.StatusInternalServerError, "failed to list clients")
		return
	}

	views := make([]map[string]any, 0, len(clients))
	for _, client := range clients {
		views = append(views, client.view())
	}
	responder.Success(w, r, http.StatusOK, views)
}

// handleGetClient serves GET /api/oidc/clients/{clientId}.
func (s *Service) handleGetClient(w http.ResponseWriter, r *http.Request) {
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
	responder.Success(w, r, http.StatusOK, client.view())
}

// handleCreateClient serves POST /api/oidc/clients; the raw secret
// is returned exactly once.
func (s *Service) handleCreateClient(w http.ResponseWriter, r *http.Request) {
	var request clientRequest
	if verr := validate.Request(r.Body, &request); verr != nil {
		responder.Fail(w, r, http.StatusUnprocessableEntity, "validation failed",
			responder.WithError(validate.FieldErrors(verr)))
		return
	}

	created, rawSecret, err := s.createClient(r.Context(), request)
	if err != nil {
		responder.Fail(w, r, http.StatusInternalServerError, "failed to create client")
		return
	}

	view := created.view()
	view["client_secret"] = rawSecret
	responder.Success(w, r, http.StatusCreated, view)
}

// handleUpdateClient serves PUT /api/oidc/clients/{clientId}.
func (s *Service) handleUpdateClient(w http.ResponseWriter, r *http.Request) {
	id, err := OIDCParseClientID(chi.URLParam(r, "clientId"))
	if err != nil {
		responder.Fail(w, r, http.StatusBadRequest, "invalid client id")
		return
	}

	var request clientRequest
	if verr := validate.Request(r.Body, &request); verr != nil {
		responder.Fail(w, r, http.StatusUnprocessableEntity, "validation failed",
			responder.WithError(validate.FieldErrors(verr)))
		return
	}

	client, err := s.updateClient(r.Context(), id, request)
	if err != nil {
		responder.NotFoundJSON(w, r)
		return
	}
	responder.Success(w, r, http.StatusOK, client.view())
}

// handleDeleteClient serves DELETE /api/oidc/clients/{clientId}.
func (s *Service) handleDeleteClient(w http.ResponseWriter, r *http.Request) {
	id, err := OIDCParseClientID(chi.URLParam(r, "clientId"))
	if err != nil {
		responder.Fail(w, r, http.StatusBadRequest, "invalid client id")
		return
	}
	if err := s.deleteClient(r.Context(), id); err != nil {
		responder.NotFoundJSON(w, r)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleListAuthorizedClients serves GET
// /api/oidc/authorized-clients (consent records, all users).
func (s *Service) handleListAuthorizedClients(w http.ResponseWriter, r *http.Request) {
	records, err := s.store.AuthorizedClients(r.Context(), nil)
	if err != nil {
		responder.Fail(w, r, http.StatusInternalServerError, "failed to list authorized clients")
		return
	}
	responder.Success(w, r, http.StatusOK, records)
}

// ----------------------------------------------------------------------------
// Token + userinfo (thin HTTP shells; logic in token.go / userinfo.go)
// ----------------------------------------------------------------------------

// handleToken implements POST /api/oidc/token (form-encoded).
func (s *Service) handleToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		tokenError(w, r, "invalid_request", http.StatusBadRequest)
		return
	}

	client, ok := s.authenticateClient(w, r)
	if !ok {
		return // response already written
	}

	switch grant := r.PostFormValue("grant_type"); grant {
	case "authorization_code":
		s.exchangeCode(w, r, client)
	case "refresh_token":
		s.exchangeRefresh(w, r, client)
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

	introspected, scope, err := s.introspectAccessToken(r, token)
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

// ----------------------------------------------------------------------------
// Interaction bridge (SPA)
// ----------------------------------------------------------------------------

// handleGetInteraction returns the interaction state for the SPA
// (client name, requested scopes, resume parameters).
func (s *Service) handleGetInteraction(w http.ResponseWriter, r *http.Request) {
	session, err := s.loadInteraction(w, r)
	if err != nil {
		return // response already written
	}
	responder.Success(w, r, http.StatusOK, map[string]any{
		"id":                      session.ID.String(),
		"client_id":               session.ClientID,
		"scopes":                  session.Scopes,
		"consent_required":        session.ConsentRequired,
		"authentication_required": session.AuthenticationRequired,
		"parameters":              session.Parameters,
	})
}

// handleApproveInteraction resolves the interaction: requires a
// signed-in session, issues the code, and returns the callback URL
// for the SPA to redirect to.
func (s *Service) handleApproveInteraction(w http.ResponseWriter, r *http.Request) {
	session, err := s.loadInteraction(w, r)
	if err != nil {
		return // response already written
	}

	principal, authenticated := s.principal(r)
	if !authenticated {
		responder.Fail(w, r, http.StatusUnauthorized, "sign-in required")
		return
	}

	client, err := s.store.GetClient(r.Context(), OIDCClientIDFromClientKey(session.ClientID))
	if err != nil {
		responder.Fail(w, r, http.StatusBadRequest, "unknown client")
		return
	}
	if client.IsGroupRestricted && !s.userAllowed(r.Context(), client, principal.UserID) {
		responder.Fail(w, r, http.StatusForbidden, "user is not allowed to use this client")
		return
	}

	params, err := authorizeParamsFromMap(session.Parameters)
	if err != nil {
		responder.Fail(w, r, http.StatusBadRequest, "interaction parameters are no longer valid")
		return
	}

	scope := params.Scope
	if granted := r.URL.Query().Get("scope"); granted != "" {
		scope = granted
	}

	code, err := s.issueCode(r.Context(), client, principal, scope, params)
	if err != nil {
		responder.Fail(w, r, http.StatusInternalServerError, "failed to issue authorization code")
		return
	}

	_ = s.store.DeleteInteraction(r.Context(), session.ID)
	responder.Success(w, r, http.StatusOK, map[string]any{
		"redirect_uri": buildCallback(params.RedirectURI, code, params.State),
	})
}

// view converts a client row to the API payload.
func (c Client) view() map[string]any {
	return map[string]any{
		"id":                             c.ID.String(),
		"name":                           c.Name,
		"description":                    c.Description,
		"has_secret":                     c.SecretHash != nil,
		"callback_urls":                  c.CallbackURLs,
		"logout_callback_urls":           c.LogoutCallbackURLs,
		"launch_url":                     c.LaunchURL,
		"is_public":                      c.IsPublic,
		"pkce_enabled":                   c.PKCEEnabled,
		"pkce_supported":                 c.PKCESupported,
		"skip_consent":                   c.SkipConsent,
		"is_group_restricted":            c.IsGroupRestricted,
		"access_token_duration_minutes":  c.AccessTokenDurationMinutes,
		"refresh_token_duration_minutes": c.RefreshTokenDurationMinutes,
		"allowed_user_group_ids":         c.AllowedGroupIDs,
	}
}
