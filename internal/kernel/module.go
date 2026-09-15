// Package kernel defines module contracts and the module registry.
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

// Module is a self-contained feature unit with routes or a lifecycle.
type Module interface {
	Name() string
}

// RootRoutable mounts routes on the root router.
type RootRoutable interface {
	Routes(r chi.Router)
}

// APIRoutable mounts routes inside the shared /api group.
type APIRoutable interface {
	APIRoutes(r chi.Router)
}

// Middleware applies middleware to the root router.
type Middleware interface {
	Middleware() func(http.Handler) http.Handler
}

// Startable is implemented by modules that own resources.
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

// Register adds a module and panics on duplicate names or missing capabilities.
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

// Apply wires middleware, then mounts root routes.
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

// ApplyAPI mounts API routes in registration order.
func (reg *Registry) ApplyAPI(api chi.Router) {
	api.NotFound(responder.NotFoundJSON)
	api.MethodNotAllowed(responder.MethodNotAllowedJSON)

	for _, m := range reg.modules {
		if a, ok := m.(APIRoutable); ok {
			a.APIRoutes(api)
		}
	}
}

// Start starts lifecycle modules in registration order.
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

// Stop stops lifecycle modules in reverse order and joins errors.
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
