package oidc

// token.go holds the token-endpoint use cases: client
// authentication against the credentials list, the authorization_code
// exchange (one-time code + PKCE), the refresh_token grant (rotation
// per use with family reuse revocation), and token minting. The HTTP
// shells and the RFC 6749 §5.2 error rendering live in
// handler_token.go.

import (
	"context"
	"crypto/subtle"
	"strings"
	"time"

	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"

	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/pkg/jwtutils"
)

// tokenResponse is the RFC 6749 §5.1 success payload.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	Scope        string `json:"scope"`
}

// tokenFailure is one RFC 6749 §5.2 error: the protocol error code
// plus the HTTP status it renders with. Plain data — the transport
// layer renders it.
type tokenFailure struct {
	Code   string
	Status int
}

func (f *tokenFailure) Error() string { return f.Code }

func invalidClient() *tokenFailure  { return &tokenFailure{Code: "invalid_client", Status: 401} }
func invalidGrant() *tokenFailure   { return &tokenFailure{Code: "invalid_grant", Status: 400} }
func invalidRequest() *tokenFailure { return &tokenFailure{Code: "invalid_request", Status: 400} }
func serverError() *tokenFailure    { return &tokenFailure{Code: "server_error", Status: 500} }

// authenticateClient validates the presented client credentials.
// Public clients (no secret) authenticate by ID alone.
func (s *Service) authenticateClient(ctx context.Context, clientID, clientSecret string) (Client, *tokenFailure) {
	if clientID == "" {
		return Client{}, invalidClient()
	}

	id, err := OIDCParseClientID(clientID)
	if err != nil {
		return Client{}, invalidClient()
	}
	client, err := s.store.GetClient(ctx, id)
	if err != nil {
		return Client{}, invalidClient()
	}

	// Public clients authenticate by identity only; confidential
	// clients must present the secret (constant-time compare of
	// hashes).
	switch {
	case client.IsPublic:
		return client, nil
	case clientSecret == "":
		return Client{}, invalidClient()
	case !secretMatches(client, clientSecret):
		return Client{}, invalidClient()
	}
	return client, nil
}

// secretMatches reports whether the presented secret matches any
// active, unexpired credentials entry.
func secretMatches(client Client, secret string) bool {
	return usableSecret(client, sha256Hex(secret))
}

// usableSecret constant-time compares the digest against every
// active, unexpired credentials entry.
func usableSecret(client Client, sum string) bool {
	for _, entry := range client.Secrets {
		if !entry.IsActive || (entry.ExpiresAt != nil && entry.ExpiresAt.Before(time.Now().UTC())) {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(entry.SecretHash), []byte(sum)) == 1 {
			return true
		}
	}
	return false
}

// exchangeCode performs the authorization_code grant: one-time
// consume, redirect_uri match, PKCE S256 verification, then token
// issuance with session bookkeeping.
func (s *Service) exchangeCode(ctx context.Context, client Client, code, verifier, redirectURI string) (*tokenResponse, *tokenFailure) {
	if code == "" || verifier == "" {
		return nil, invalidRequest()
	}

	sum := sha256Hex(code)
	consumed, err := s.store.ConsumeCode(ctx, sum)
	if err != nil {
		return nil, invalidGrant()
	}

	// The authorize_code session row carries the redirect_uri and
	// login context captured at /authorize time.
	seed, err := s.store.GetSession(ctx, KindAuthorizeCode, sum)
	if err != nil || seed.ClientID != client.ID.String() {
		return nil, invalidGrant()
	}
	seedRedirect, _ := seed.RequestData["redirect_uri"].(string)
	sid, _ := seed.RequestData["sid"].(string)
	audience, _ := seed.RequestData["audience"].(string)
	if seedRedirect != redirectURI || !client.MatchesCallback(seedRedirect) {
		return nil, invalidGrant()
	}
	if !consumed.MethodSHA256 || pkceS256(verifier) != consumed.CodeChallenge {
		return nil, invalidGrant()
	}

	family := seedFamily(seed.RequestID, sid, consumed.AuthMethod, seedCreatedAt(seed))
	response, err := s.mintTokens(ctx, client, consumed.UserID, consumed.Scope, consumed.Nonce, audience, family)
	if err != nil {
		return nil, serverError()
	}

	// The code is spent; the parked authorize_code session must not
	// stay active.
	if err := s.store.DeactivateSession(ctx, KindAuthorizeCode, sum); err != nil {
		return nil, serverError()
	}

	if err := s.store.UpsertAuthorizedClient(ctx, consumed.UserID, client.ID.String(), strings.Fields(consumed.Scope)); err != nil {
		return nil, serverError()
	}
	s.record(ctx, "oidc_token_issued", map[string]any{
		"client_id": client.ID.String(),
		"user_id":   consumed.UserID,
		"grant":     "authorization_code",
	})
	return response, nil
}

// exchangeRefresh performs the refresh_token grant: rotation per
// use with reuse detection — replaying a rotated token revokes the
// whole family.
func (s *Service) exchangeRefresh(ctx context.Context, client Client, raw string) (*tokenResponse, *tokenFailure) {
	if raw == "" {
		return nil, invalidRequest()
	}

	sum := sha256Hex(raw)
	session, err := s.store.GetSession(ctx, KindRefresh, sum)
	if err != nil || session.ClientID != client.ID.String() {
		return nil, &tokenFailure{Code: "invalid_grant", Status: 401}
	}

	// Reuse detection: the family has already been rotated once, or
	// the jti is on the replay registry.
	if !session.Active {
		_ = s.store.DeactivateFamily(ctx, session.RequestID)
		return nil, &tokenFailure{Code: "invalid_grant", Status: 401}
	}
	spent, err := s.store.JTIExists(ctx, sum)
	if err != nil || spent {
		_ = s.store.DeactivateFamily(ctx, session.RequestID)
		return nil, &tokenFailure{Code: "invalid_grant", Status: 401}
	}

	userID, _ := session.RequestData["subject"].(string)
	scope, _ := session.RequestData["scope"].(string)
	sid, _ := session.RequestData["sid"].(string)
	authMethod, _ := session.RequestData["authentication_method"].(string)
	authTime := time.Now().UTC()
	if issued, ok := session.RequestData["auth_time"].(float64); ok {
		authTime = time.Unix(int64(issued), 0).UTC()
	}

	// Rotate: retire the presented token, remember its jti on the
	// replay registry, and mint a fresh pair in the same family.
	if deactivateErr := s.store.DeactivateSession(ctx, KindRefresh, sum); deactivateErr != nil {
		return nil, serverError()
	}
	if recordErr := s.store.RecordJTI(ctx, sum, time.Now().UTC().Add(s.RefreshTokenTTL())); recordErr != nil {
		return nil, serverError()
	}

	family := seedFamily(session.RequestID, sid, authMethod, authTime)
	audience, _ := session.RequestData["audience"].(string)
	response, err := s.mintTokens(ctx, client, userID, scope, "", audience, family)
	if err != nil {
		return nil, serverError()
	}

	s.record(ctx, "oidc_token_refreshed", map[string]any{
		"client_id": client.ID.String(),
		"user_id":   userID,
	})
	return response, nil
}

// seedFamily seeds the family metadata for a token exchange; the
// authorize_code session row carries sid + request id.
func seedFamily(requestID, sid, authMethod string, authTime time.Time) refreshContext {
	return refreshContext{requestID: requestID, sid: sid, authMethod: authMethod, authTime: authTime}
}

// seedCreatedAt recovers the login time from the authorize_code
// session row (falls back to now when absent).
func seedCreatedAt(session OAuth2Session) time.Time {
	if session.CreatedAt != nil {
		return *session.CreatedAt
	}
	return time.Now().UTC()
}

// refreshContext carries family state across a token exchange.
type refreshContext struct {
	requestID  string
	sid        string
	authMethod string
	authTime   time.Time
}

// mintTokens creates the access token, refresh token, and ID token,
// persisting session rows in one family. The access token carries
// the same jti as its oauth2_sessions row.
func (s *Service) mintTokens(ctx context.Context, client Client, userID, scope, nonce, audience string, family refreshContext) (*tokenResponse, error) {
	accessTTL := time.Duration(client.AccessTokenDurationMinutes) * time.Minute
	refreshTTL := time.Duration(client.RefreshTokenDurationMinutes) * time.Minute
	if accessTTL <= 0 {
		accessTTL = s.AccessTokenTTL()
	}
	if refreshTTL <= 0 {
		refreshTTL = s.RefreshTokenTTL()
	}
	// Default audience: the requesting client (plain login token).
	if audience == "" {
		audience = client.ID.String()
	}

	signKey, err := s.keys.SignKey(ctx)
	if err != nil {
		return nil, err
	}
	claims, err := s.claimsFor(ctx, userID, scope, family.sid, family.authTime)
	if err != nil {
		return nil, err
	}

	accessJTI := NewID().String()
	access, err := s.signToken(ctx, signKey, map[string]any{
		"client_id": client.ID.String(),
		"scope":     scope,
		"jti":       accessJTI,
		"sid":       family.sid,
	}, accessTTL, claims.Subject, audience)
	if err != nil {
		return nil, err
	}

	idToken, err := s.signIDToken(ctx, signKey, client, claims, nonce, family.authTime, accessTTL, accessJTI)
	if err != nil {
		return nil, err
	}

	rawRefresh, err := randomToken()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()

	accessRow := OAuth2Session{
		Kind:      KindAccessToken,
		Key:       accessJTI,
		RequestID: family.requestID,
		Active:    true,
		RequestData: map[string]any{
			"client_id": client.ID.String(),
			"subject":   claims.Subject,
			"scope":     scope,
		},
		ClientID:  client.ID.String(),
		ExpiresAt: datastore.Ptr(now.Add(accessTTL)),
	}
	refreshRow := OAuth2Session{
		Kind:      KindRefresh,
		Key:       sha256Hex(rawRefresh),
		RequestID: family.requestID,
		Active:    true,
		RequestData: map[string]any{
			"client_id":             client.ID.String(),
			"subject":               claims.Subject,
			"scope":                 scope,
			"sid":                   family.sid,
			"authentication_method": family.authMethod,
			"auth_time":             family.authTime.Unix(),
		},
		ClientID:  client.ID.String(),
		ExpiresAt: datastore.Ptr(now.Add(refreshTTL)),
	}
	if err := s.store.PutSession(ctx, accessRow); err != nil {
		return nil, err
	}
	if err := s.store.PutSession(ctx, refreshRow); err != nil {
		return nil, err
	}

	return &tokenResponse{
		AccessToken:  access,
		TokenType:    "Bearer",
		ExpiresIn:    int(accessTTL.Seconds()),
		RefreshToken: rawRefresh,
		IDToken:      idToken,
		Scope:        scope,
	}, nil
}

// signToken builds a compact access-token JWT with the given
// private claims; a "jti" entry promotes to the registered JWT ID
// claim instead of colliding with it.
func (s *Service) signToken(ctx context.Context, key jwk.Key, private map[string]any, ttl time.Duration, subject, audience string) (string, error) {
	signer, err := jwtutils.NewSigner[map[string]any](key, jwa.RS256())
	if err != nil {
		return "", err
	}

	now := time.Now().UTC()
	std := jwtutils.Standard{
		Issuer:    s.issuer,
		Subject:   subject,
		Audience:  []string{audience},
		IssuedAt:  now,
		ExpiresAt: now.Add(ttl),
	}
	if jti, ok := private["jti"].(string); ok {
		std.JWTID = jti
		delete(private, "jti")
	}
	return signer.Sign(private, std)
}

// signIDToken mints the OIDC ID token (aud, azp, nonce, sid,
// auth_time, profile claims, groups, custom claims). The jti
// mirrors the access token's, giving the end-session hint a path
// back to the token family.
func (s *Service) signIDToken(ctx context.Context, key jwk.Key, client Client, claims UserClaims, nonce string, authTime time.Time, ttl time.Duration, jti string) (string, error) {
	signer, err := jwtutils.NewSigner[map[string]any](key, jwa.RS256())
	if err != nil {
		return "", err
	}

	now := time.Now().UTC()
	private := map[string]any{
		"azp":       client.ID.String(),
		"nonce":     nonce,
		"sid":       claims.SessionID,
		"auth_time": authTime.Unix(),
		"groups":    claims.Groups,
	}
	if claims.Email != "" {
		private["email"] = claims.Email
		private["email_verified"] = claims.EmailVerified
	}
	if claims.Name != "" {
		private["name"] = claims.Name
	}
	if claims.PreferredUsername != "" {
		private["preferred_username"] = claims.PreferredUsername
	}
	for k, v := range claims.Custom {
		private[k] = v
	}

	std := jwtutils.Standard{
		Issuer:    s.issuer,
		Subject:   claims.Subject,
		Audience:  []string{client.ID.String()},
		JWTID:     jti,
		IssuedAt:  now,
		ExpiresAt: now.Add(ttl),
	}
	return signer.Sign(private, std)
}
