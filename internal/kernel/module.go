// Package kernel defines the module contract and registry for the
// modular monolith. A module owns its slice end-to-end and declares
// its routes; add/remove is one registry line.
package kernel

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/go-chi/chi/v5"
	"github.com/riipandi/tango/pkg/responder"
)

// Module is a self-contained feature unit: routes (RootRoutable or APIRoutable)
// or a lifecycle (Startable), at least one.
type Module interface {
	Name() string
}

// RootRoutable mounts routes on the router root.
type RootRoutable interface {
	Routes(r chi.Router)
}

// APIRoutable mounts inside the shared /api group (one group;
// chi forbids mounting the same path twice).
type APIRoutable interface {
	APIRoutes(r chi.Router)
}

// Middleware applies to the root router before routing.
type Middleware interface {
	Middleware() func(http.Handler) http.Handler
}

// Startable is for modules holding resources. Start runs in
// registration order, Stop in reverse.
type Startable interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

// Registry keeps modules in registration order.
type Registry struct {
	modules []Module
	byName  map[string]Module
}

func NewRegistry() *Registry {
	return &Registry{byName: make(map[string]Module)}
}

// Register adds a module; order sets route and middleware priority.
// Panics on duplicate name. Lifecycle-only modules (Startable, no
// routes) are valid: e.g. the task queue.
func (reg *Registry) Register(m Module) {
	if _, ok := m.(RootRoutable); !ok {
		if _, ok := m.(APIRoutable); !ok {
			if _, ok := m.(Startable); !ok {
				panic(fmt.Sprintf("kernel: module %q has no route capability", m.Name()))
			}
		}
	}

	if _, dup := reg.byName[m.Name()]; dup {
		panic(fmt.Sprintf("kernel: duplicate module %q", m.Name()))
	}
	reg.byName[m.Name()] = m
	reg.modules = append(reg.modules, m)
}

// Modules returns modules in registration order.
func (reg *Registry) Modules() []Module {
	return reg.modules
}

// Get returns a module by name, nil when absent.
func (reg *Registry) Get(name string) Module {
	return reg.byName[name]
}

// Apply wires module middleware, then mounts root routes.
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

// ApplyAPI mounts APIRoutable modules in the /api group, registration order.
func (reg *Registry) ApplyAPI(api chi.Router) {
	api.NotFound(responder.NotFoundJSON)
	api.MethodNotAllowed(responder.MethodNotAllowedJSON)

	for _, m := range reg.modules {
		if a, ok := m.(APIRoutable); ok {
			a.APIRoutes(api)
		}
	}
}

// Start starts Startable modules in order; stops at first error (caller Stops the rest).
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

// Stop stops Startable modules in reverse order, joining errors
// so one failure doesn't block the rest.
func (reg *Registry) Stop(ctx context.Context) error {
	var errs []error
	for _, v := range slices.Backward(reg.modules) {
		s, ok := v.(Startable)
		if !ok {
			continue
		}
		if err := s.Stop(ctx); err != nil {
			errs = append(errs, fmt.Errorf("stop module %q: %w", v.Name(), err))
		}
	}
	return errors.Join(errs...)
}
