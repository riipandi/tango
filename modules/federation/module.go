// Package federation is the optional identity provider surface for
// other systems (OIDC/OAuth 2.0, SCIM, discovery). See schema.go.
package federation

import (
	"context"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/riipandi/tango/internal/kernel"
)

// ModuleName identifies the federation module in the registry.
const ModuleName = "federation"

// Feature is one mounted unit chosen at the composition root. A
// feature left out of New does not exist: no routes, no storage, no
// lifecycle.
type Feature interface {
	Name() string
}

// APIFeature mounts endpoints inside the shared /api group. A
// feature that serves ConnectRPC exclusively implements
// RPCServiceProvider instead — the two are mutually exclusive.
type APIFeature interface {
	Feature
	APIRoutes(r chi.Router, g RouteGroups)
}

// RPCServiceProvider exposes a Connect registration instead of REST
// routes; the composition root mounts it under /rpc.
type RPCServiceProvider interface {
	Feature
	RPCService() (string, http.Handler)
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

// RouteGroups carries the authentication middleware chains a feature
// applies at mount time; the composition root builds each chain once.
// A nil group leaves its guarded routes unmounted (fail closed).
type RouteGroups struct {
	Admin kernel.Guard // session auth + admin requirement
	Self  kernel.Guard // session auth (cookie)
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
	scim      RPCServiceProvider
	keys      StartableFeature
	discovery RootRoutableFeature
}

// New wires the federation surface. The signing-key service is
// mandatory; a nil one is a wiring bug.
func New(provider ProviderFeature, scim RPCServiceProvider, keys StartableFeature, discovery RootRoutableFeature) *Module {
	if provider == nil || scim == nil || keys == nil || discovery == nil {
		panic("federation: nil module dependency")
	}
	return &Module{provider: provider, scim: scim, keys: keys, discovery: discovery}
}

func (m *Module) Name() string { return ModuleName }

// APIRoutes mounts the provider feature endpoints inside the shared
// /api group. The SCIM surface serves ConnectRPC exclusively.
func (m *Module) APIRoutes(r chi.Router, g RouteGroups) {
	m.provider.APIRoutes(r, g)
}

// rpcServiceProvider is implemented by features exposing a Connect
// service beside their REST routes.
type rpcServiceProvider interface {
	RPCService() (string, http.Handler)
}

// consentRPCProvider is the consent surface's Connect registration.
type consentRPCProvider interface {
	ConsentRPCService() (string, http.Handler)
}

// notFoundRPC is the stub for unwired surfaces; the prefix stays a
// valid chi pattern that never matches real traffic, and the handler
// answers a Connect-style 404 document.
func notFoundRPC() (string, http.Handler) {
	return "/tango.void.v1.NotFound/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"code":"not_found","message":"procedure not found"}`))
	})
}

// ClientRPCService returns the OIDC client administration Connect
// registration (admin-only surface).
func (m *Module) ClientRPCService() (string, http.Handler) {
	if p, ok := m.provider.(rpcServiceProvider); ok {
		return p.RPCService()
	}
	return notFoundRPC()
}

// ConsentRPCService returns the consent Connect registration (mixed
// self/admin visibility).
func (m *Module) ConsentRPCService() (string, http.Handler) {
	if p, ok := m.provider.(consentRPCProvider); ok {
		return p.ConsentRPCService()
	}
	return notFoundRPC()
}

// ScimRPCService returns the SCIM provider Connect registration
// (admin-only surface).
func (m *Module) ScimRPCService() (string, http.Handler) {
	if p, ok := m.scim.(rpcServiceProvider); ok {
		return p.RPCService()
	}
	return notFoundRPC()
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
