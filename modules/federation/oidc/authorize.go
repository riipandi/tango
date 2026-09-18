package oidc

// authorize.go holds the authorization-endpoint use cases: request
// parameter validation, the interaction state machine, and one-time
// code issuance. The HTTP shells and redirect rendering live in
// handler_authorize.go.

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/url"
	"strings"
	"time"

	"go.jetify.com/typeid"

	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/internal/transport/middleware"
)

// Interaction lookup outcomes (the HTTP layer maps them onto
// status codes).
var (
	ErrInvalidInteraction = errors.New("oidc: invalid interaction id")
	ErrInteractionExpired = errors.New("oidc: interaction expired")
)

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

// parseAuthorizeForm validates the authorize parameter set from any
// values collection (query or form). PKCE is mandatory: S256 only,
// plain is rejected (spec-allowed but weak).
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

// createInteraction parks an unauthenticated (or consent-pending)
// authorize request; the SPA resumes via the approve endpoint.
// userID is empty for anonymous requests.
func (s *Service) createInteraction(ctx context.Context, client Client, params *authorizeParams, authenticationRequired bool, userID string) (InteractionSession, error) {
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
	if userID != "" {
		interaction.UserID = &userID
	}

	if err := s.store.CreateInteraction(ctx, interaction); err != nil {
		return InteractionSession{}, err
	}
	return interaction, nil
}

// getInteraction resolves an interaction by its raw path segment,
// enforcing the interaction TTL. Expired interactions are deleted
// and answer ErrInteractionExpired; unknown ids answer ErrNotFound.
func (s *Service) getInteraction(ctx context.Context, raw string) (InteractionSession, error) {
	id, err := InteractionIDFromString(raw)
	if err != nil {
		return InteractionSession{}, ErrInvalidInteraction
	}

	session, err := s.store.GetInteraction(ctx, id)
	if err != nil {
		return InteractionSession{}, ErrNotFound
	}
	if time.Since(session.RequestedAt) > InteractionSessionTTL {
		_ = s.store.DeleteInteraction(ctx, session.ID)
		return InteractionSession{}, ErrInteractionExpired
	}
	return session, nil
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
