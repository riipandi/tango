package oidc

// handler_authorize.go is the HTTP surface of the authorization
// endpoint and the interaction resume: it renders redirects and
// maps use-case failures onto responses. Unverified requests never
// redirect — the redirect_uri is not validated yet.

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/riipandi/tango/internal/transport/middleware"
	"github.com/riipandi/tango/pkg/responder"
)

// handleAuthorize is the HTTP shell of /authorize.
func (s *Service) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	// RFC 9126: a pushed request_uri REPLACES the inline parameters —
	// only request_uri (and nothing else) is on the query when PAR
	// is used, so it is resolved before any inline validation.
	var params *authorizeParams
	if par := parRequestURI(r.URL.Query().Get("request_uri")); par != "" {
		loaded, parErr := s.loadPARParams(r.Context(), par)
		if parErr != nil {
			responder.Fail(w, r, http.StatusBadRequest, "invalid authorization request")
			return
		}
		params = loaded
	} else {
		loaded, err := parseAuthorizeRequest(r)
		if err != nil {
			responder.Fail(w, r, http.StatusBadRequest, "invalid authorization request")
			return
		}
		params = loaded
	}

	client, err := s.store.GetClient(r.Context(), params.ClientID)
	if err != nil {
		// Unknown client or unregistered callback: never redirect —
		// the redirect target itself is unverified.
		responder.Fail(w, r, http.StatusBadRequest, "invalid authorization request")
		return
	}

	// redirect_uri must match a registered pattern before anything
	// else is disclosed.
	if !client.MatchesCallback(params.RedirectURI) {
		responder.Fail(w, r, http.StatusBadRequest, "invalid authorization request")
		return
	}
	if params.ResponseType != "code" {
		s.redirectError(w, r, params, ErrInvalidRequest)
		return
	}
	if !scopeList(params.Scope)[ScopeOpenID] {
		s.redirectError(w, r, params, ErrInvalidRequest)
		return
	}

	principal, authenticated := s.principal(r)
	switch {
	case !authenticated:
		s.startInteraction(w, r, client, params, true)
	case client.IsGroupRestricted && !s.userAllowed(r.Context(), client, principal.UserID):
		responder.Fail(w, r, http.StatusForbidden, "user is not allowed to use this client")
	case client.SkipConsent:
		s.issueCodeRedirect(w, r, client, params, principal)
	default:
		s.startInteraction(w, r, client, params, false)
	}
}

// parseAuthorizeRequest extracts the query set from the GET query
// or the POST form.
func parseAuthorizeRequest(r *http.Request) (*authorizeParams, error) {
	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			return nil, ErrInvalidRequest
		}
		return parseAuthorizeForm(r.PostForm)
	}
	return parseAuthorizeForm(r.URL.Query())
}

// startInteraction parks the authorize request and redirects the
// browser to the interaction resume.
func (s *Service) startInteraction(w http.ResponseWriter, r *http.Request, client Client, params *authorizeParams, authenticationRequired bool) {
	userID := ""
	if !authenticationRequired {
		if principal, ok := middleware.PrincipalFromContext(r.Context()); ok {
			userID = principal.UserID
		}
	}

	interaction, err := s.createInteraction(r.Context(), client, params, authenticationRequired, userID)
	if err != nil {
		responder.Fail(w, r, http.StatusInternalServerError, "failed to start interaction")
		return
	}

	http.Redirect(w, r, s.issuer+InteractionPath+"/"+interaction.ID.String(), http.StatusFound)
}

// redirectToCallback sends the browser to a validated callback with
// the appended query parameters. The target is rebuilt from its parts
// instead of echoing the raw redirect_uri.
func redirectToCallback(w http.ResponseWriter, r *http.Request, rawURL, code, errorCode, state string) {
	scheme, host, path, rawQuery := callbackQuery(rawURL, code, errorCode, state)
	target := url.URL{Scheme: scheme, Host: host, Path: path, RawQuery: rawQuery}
	http.Redirect(w, r, target.String(), http.StatusFound)
}

// issueCodeRedirect resolves the RFC 8707 resource (if any), mints
// the one-time code with the granted scope subset, and redirects
// back to the relying party with code + state.
func (s *Service) issueCodeRedirect(w http.ResponseWriter, r *http.Request, client Client, params *authorizeParams, principal middleware.Principal) {
	audience, granted, errName := s.resolveResource(r.Context(), client.ID.String(), params.Resource, params.Scope, SubjectUser)
	if errName != "" {
		s.redirectError(w, r, params, errName)
		return
	}
	params.Resource = audience
	params.Scope = strings.Join(granted, " ")

	code, err := s.issueCode(r.Context(), client, principal, params.Scope, params)
	if err != nil {
		s.redirectError(w, r, params, ErrInvalidRequest)
		return
	}

	redirectToCallback(w, r, params.RedirectURI, code, "", params.State)
}

// redirectError sends the RFC 6749 §4.1.2.1 error response. Only
// callable with a verified redirect_uri (client + MatchesCallback
// already checked), which is what keeps this off the open-redirect
// path.
func (s *Service) redirectError(w http.ResponseWriter, r *http.Request, params *authorizeParams, cause any) {
	errorCode := "invalid_request"
	switch c := cause.(type) {
	case error:
		if c == ErrAccessDenied {
			errorCode = "access_denied"
		}
	case string:
		errorCode = c
	}

	redirectToCallback(w, r, params.RedirectURI, "", errorCode, params.State)
}

// principal resolves the session cookie (optional auth): /authorize
// is callable both signed-in and anonymous. The context principal
// (middleware chain) wins; the cookie is the fallback.
func (s *Service) principal(r *http.Request) (middleware.Principal, bool) {
	if principal, ok := middleware.PrincipalFromContext(r.Context()); ok {
		return principal, true
	}
	if s.authenticator == nil {
		return middleware.Principal{}, false
	}

	cookie, err := r.Cookie(s.cookieName)
	if err != nil || cookie.Value == "" {
		return middleware.Principal{}, false
	}
	principal, err := s.authenticator.ResolveSession(r.Context(), cookie.Value)
	if err != nil {
		return middleware.Principal{}, false
	}
	return principal, true
}

// interactionError maps a use-case failure onto its response.
func (s *Service) interactionError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrInvalidInteraction):
		responder.Fail(w, r, http.StatusBadRequest, "invalid interaction id")
	case errors.Is(err, ErrNotFound):
		responder.Fail(w, r, http.StatusNotFound, "interaction not found")
	case errors.Is(err, ErrInteractionExpired):
		responder.Fail(w, r, http.StatusNotFound, "interaction expired")
	default:
		responder.Fail(w, r, http.StatusInternalServerError, "internal error")
	}
}
