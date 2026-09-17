package oidc

// userinfo.go holds the userinfo-endpoint use cases: access-token
// introspection via the published JWKS and scoped claim assembly.
// The HTTP shell and the RFC 6750 error rendering live in
// handler_token.go.

import (
	"context"

	"github.com/lestrrat-go/jwx/v3/jwa"

	"github.com/riipandi/tango/pkg/jwtutils"
)

// introspectAccessToken verifies the access token against the
// published key set and returns its claims + scope.
func (s *Service) introspectAccessToken(ctx context.Context, token string) (accessClaims, string, error) {
	set, err := s.keys.VerifyKeySet(ctx)
	if err != nil {
		return accessClaims{}, "", err
	}

	verifier, err := jwtutils.NewVerifier[map[string]any](nil, jwa.RS256())
	if err != nil {
		return accessClaims{}, "", err
	}
	verified, err := verifier.WithKeySet(set).WithIssuer(s.issuer).Verify(token)
	if err != nil {
		return accessClaims{}, "", err
	}

	scope, _ := verified.Private["scope"].(string)
	sid, _ := verified.Private["sid"].(string)
	clientID, _ := verified.Private["client_id"].(string)
	return accessClaims{
		Subject:  verified.Subject,
		Scope:    scope,
		SID:      sid,
		ClientID: clientID,
	}, scope, nil
}

// accessClaims is the subset of access-token private claims the
// userinfo endpoint relies on.
type accessClaims struct {
	Subject  string
	Scope    string
	SID      string
	ClientID string
}
