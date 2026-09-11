// Package identity owns user management and all authn/authz concerns:
// HTTP layer, business rules, and storage. Mounted inside the shared
// /api group.
package identity

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/go-chi/chi/v5"

	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/internal/kernel"
	"github.com/riipandi/tango/pkg/responder"
)

// ModuleName identifies the identity module in the registry.
const ModuleName = "identity"

// Module is the identity feature unit: the mandatory user core plus
// whichever authn/authz features the composition root selected.
type Module struct {
	core APIFeature

	// features in selection order; seen guards duplicates.
	features []Feature
	seen     map[string]bool
}

// New builds the identity module from its mandatory user core plus
// the selected authn/authz features. A feature not passed here does
// not exist: no routes, no storage, no lifecycle. Construction fails
// fast on a malformed feature set.
func New(core APIFeature, features ...Feature) kernel.Module {
	if core == nil {
		panic("identity: nil user core")
	}

	m := &Module{
		core: core,
		seen: make(map[string]bool, len(features)),
	}

	for _, f := range features {
		if f == nil {
			panic("identity: nil feature")
		}
		if m.seen[f.Name()] {
			panic(fmt.Sprintf("identity: duplicate feature %q", f.Name()))
		}
		m.seen[f.Name()] = true
		m.features = append(m.features, f)
	}

	return m
}

func (m *Module) Name() string { return ModuleName }

// APIRoutes mounts the identity API root, the user core, and every
// selected feature's endpoints inside the shared /api group.
// Implements kernel.APIRoutable.
func (m *Module) APIRoutes(r chi.Router) {
	r.Get("/", m.apiRoot)
	m.core.APIRoutes(r)

	for _, f := range m.features {
		if af, ok := f.(APIFeature); ok {
			af.APIRoutes(r)
		}
	}
}

// Routes mounts root-router routes (outside /api) declared by
// root-routable features — e.g. the OIDC /authorize endpoint.
// Implements kernel.RootRoutable.
func (m *Module) Routes(r chi.Router) {
	for _, f := range m.features {
		if rf, ok := f.(RootRoutableFeature); ok {
			rf.Routes(r)
		}
	}
}

// Start starts the user core then startable features in selection
// order. Implements kernel.Startable.
func (m *Module) Start(ctx context.Context) error {
	if sf, ok := m.core.(StartableFeature); ok {
		if err := sf.Start(ctx); err != nil {
			return fmt.Errorf("start feature %q: %w", m.core.Name(), err)
		}
	}

	for _, f := range m.features {
		if sf, ok := f.(StartableFeature); ok {
			if err := sf.Start(ctx); err != nil {
				return fmt.Errorf("start feature %q: %w", f.Name(), err)
			}
		}
	}
	return nil
}

// Stop stops startable features in reverse selection order (core
// last) and joins all errors so one failing feature does not block
// the rest.
func (m *Module) Stop(ctx context.Context) error {
	var errs []error
	for _, f := range slices.Backward(m.features) {
		if sf, ok := f.(StartableFeature); ok {
			if err := sf.Stop(ctx); err != nil {
				errs = append(errs, fmt.Errorf("stop feature %q: %w", f.Name(), err))
			}
		}
	}

	if sf, ok := m.core.(StartableFeature); ok {
		if err := sf.Stop(ctx); err != nil {
			errs = append(errs, fmt.Errorf("stop feature %q: %w", m.core.Name(), err))
		}
	}
	return errors.Join(errs...)
}

func (m *Module) apiRoot(w http.ResponseWriter, r *http.Request) {
	responder.WriteJSON(w, http.StatusOK, map[string]string{
		"name":     config.AppName,
		"version":  config.AppVersion,
		"platform": config.Platform,
		"build":    config.BuildDate,
		"hash":     config.BuildHash,
	})
}
