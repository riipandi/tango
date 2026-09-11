// Package kernel defines the module contract and registry that powers
// the modular monolith. A module owns its feature slice end-to-end
// (handlers, services, storage) and declares its own routes, so adding
// or removing a feature is a single registry line in the composition
// root (cmd/launcher).
package kernel

import (
	"context"
	"errors"
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

// Startable is an optional lifecycle capability for modules holding
// resources (database pools, background workers, consumers). Start
// runs in registration order, Stop in reverse order.
type Startable interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
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

// Start starts every Startable module in registration order. It stops
// at the first error; already-started modules are left running (the
// caller should Stop them).
func (reg *Registry) Start(ctx context.Context) error {
	for _, m := range reg.modules {
		s, ok := m.(Startable)
		if !ok {
			continue
		}
		if err := s.Start(ctx); err != nil {
			return fmt.Errorf("start module %q: %w", m.Name(), err)
		}
	}
	return nil
}

// Stop stops every Startable module in reverse registration order and
// joins all errors so one failing module does not block the rest.
func (reg *Registry) Stop(ctx context.Context) error {
	var errs []error
	for i := len(reg.modules) - 1; i >= 0; i-- {
		s, ok := reg.modules[i].(Startable)
		if !ok {
			continue
		}
		if err := s.Stop(ctx); err != nil {
			errs = append(errs, fmt.Errorf("stop module %q: %w", reg.modules[i].Name(), err))
		}
	}
	return errors.Join(errs...)
}
