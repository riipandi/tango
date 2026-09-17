// Package discovery serves RFC 8615 endpoints:
// /.well-known/openid-configuration and /.well-known/jwks.json.
package discovery

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/riipandi/tango/modules/federation"
	"github.com/riipandi/tango/pkg/jwtutils"
	"github.com/riipandi/tango/pkg/responder"
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
	AuthorizeEndpoint  = "/authorize"
	TokenEndpoint      = "/api/oidc/token"
	UserInfoEndpoint   = "/api/oidc/userinfo"
	EndSessionEndpoint = "/api/oidc/end-session"
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

// jwks publishes the public halves of every currently published
// signing key, including retired keys inside the rotation overlap.
// The set is built from public PEMs only, so no private material
// can leak; no envelope — clients expect a bare JWKS document.
func (f Feature) jwks(w http.ResponseWriter, r *http.Request) {
	set, err := f.provider.VerifyKeySet(r.Context())
	if err != nil {
		responder.Fail(w, r, http.StatusInternalServerError, "failed to load signing keys")
		return
	}

	w.Header().Set("Cache-Control", jwksCacheControl)
	responder.WriteJSON(w, http.StatusOK, set)
}

// openIDConfiguration serves the OIDC discovery document.
func (f Feature) openIDConfiguration(w http.ResponseWriter, r *http.Request) {
	responder.WriteJSON(w, http.StatusOK, newDiscoveryDocument(f.issuer))
}

// oauthServerMetadata serves the RFC 8414 authorization-server
// metadata — the same capability set under the OAuth 2.0 name.
func (f Feature) oauthServerMetadata(w http.ResponseWriter, r *http.Request) {
	responder.WriteJSON(w, http.StatusOK, newDiscoveryDocument(f.issuer))
}
