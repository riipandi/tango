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

	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/internal/transport/middleware"
	"github.com/riipandi/tango/pkg/responder"
)

// handleAuthorize is the HTTP shell; see authorizeLogic below.
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
			// Unverified request: never redirect — the redirect_uri has
			// not been validated against the client's callbacks yet.
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

// authorizeParams is the validated /authorize query.
type authorizeParams struct {
	ClientID            OIDCClientID
	RedirectURI         string
	ResponseType        string
	Scope               string
	Resource            string
	State               string
	Nonce               string
	CodeChallenge       string
	CodeChallengeMethod string
	Prompt              string
}

// parseAuthorizeRequest validates the query set. PKCE is mandatory:
// S256 only, plain is rejected (spec-allowed but weak).
func parseAuthorizeRequest(r *http.Request) (*authorizeParams, error) {
	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			return nil, ErrInvalidRequest
		}
		return parseAuthorizeForm(r.PostForm)
	}
	return parseAuthorizeForm(r.URL.Query())
}

// parseAuthorizeForm validates the authorize parameter set from any
// values collection (query or form). PKCE is mandatory: S256 only.
func parseAuthorizeForm(values url.Values) (*authorizeParams, error) {
	p := &authorizeParams{
		RedirectURI:         values.Get("redirect_uri"),
		ResponseType:        values.Get("response_type"),
		Scope:               values.Get("scope"),
		Resource:            values.Get("resource"),
		State:               values.Get("state"),
		Nonce:               values.Get("nonce"),
		CodeChallenge:       values.Get("code_challenge"),
		CodeChallengeMethod: values.Get("code_challenge_method"),
		Prompt:              values.Get("prompt"),
	}

	clientID, err := OIDCParseClientID(values.Get("client_id"))
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

// loadPARParams resolves a pushed request_uri into the authorize
// parameters; an unknown or expired push is invalid.
func (s *Service) loadPARParams(ctx context.Context, requestURI string) (*authorizeParams, error) {
	sum := sha256Hex(requestURI)
	session, err := s.store.GetSession(ctx, KindPAR, sum)
	if err != nil || !session.Active {
		return nil, ErrInvalidRequest
	}
	// One-time use: the push dies with the authorize resume.
	if deactivateErr := s.store.DeactivateSession(ctx, KindPAR, sum); deactivateErr != nil {
		return nil, ErrInvalidRequest
	}

	str := func(key string) string {
		value, _ := session.RequestData[key].(string)
		return value
	}
	clientID, err := OIDCParseClientID(str("client_id"))
	if err != nil {
		return nil, ErrInvalidRequest
	}
	return &authorizeParams{
		ClientID:            clientID,
		RedirectURI:         str("redirect_uri"),
		ResponseType:        "code",
		Scope:               str("scope"),
		Resource:            str("resource"),
		State:               str("state"),
		Nonce:               str("nonce"),
		CodeChallenge:       str("code_challenge"),
		CodeChallengeMethod: str("code_challenge_met"),
		Prompt:              str("prompt"),
	}, nil
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
		UserID:        datastore.UserUUID(principal.UserID),
		ClientID:      client.ID.String(),
		ExpiresAt:     time.Now().UTC().Add(AuthorizationCodeTTL),
	}
	if err := s.store.InsertCode(ctx, code); err != nil {
		return "", err
	}

	// Park the authorize context for the token exchange: redirect
	// check + sid continuity live here, not on the code row.
	requestData := map[string]any{
		"client_id":    client.ID.String(),
		"subject":      principal.UserID,
		"redirect_uri": params.RedirectURI,
		"scope":        scope,
		"sid":          principal.SessionID,
	}
	if params.Resource != "" {
		requestData["audience"] = params.Resource
	}
	if err := s.store.PutSession(ctx, OAuth2Session{
		Kind:        KindAuthorizeCode,
		Key:         sum,
		RequestID:   NewID().String(),
		Active:      true,
		RequestData: requestData,
		ClientID:    client.ID.String(),
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
