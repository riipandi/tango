package oidc

// userinfo.go holds the userinfo-endpoint logic: access-token
// introspection via the published JWKS and scoped claim assembly.
// The HTTP shell lives in handler.go.

import (
	"net/http"

	"github.com/lestrrat-go/jwx/v3/jwa"

	"github.com/riipandi/tango/pkg/jwtutils"
	"github.com/riipandi/tango/pkg/responder"
)

// introspectAccessToken verifies the access token against the
// published key set and returns its claims + scope.
func (s *Service) introspectAccessToken(r *http.Request, token string) (accessClaims, string, error) {
	set, err := s.keys.VerifyKeySet(r.Context())
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

// userInfoError writes the RFC 6750 §3 WWW-Authenticate error form.
func userInfoError(w http.ResponseWriter, r *http.Request, status int, code, description string) {
	w.Header().Set("WWW-Authenticate", `Bearer error="`+code+`", error_description="`+description+`"`)
	responder.Fail(w, r, status, description)
}
