// Package kernel defines the module contract and registry that powers
// the modular monolith. A module owns its feature slice end-to-end
// (handlers, services, storage) and declares its own routes, so adding
// or removing a feature is a single registry line in the composition
// root (cmd/launcher).
package kernel

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/riipandi/tango/pkg/responder"
)

// Module is a self-contained feature unit. It must implement at least
// one routing capability: RootRoutable (mounts on the router root,
// e.g. wellknown) or APIRoutable (mounts inside the shared /api
// group, e.g. identity).
type Module interface {
	Name() string
}

// RootRoutable is the capability to mount routes on the root router.
type RootRoutable interface {
	Routes(r chi.Router)
}

// APIRoutable is the capability to mount routes inside the shared
// /api subtree. The registry mounts them inside a single "/api"
// group (chi forbids mounting the same path twice).
type APIRoutable interface {
	APIRoutes(r chi.Router)
}

// Middleware is an optional capability a module can provide. Modules
// implementing it have their middleware applied to the root router
// before any routing happens.
type Middleware interface {
	Middleware() func(http.Handler) http.Handler
}

// Registry keeps module instances in registration order.
type Registry struct {
	modules []Module
	byName  map[string]Module
}

func NewRegistry() *Registry {
	return &Registry{byName: make(map[string]Module)}
}

// Register adds a module. Registration order determines route
// priority and middleware order.
func (reg *Registry) Register(m Module) {
	if _, ok := m.(RootRoutable); !ok {
		if _, ok := m.(APIRoutable); !ok {
			panic(fmt.Sprintf("kernel: module %q has no route capability", m.Name()))
		}
	}

	if _, dup := reg.byName[m.Name()]; dup {
		panic(fmt.Sprintf("kernel: duplicate module %q", m.Name()))
	}
	reg.byName[m.Name()] = m
	reg.modules = append(reg.modules, m)
}

// Modules returns registered modules in registration order.
func (reg *Registry) Modules() []Module {
	return reg.modules
}

// Get returns a module by name, or nil when not registered.
func (reg *Registry) Get(name string) Module {
	return reg.byName[name]
}

// Apply mounts root-level module routes and wires module-provided
// middleware.
func (reg *Registry) Apply(r chi.Router) {
	for _, m := range reg.modules {
		if mw, ok := m.(Middleware); ok {
			r.Use(mw.Middleware())
		}
	}
	for _, m := range reg.modules {
		if rr, ok := m.(RootRoutable); ok {
			rr.Routes(r)
		}
	}
}

// ApplyAPI mounts every APIRoutable module inside the shared /api
// group, in registration order.
func (reg *Registry) ApplyAPI(api chi.Router) {
	api.NotFound(responder.NotFoundJSON)
	api.MethodNotAllowed(responder.MethodNotAllowedJSON)

	for _, m := range reg.modules {
		if a, ok := m.(APIRoutable); ok {
			a.APIRoutes(api)
		}
	}
}
