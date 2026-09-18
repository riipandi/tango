package oidc

// end_session.go implements the RP-initiated logout contract: verify
// the ID-token hint, revoke the token family minted by that grant,
// and resolve the registered post-logout redirect.

import (
	"context"
	"errors"
	"net/url"

	"github.com/lestrrat-go/jwx/v3/jwa"

	"github.com/riipandi/tango/pkg/jwtutils"
)

// End-session failures; the handler folds every one of them into the
// same redirect so callers cannot probe token or authorization state.
var (
	ErrInvalidHint           = errors.New("oidc: id_token_hint is invalid")
	ErrMissingAuthorization  = errors.New("oidc: the user has not authorized this client")
	ErrInvalidLogoutCallback = errors.New("oidc: post_logout_redirect_uri is not registered")
)

// EndSession verifies the ID-token hint against the published key
// set, revokes the token family the ID token was minted with, and
// resolves the post-logout redirect target. An empty callback means
// the client registered no logout URLs; the caller falls back to the
// instance logout page.
func (s *Service) EndSession(ctx context.Context, hint, clientID, postLogoutRedirectURI string) (string, error) {
	if hint == "" {
		return "", ErrInvalidHint
	}

	set, err := s.keys.VerifyKeySet(ctx)
	if err != nil {
		return "", ErrInvalidHint
	}
	verifier, err := jwtutils.NewVerifier[map[string]any](nil, jwa.RS256())
	if err != nil {
		return "", ErrInvalidHint
	}
	verified, err := verifier.WithKeySet(set).
		WithIssuer(s.issuer).
		WithRequiredClaims("jti", "aud", "sub").
		Verify(hint)
	if err != nil {
		return "", ErrInvalidHint
	}

	audience := verified.Audience
	if len(audience) == 0 || audience[0] == "" {
		return "", ErrInvalidHint
	}
	hintedClient := audience[0]
	if clientID != "" && clientID != hintedClient {
		return "", ErrInvalidHint
	}
	if verified.Subject == "" || verified.JWTID == "" {
		return "", ErrInvalidHint
	}

	hintedClientID, parseErr := OIDCParseClientID(hintedClient)
	if parseErr != nil {
		return "", ErrInvalidHint
	}
	client, err := s.store.GetClient(ctx, hintedClientID)
	if err != nil {
		return "", ErrInvalidHint
	}
	authorized, err := s.store.HasAuthorizedClient(ctx, verified.Subject, hintedClient)
	if err != nil {
		return "", err
	}
	if !authorized {
		return "", ErrMissingAuthorization
	}

	// Resolve the callback first: like upstream's transaction, a
	// bad post-logout URI must not cost the grant its tokens.
	callback := ""
	if len(client.LogoutCallbackURLs) > 0 {
		if postLogoutRedirectURI == "" {
			callback = client.LogoutCallbackURLs[0]
		} else {
			for _, registered := range client.LogoutCallbackURLs {
				if registered == postLogoutRedirectURI {
					callback = postLogoutRedirectURI
					break
				}
			}
			if callback == "" {
				return "", ErrInvalidLogoutCallback
			}
		}
	}

	// The access token carries the same jti as its session row; the
	// whole family (refresh included) dies with the grant. A missing
	// row skips revocation, matching upstream's tolerant store.
	if access, getErr := s.store.GetSession(ctx, KindAccessToken, verified.JWTID); getErr == nil {
		if deactErr := s.store.DeactivateFamily(ctx, access.RequestID); deactErr != nil {
			return "", deactErr
		}
	}
	return callback, nil
}

// appendStateToURL re-encodes the callback with the state parameter.
func appendStateToURL(callbackURL, state string) string {
	if state == "" {
		return callbackURL
	}
	parsed, err := url.Parse(callbackURL)
	if err != nil {
		return callbackURL
	}
	query := parsed.Query()
	query.Set("state", state)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}
