package oidc

// handler.go owns every HTTP surface of the provider: the
// root-router /authorize endpoint, the /api/oidc token, userinfo,
// client CRUD, authorized-clients, and interaction endpoints —
// mirroring the user module split (handlers here, logic in the
// area files).

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-ozzo/ozzo-validation/v4"
	"github.com/go-ozzo/ozzo-validation/v4/is"

	"github.com/riipandi/tango/modules/federation"
	"github.com/riipandi/tango/pkg/responder"
)

// Routes mounts the root-router endpoints (/authorize) — implements
// federation.RootRoutableFeature.
func (f Feature) Routes(r chi.Router) {
	r.Get(AuthorizePath, f.service.handleAuthorize)
	r.Post(AuthorizePath, f.service.handleAuthorize)
}

// APIRoutes mounts /api/oidc endpoints — implements
// federation.APIFeature. Paths are RELATIVE to the /api group (the
// discovery document advertises the absolute /api/oidc/* URLs). The
// client administration and consent surfaces serve ConnectRPC
// exclusively (see handler_rpc.go); what remains here is the OIDC
// protocol surface, the public logo read, and the interaction flow.
func (f Feature) APIRoutes(r chi.Router, _ federation.RouteGroups) {
	r.Post(tokenAPIPath, f.service.handleToken)
	r.Post(userinfoAPIPath, f.service.handleUserInfo)
	r.Get(userinfoAPIPath, f.service.handleUserInfo)

	// Public logo read: bare bytes, no guard.
	if f.service.images != nil {
		r.Get("/oidc/clients/{clientId}/logo", f.service.serveClientLogo)
	}

	r.Route(interactionAPIPrefix, func(ir chi.Router) {
		ir.Get("/{id}", f.service.handleGetInteraction)
		ir.Post("/{id}/approve", f.service.handleApproveInteraction)
	})

	r.Post(introspectAPIPath, f.service.handleIntrospect)
	r.Post(endSessionAPIPath, f.service.handleEndSession)
	r.Get(endSessionAPIPath, f.service.handleEndSession)

	// RFC 9126 pushed authorization requests.
	r.Post(parAPIPath, f.service.HandlePAR)

	// RFC 8628 device authorization grant.
	r.Post(deviceAPIPrefix+"/authorize", f.service.HandleDeviceAuthorize)
	r.Post(deviceAPIPrefix+"/verify", f.service.HandleDeviceVerify)
	r.Get(deviceAPIPrefix+"/info", f.service.HandleDeviceInfo)
}

// API mount prefixes (relative to the /api group).
const (
	interactionAPIPrefix = "/oidc/interaction"
	introspectAPIPath    = "/oidc/introspect"
	endSessionAPIPath    = "/oidc/end-session"

	tokenAPIPath    = "/oidc/token"
	userinfoAPIPath = "/oidc/userinfo"

	deviceAPIPrefix = "/oidc/device"
	parAPIPath      = "/oidc/par"
)

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
	// MetadataURL opts the client into CIMD-lite (create only): the
	// document at this URL materializes name + redirect URIs and the
	// client becomes public.
	MetadataURL string `json:"metadata_url,omitzero"`
}

func (r clientRequest) Validate() error {
	return r.validateWith(r.MetadataURL != "")
}

// validateWith relaxes the document-owned fields when a CIMD
// document owns them (create-with-url, or updates to a cimd client).
func (r clientRequest) validateWith(metadataOwned bool) error {
	if metadataOwned {
		return nil
	}
	return validation.ValidateStruct(&r,
		validation.Field(&r.Name, validation.Required),
		validation.Field(&r.CallbackURLs, validation.Required, validation.Each(is.URL)),
	)
}

// isMetadataError reports whether the error came from the CIMD fetch/
// validate/policy path (caller-facing message).
func isMetadataError(err error) bool {
	var me metadataError
	return errors.As(err, &me)
}

// handleGetInteraction returns the interaction state for the SPA
// (client name, requested scopes, resume parameters).
func (s *Service) handleGetInteraction(w http.ResponseWriter, r *http.Request) {
	session, err := s.getInteraction(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.interactionError(w, r, err)
		return
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
	session, err := s.getInteraction(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.interactionError(w, r, err)
		return
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
