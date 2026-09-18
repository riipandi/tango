// Package discovery serves RFC 8615 endpoints:
// /.well-known/openid-configuration and /.well-known/jwks.json.
package discovery

import (
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/riipandi/tango/modules/federation"
	"github.com/riipandi/tango/pkg/jwtutils"
)

// Well-known endpoint paths and cache policy.
const (
	JWKSPath   = "/.well-known/jwks.json"
	ConfigPath = "/.well-known/openid-configuration"
	// OAuthServerPath is the RFC 8414 authorization-server metadata mirror of the OIDC discovery document.
	OAuthServerPath = "/.well-known/oauth-authorization-server"

	jwksCacheControl = "public, max-age=300, must-revalidate"
)

// OIDC endpoint paths advertised in the discovery document.
const (
	AuthorizeEndpoint     = "/authorize"
	TokenEndpoint         = "/api/oidc/token"
	UserInfoEndpoint      = "/api/oidc/userinfo"
	EndSessionEndpoint    = "/api/oidc/end-session"
	IntrospectionEndpoint = "/api/oidc/introspect"
	// PushedAuthorizationRequestEndpoint is the RFC 9126 PAR endpoint.
	PushedAuthorizationRequestEndpoint = "/api/oidc/par"
	// DeviceAuthorizationEndpoint is the RFC 8628 device authorization endpoint.
	DeviceAuthorizationEndpoint = "/api/oidc/device/authorize"
	JWKSURI                     = "/.well-known/jwks.json"
)

// Feature serves the discovery document and JWKS from the shared
// key provider.
type Feature struct {
	provider jwtutils.KeyProvider
	issuer   string
}

var _ federation.RootRoutableFeature = Feature{}

// New builds the feature; issuer is the public base URL (a
// trailing slash is trimmed).
func New(provider jwtutils.KeyProvider, issuer string) Feature {
	return Feature{provider: provider, issuer: strings.TrimRight(issuer, "/")}
}

// Name implements federation.Feature.
func (Feature) Name() string { return "discovery" }

// Routes mounts the discovery endpoints on the root router.
func (f Feature) Routes(r chi.Router) {
	r.Get(JWKSPath, f.jwks)
	r.Get(ConfigPath, f.openIDConfiguration)
	r.Get(OAuthServerPath, f.oauthServerMetadata)
}
