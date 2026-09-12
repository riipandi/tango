// Package federation is the optional identity provider surface this
// application exposes to other systems (OIDC/OAuth 2.0, SCIM,
// discovery). It has no mandatory core: it is exactly the features
// the composition root selects. Remove its registration line to
// build a pure internal-identity binary.
package federation

import (
	"github.com/go-chi/chi/v5"

	"github.com/riipandi/tango/internal/kernel"
)

// Feature is one selectable unit chosen at the composition root. A
// feature left out of New does not exist: no routes, no storage, no
// lifecycle.
type Feature interface {
	Name() string
}

// APIFeature mounts endpoints inside the shared /api group.
type APIFeature interface {
	Feature
	APIRoutes(r chi.Router)
}

// StartableFeature holds resources with a lifecycle: started in
// selection order, stopped in reverse.
type StartableFeature interface {
	Feature
	kernel.Startable
}

// RootRoutableFeature mounts routes on the root router, outside /api
// — e.g. the OIDC /authorize endpoint.
type RootRoutableFeature interface {
	Feature
	Routes(r chi.Router)
}
