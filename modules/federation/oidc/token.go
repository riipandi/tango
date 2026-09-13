package oidc

// token.go holds the token-endpoint logic: client authentication,
// the authorization_code exchange (one-time code + PKCE), the
// refresh_token grant (rotation per use with family reuse
// revocation), and token minting. HTTP shells live in handler.go.

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"
	"time"

	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"

	"github.com/riipandi/tango/pkg/jwtutils"
	"github.com/riipandi/tango/pkg/responder"
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

// tokenError writes the RFC 6749 §5.2 error payload.
func tokenError(w http.ResponseWriter, r *http.Request, code string, status int) {
	responder.WriteJSON(w, status, map[string]any{
		"error":             code,
		"error_description": http.StatusText(status),
	})
}

// authenticateClient validates Basic or form-body client
// credentials. Public clients (no secret) authenticate by ID alone.
func (s *Service) authenticateClient(w http.ResponseWriter, r *http.Request) (Client, bool) {
	ctx := r.Context()

	var clientID, clientSecret string
	if id, secret, ok := r.BasicAuth(); ok {
		clientID, clientSecret = id, secret
	} else {
		clientID = r.PostFormValue("client_id")
		clientSecret = r.PostFormValue("client_secret")
	}

	if clientID == "" {
		tokenError(w, r, "invalid_client", http.StatusUnauthorized)
		return Client{}, false
	}

	id, err := OIDCParseClientID(clientID)
	if err != nil {
		tokenError(w, r, "invalid_client", http.StatusUnauthorized)
		return Client{}, false
	}
	client, err := s.store.GetClient(ctx, id)
	if err != nil {
		tokenError(w, r, "invalid_client", http.StatusUnauthorized)
		return Client{}, false
	}

	// Public clients authenticate by identity only; confidential
	// clients must present the secret (constant-time compare of
	// hashes).
	switch {
	case client.IsPublic:
		return client, true
	case clientSecret == "":
		tokenError(w, r, "invalid_client", http.StatusUnauthorized)
		return Client{}, false
	case !secretMatches(client, clientSecret):
		tokenError(w, r, "invalid_client", http.StatusUnauthorized)
		return Client{}, false
	}
	return client, true
}

// secretMatches compares the presented secret with the stored
// SHA-256.
func secretMatches(client Client, secret string) bool {
	if client.SecretHash == nil {
		return false
	}
	sum := sha256Hex(secret)
	return subtle.ConstantTimeCompare([]byte(*client.SecretHash), []byte(sum)) == 1
}

// exchangeCode performs the authorization_code grant: one-time
// consume, redirect_uri match, PKCE S256 verification, then token
// issuance with session bookkeeping.
func (s *Service) exchangeCode(w http.ResponseWriter, r *http.Request, client Client) {
	ctx := r.Context()

	code := r.PostFormValue("code")
	verifier := r.PostFormValue("code_verifier")
	if code == "" || verifier == "" {
		tokenError(w, r, "invalid_request", http.StatusBadRequest)
		return
	}

	sum := sha256Hex(code)
	consumed, err := s.store.ConsumeCode(ctx, sum)
	if err != nil {
		tokenError(w, r, "invalid_grant", http.StatusBadRequest)
		return
	}

	// The authorize_code session row carries the redirect_uri and
	// login context captured at /authorize time.
	seed, err := s.store.GetSession(ctx, KindAuthorizeCode, sum)
	if err != nil || seed.ClientID != client.ID.String() {
		tokenError(w, r, "invalid_grant", http.StatusBadRequest)
		return
	}
	redirectURI, _ := seed.RequestData["redirect_uri"].(string)
	sid, _ := seed.RequestData["sid"].(string)
	if redirectURI != r.PostFormValue("redirect_uri") || !client.MatchesCallback(redirectURI) {
		tokenError(w, r, "invalid_grant", http.StatusBadRequest)
		return
	}
	if !consumed.MethodSHA256 || pkceS256(verifier) != consumed.CodeChallenge {
		tokenError(w, r, "invalid_grant", http.StatusBadRequest)
		return
	}

	family := seedFamily(seed.RequestID, sid, consumed.AuthMethod, seedCreatedAt(seed))
	response, err := s.mintTokens(ctx, client, consumed.UserID, consumed.Scope, consumed.Nonce, family)
	if err != nil {
		tokenError(w, r, "server_error", http.StatusInternalServerError)
		return
	}

	if err := s.store.UpsertAuthorizedClient(ctx, consumed.UserID, client.ID.String(), strings.Fields(consumed.Scope)); err != nil {
		tokenError(w, r, "server_error", http.StatusInternalServerError)
		return
	}
	s.record(ctx, "oidc_token_issued", map[string]any{
		"client_id": client.ID.String(),
		"user_id":   consumed.UserID,
		"grant":     "authorization_code",
	})
	responder.WriteJSON(w, http.StatusOK, response)
}

// exchangeRefresh performs the refresh_token grant: rotation per
// use with reuse detection — replaying a rotated token revokes the
// whole family.
func (s *Service) exchangeRefresh(w http.ResponseWriter, r *http.Request, client Client) {
	ctx := r.Context()

	raw := r.PostFormValue("refresh_token")
	if raw == "" {
		tokenError(w, r, "invalid_request", http.StatusBadRequest)
		return
	}

	sum := sha256Hex(raw)
	session, err := s.store.GetSession(ctx, KindRefresh, sum)
	if err != nil || session.ClientID != client.ID.String() {
		tokenError(w, r, "invalid_grant", http.StatusUnauthorized)
		return
	}

	// Reuse detection: the family has already been rotated once, or
	// the jti is on the replay registry.
	if !session.Active {
		_ = s.store.DeactivateFamily(ctx, session.RequestID)
		tokenError(w, r, "invalid_grant", http.StatusUnauthorized)
		return
	}
	spent, err := s.store.JTIExists(ctx, sum)
	if err != nil || spent {
		_ = s.store.DeactivateFamily(ctx, session.RequestID)
		tokenError(w, r, "invalid_grant", http.StatusUnauthorized)
		return
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
	deactivateErr := s.store.DeactivateSession(ctx, KindRefresh, sum)
	if deactivateErr != nil {
		tokenError(w, r, "server_error", http.StatusInternalServerError)
		return
	}
	if recordErr := s.store.RecordJTI(ctx, sum, time.Now().UTC().Add(RefreshTokenTTL)); recordErr != nil {
		tokenError(w, r, "server_error", http.StatusInternalServerError)
		return
	}

	family := seedFamily(session.RequestID, sid, authMethod, authTime)
	response, err := s.mintTokens(ctx, client, userID, scope, "", family)
	if err != nil {
		tokenError(w, r, "server_error", http.StatusInternalServerError)
		return
	}

	s.record(ctx, "oidc_token_refreshed", map[string]any{
		"client_id": client.ID.String(),
		"user_id":   userID,
	})
	responder.WriteJSON(w, http.StatusOK, response)
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
func (s *Service) mintTokens(ctx context.Context, client Client, userID, scope, nonce string, family refreshContext) (*tokenResponse, error) {
	accessTTL := time.Duration(client.AccessTokenDurationMinutes) * time.Minute
	refreshTTL := time.Duration(client.RefreshTokenDurationMinutes) * time.Minute
	if accessTTL <= 0 {
		accessTTL = AccessTokenTTL
	}
	if refreshTTL <= 0 {
		refreshTTL = RefreshTokenTTL
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
	}, accessTTL, claims.Subject)
	if err != nil {
		return nil, err
	}

	idToken, err := s.signIDToken(ctx, signKey, client, claims, nonce, family.authTime, accessTTL)
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
		ExpiresAt: ptrTime(now.Add(accessTTL)),
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
		ExpiresAt: ptrTime(now.Add(refreshTTL)),
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
func (s *Service) signToken(ctx context.Context, key jwk.Key, private map[string]any, ttl time.Duration, subject string) (string, error) {
	signer, err := jwtutils.NewSigner[map[string]any](key, jwa.RS256())
	if err != nil {
		return "", err
	}

	now := time.Now().UTC()
	std := jwtutils.Standard{
		Issuer:    s.issuer,
		Subject:   subject,
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
// auth_time, profile claims, groups, custom claims).
func (s *Service) signIDToken(ctx context.Context, key jwk.Key, client Client, claims UserClaims, nonce string, authTime time.Time, ttl time.Duration) (string, error) {
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
		IssuedAt:  now,
		ExpiresAt: now.Add(ttl),
	}
	return signer.Sign(private, std)
}

// ptrTime returns a pointer to t.
func ptrTime(t time.Time) *time.Time { return &t }
