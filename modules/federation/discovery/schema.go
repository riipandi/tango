// Package discovery serves RFC 8615 endpoints:
// /.well-known/openid-configuration and /.well-known/jwks.json.
package discovery

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/riipandi/tango/modules/federation"
	"github.com/riipandi/tango/pkg/responder"
)

// Feature is the wireable discovery unit.
type Feature struct{}

// New returns the placeholder feature. The real constructor takes
// the signing-key provider from the oidc package via a
// consumer-side adapter in the registry.
func New() Feature { return Feature{} }

// Name implements federation.Feature.
func (Feature) Name() string { return "discovery" }

var _ federation.Feature = Feature{}

// Routes mounts the discovery endpoints on the root router.
func (Feature) Routes(r chi.Router) {
	r.Get("/.well-known/jwks.json", jwks)
	r.Get("/.well-known/openid-configuration", openIDConfiguration)
}

func jwks(w http.ResponseWriter, _ *http.Request) {
	// TODO: serve real JWKS from the configured signing keys.
	responder.WriteJSON(w, http.StatusOK, map[string]any{
		"message": "Not yet implemented",
	})
}

func openIDConfiguration(w http.ResponseWriter, _ *http.Request) {
	// TODO: full OIDC discovery document once the oidc package
	// exposes its token endpoints.
	responder.WriteJSON(w, http.StatusOK, map[string]any{
		"message": "Not yet implemented",
	})
}
