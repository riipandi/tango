// Package identity is the identity area: the features that establish who a
// caller is and what they may do.
//
// The area is the unit the composition root loads. It mounts every feature it
// holds on one router, so the registry names one module per area rather than
// one per feature, and a new identity feature is added here without the
// transport or the registry learning about it.
package identity

import (
	"github.com/go-chi/chi/v5"

	"github.com/riipandi/tango/internal/kernel"
	"github.com/riipandi/tango/modules/identity/jwks"
)

// ModuleName is the name the area reports under.
const ModuleName = "identity"

// Deps are the resolved services the area's features are built from. The
// registry resolves them; the area decides which feature takes which, so a
// feature's dependencies are named here rather than at the call site.
type Deps struct {
	// KeySet publishes the JSON Web Key Set.
	KeySet *jwks.Service
}

// Module mounts every identity feature.
type Module struct {
	features []kernel.Module
}

// NewModule builds the area over its dependencies.
func NewModule(deps Deps) *Module {
	return &Module{features: features(deps)}
}

// Name reports the area in composition reports and logs.
func (m *Module) Name() string { return ModuleName }

// Mount registers every feature's endpoints on the router. It runs once, at
// startup, before the listener opens.
//
// The features mount in the order features returns. chi replaces the handler
// of a pattern registered twice, so the last one wins; two features claiming
// the same route is a defect to fix, not an ordering to rely on.
func (m *Module) Mount(r chi.Router) {
	kernel.Mount(r, m.features...)
}

// features is the area's feature list, the one place an identity feature is
// named. A feature that serves a protocol endpoint — the key set, a discovery
// document — is mounted on the router's root; one that serves the application
// API mounts itself under /api.
func features(deps Deps) []kernel.Module {
	return []kernel.Module{
		jwks.NewModule(deps.KeySet),
	}
}
