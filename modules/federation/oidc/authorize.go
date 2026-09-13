package oidc

// authorize.go holds the authorization-endpoint logic: request
// parsing/validation, the interaction state machine, and one-time
// code issuance. HTTP shells live in handler.go.

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"go.jetify.com/typeid"

	"github.com/riipandi/tango/internal/transport/middleware"
	"github.com/riipandi/tango/pkg/responder"
)

// handleAuthorize is the HTTP shell; see authorizeLogic below.
func (s *Service) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	params, err := parseAuthorizeRequest(r)
	if err != nil {
		// Unverified request: never redirect — the redirect_uri has
		// not been validated against the client's callbacks yet.
		responder.Fail(w, r, http.StatusBadRequest, "invalid authorization request")
		return
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

// authorizeParams is the validated /authorize query.
type authorizeParams struct {
	ClientID            OIDCClientID
	RedirectURI         string
	ResponseType        string
	Scope               string
	State               string
	Nonce               string
	CodeChallenge       string
	CodeChallengeMethod string
	Prompt              string
}

// parseAuthorizeRequest validates the query set. PKCE is mandatory:
// S256 only, plain is rejected (spec-allowed but weak).
func parseAuthorizeRequest(r *http.Request) (*authorizeParams, error) {
	query := r.URL.Query()

	p := &authorizeParams{
		RedirectURI:         query.Get("redirect_uri"),
		ResponseType:        query.Get("response_type"),
		Scope:               query.Get("scope"),
		State:               query.Get("state"),
		Nonce:               query.Get("nonce"),
		CodeChallenge:       query.Get("code_challenge"),
		CodeChallengeMethod: query.Get("code_challenge_method"),
		Prompt:              query.Get("prompt"),
	}

	clientID, err := OIDCParseClientID(query.Get("client_id"))
	if err != nil {
		return p, ErrInvalidRequest
	}
	p.ClientID = clientID

	if p.RedirectURI == "" || p.ResponseType == "" || !strings.Contains(p.Scope, ScopeOpenID) {
		return p, ErrInvalidRequest
	}
	if p.CodeChallenge == "" || p.CodeChallengeMethod != "S256" {
		return p, ErrInvalidRequest
	}
	return p, nil
}

// OIDCParseClientID parses the clients.id TEXT key into the typed
// form.
func OIDCParseClientID(raw string) (OIDCClientID, error) {
	return typeid.Parse[OIDCClientID](raw)
}

// OIDCClientIDFromClientKey parses a stored client key back into
// the typed form.
func OIDCClientIDFromClientKey(raw string) OIDCClientID {
	id, err := typeid.Parse[OIDCClientID](raw)
	if err != nil {
		panic("oidc: stored client id is not a typeid: " + raw)
	}
	return id
}

// startInteraction parks an unauthenticated (or consent-pending)
// authorize request; the SPA resumes via the approve endpoint.
func (s *Service) startInteraction(w http.ResponseWriter, r *http.Request, client Client, params *authorizeParams, authenticationRequired bool) {
	interaction := InteractionSession{
		ID:       NewInteractionID(),
		ClientID: client.ID.String(),
		Scopes:   strings.Fields(params.Scope),
		Parameters: map[string]any{
			"client_id":             client.ID.String(),
			"redirect_uri":          params.RedirectURI,
			"response_type":         params.ResponseType,
			"scope":                 params.Scope,
			"state":                 params.State,
			"nonce":                 params.Nonce,
			"code_challenge":        params.CodeChallenge,
			"code_challenge_method": params.CodeChallengeMethod,
			"prompt":                params.Prompt,
		},
		ConsentRequired:        !client.SkipConsent,
		AuthenticationRequired: authenticationRequired,
		RequestedAt:            time.Now().UTC(),
	}
	if !authenticationRequired {
		if principal, ok := middleware.PrincipalFromContext(r.Context()); ok {
			interaction.UserID = &principal.UserID
		}
	}

	if err := s.store.CreateInteraction(r.Context(), interaction); err != nil {
		responder.Fail(w, r, http.StatusInternalServerError, "failed to start interaction")
		return
	}

	http.Redirect(w, r, s.issuer+InteractionPath+"/"+interaction.ID.String(), http.StatusFound)
}

// loadInteraction resolves the interaction path segment, enforcing
// the interaction TTL. HTTP-facing: writes the error response.
func (s *Service) loadInteraction(w http.ResponseWriter, r *http.Request) (InteractionSession, error) {
	id, err := InteractionIDFromString(chi.URLParam(r, "id"))
	if err != nil {
		responder.Fail(w, r, http.StatusBadRequest, "invalid interaction id")
		return InteractionSession{}, err
	}

	session, err := s.store.GetInteraction(r.Context(), id)
	if err != nil {
		responder.Fail(w, r, http.StatusNotFound, "interaction not found")
		return InteractionSession{}, err
	}
	if time.Since(session.RequestedAt) > InteractionSessionTTL {
		_ = s.store.DeleteInteraction(r.Context(), session.ID)
		responder.Fail(w, r, http.StatusNotFound, "interaction expired")
		return InteractionSession{}, ErrNotFound
	}
	return session, nil
}

// issueCodeRedirect mints the one-time code and redirects back to
// the relying party with code + state.
func (s *Service) issueCodeRedirect(w http.ResponseWriter, r *http.Request, client Client, params *authorizeParams, principal middleware.Principal) {
	code, err := s.issueCode(r.Context(), client, principal, params.Scope, params)
	if err != nil {
		s.redirectError(w, r, params, ErrInvalidRequest)
		return
	}

	http.Redirect(w, r, buildCallback(params.RedirectURI, code, params.State), http.StatusFound) // #nosec G710 -- redirect_uri verified against the client's registered patterns
}

// issueCode persists a fresh one-time code (hash at rest) plus its
// authorize context, and returns the raw value.
func (s *Service) issueCode(ctx context.Context, client Client, principal middleware.Principal, scope string, params *authorizeParams) (string, error) {
	raw, err := randomToken()
	if err != nil {
		return "", err
	}

	sum := sha256Hex(raw)
	code := AuthorizationCode{
		CodeHash:      sum,
		Scope:         scope,
		Nonce:         params.Nonce,
		CodeChallenge: params.CodeChallenge,
		MethodSHA256:  params.CodeChallengeMethod == "S256",
		AuthMethod:    "password",
		UserID:        userUUID(principal.UserID),
		ClientID:      client.ID.String(),
		ExpiresAt:     time.Now().UTC().Add(AuthorizationCodeTTL),
	}
	if err := s.store.InsertCode(ctx, code); err != nil {
		return "", err
	}

	// Park the authorize context for the token exchange: redirect
	// check + sid continuity live here, not on the code row.
	if err := s.store.PutSession(ctx, OAuth2Session{
		Kind:      KindAuthorizeCode,
		Key:       sum,
		RequestID: NewID().String(),
		Active:    true,
		RequestData: map[string]any{
			"client_id":    client.ID.String(),
			"subject":      principal.UserID,
			"redirect_uri": params.RedirectURI,
			"scope":        scope,
			"sid":          principal.SessionID,
		},
		ClientID: client.ID.String(),
	}); err != nil {
		return "", err
	}

	s.record(ctx, "oidc_code_issued", map[string]any{
		"client_id": client.ID.String(),
		"user_id":   principal.UserID,
	})
	return raw, nil
}

// authorizeParamsFromMap rebuilds validated params from an
// interaction row.
func authorizeParamsFromMap(values map[string]any) (*authorizeParams, error) {
	str := func(key string) string {
		value, _ := values[key].(string)
		return value
	}

	params := &authorizeParams{
		RedirectURI:         str("redirect_uri"),
		ResponseType:        str("response_type"),
		Scope:               str("scope"),
		State:               str("state"),
		Nonce:               str("nonce"),
		CodeChallenge:       str("code_challenge"),
		CodeChallengeMethod: str("code_challenge_method"),
		Prompt:              str("prompt"),
	}
	if params.RedirectURI == "" || params.CodeChallenge == "" {
		return nil, ErrInvalidRequest
	}
	return params, nil
}

// buildCallback appends code + state to the redirect URI.
func buildCallback(redirectURI, code, state string) string {
	callback, err := url.Parse(redirectURI)
	if err != nil {
		return redirectURI
	}
	query := callback.Query()
	query.Set("code", code)
	if state != "" {
		query.Set("state", state)
	}
	callback.RawQuery = query.Encode()
	return callback.String()
}

// redirectError sends the RFC 6749 §4.1.2.1 error response. Only
// callable with a verified redirect_uri (client + MatchesCallback
// already checked), which is what keeps this off the open-redirect
// path.
func (s *Service) redirectError(w http.ResponseWriter, r *http.Request, params *authorizeParams, cause error) {
	errorCode := "invalid_request"
	if cause == ErrAccessDenied {
		errorCode = "access_denied"
	}

	callback, err := url.Parse(params.RedirectURI)
	if err != nil {
		responder.Fail(w, r, http.StatusBadRequest, "invalid authorization request")
		return
	}
	query := callback.Query()
	query.Set("error", errorCode)
	if params.State != "" {
		query.Set("state", params.State)
	}
	callback.RawQuery = query.Encode()
	http.Redirect(w, r, callback.String(), http.StatusFound) // #nosec G710 -- redirect_uri verified against the client's registered patterns
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

// userAllowed checks the group restriction: at least one of the
// user's groups must be on the client allowlist.
func (s *Service) userAllowed(ctx context.Context, client Client, userID string) bool {
	for _, allowed := range client.AllowedGroupIDs {
		if s.store.UserInGroup(ctx, userID, allowed) {
			return true
		}
	}
	return false
}

// randomToken returns a 256-bit URL-safe random string.
func randomToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// randomHex returns n random bytes as lowercase hex.
func randomHex(n int) string {
	buf := make([]byte, n)
	_, _ = rand.Read(buf) // crypto/rand never fails per contract
	return hex.EncodeToString(buf)
}
