// Package federation is the optional identity provider surface for
// other systems (OIDC/OAuth 2.0, SCIM, discovery). See schema.go.
package federation

import (
	"context"
	"fmt"

	"github.com/go-chi/chi/v5"
)

// ModuleName identifies the federation module in the registry.
const ModuleName = "federation"

// Feature is one mounted unit chosen at the composition root. A
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
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

// RootRoutableFeature mounts routes on the root router, outside /api
// — e.g. the OIDC /authorize endpoint.
type RootRoutableFeature interface {
	Feature
	Routes(r chi.Router)
}

// ProviderFeature is the OIDC surface: protocol routes on the root
// router plus client/token management inside /api.
type ProviderFeature interface {
	APIFeature
	RootRoutableFeature
}

// Module is the federation surface: the explicitly wired provider
// features, mounted in construction order. There is no feature
// discovery or late registration.
type Module struct {
	provider  ProviderFeature
	scim      APIFeature
	keys      StartableFeature
	discovery RootRoutableFeature
}

// New wires the federation surface. The signing-key service is
// mandatory; a nil one is a wiring bug.
func New(provider ProviderFeature, scim APIFeature, keys StartableFeature, discovery RootRoutableFeature) *Module {
	if provider == nil || scim == nil || keys == nil || discovery == nil {
		panic("federation: nil module dependency")
	}
	return &Module{provider: provider, scim: scim, keys: keys, discovery: discovery}
}

func (m *Module) Name() string { return ModuleName }

// APIRoutes mounts the provider feature endpoints inside the shared
// /api group.
func (m *Module) APIRoutes(r chi.Router) {
	m.provider.APIRoutes(r)
	m.scim.APIRoutes(r)
}

// Routes mounts the root-router routes (OIDC /authorize, discovery)
// outside /api.
func (m *Module) Routes(r chi.Router) {
	m.provider.Routes(r)
	m.discovery.Routes(r)
}

// Start guarantees the signing key exists before token issuance.
func (m *Module) Start(ctx context.Context) error {
	if err := m.keys.Start(ctx); err != nil {
		return fmt.Errorf("start feature %q: %w", m.keys.Name(), err)
	}
	return nil
}

// Stop stops the federation features in reverse construction order.
func (m *Module) Stop(ctx context.Context) error {
	return m.keys.Stop(ctx)
}
