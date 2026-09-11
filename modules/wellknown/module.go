// Package wellknown serves RFC 8615 discovery endpoints under
// /.well-known (OIDC discovery, JWKS, etc.).
package wellknown

import (
	"github.com/go-chi/chi/v5"

	"github.com/riipandi/tango/internal/kernel"
)

// ModuleName identifies the well-known module in the registry.
const ModuleName = "wellknown"

type Module struct{}

func New() kernel.Module {
	return &Module{}
}

func (m *Module) Name() string { return ModuleName }

// Routes mounts the discovery endpoints at the router root.
func (m *Module) Routes(r chi.Router) {
	// Get JSON Web Key Set (JWKS)
	r.Get("/.well-known/jwks.json", m.jwks)
	// Get current application version
	r.Get("/.well-known/version", m.version)
	// OIDC discovery
	r.Get("/.well-known/openid-configuration", m.openIDConfiguration)
}
