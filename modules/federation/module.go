// Package federation is the optional identity provider surface for
// other systems (OIDC/OAuth 2.0, SCIM, discovery). See schema.go.
package federation

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/go-chi/chi/v5"
)

// ModuleName identifies the federation module in the registry.
const ModuleName = "federation"

// Module is the federation feature unit: the selected provider
// features.
type Module struct {
	// features in selection order; seen guards duplicates.
	features []Feature
	seen     map[string]bool
}

// New builds the module from the selected features; anything omitted
// has no routes, storage, or lifecycle. Fails fast on a malformed
// feature set.
func New(features ...Feature) *Module {
	m := &Module{seen: make(map[string]bool, len(features))}

	for _, f := range features {
		if f == nil {
			panic("federation: nil feature")
		}
		if m.seen[f.Name()] {
			panic(fmt.Sprintf("federation: duplicate feature %q", f.Name()))
		}
		m.seen[f.Name()] = true
		m.features = append(m.features, f)
	}

	return m
}

func (m *Module) Name() string { return ModuleName }

// APIRoutes mounts every selected feature's endpoints inside the
// shared /api group. Implements kernel.APIRoutable.
func (m *Module) APIRoutes(r chi.Router) {
	for _, f := range m.features {
		if af, ok := f.(APIFeature); ok {
			af.APIRoutes(r)
		}
	}
}

// Routes mounts root-router routes declared by root-routable
// features. Implements kernel.RootRoutable.
func (m *Module) Routes(r chi.Router) {
	for _, f := range m.features {
		if rf, ok := f.(RootRoutableFeature); ok {
			rf.Routes(r)
		}
	}
}

// Start starts startable features in selection order. Implements
// kernel.Startable.
func (m *Module) Start(ctx context.Context) error {
	for _, f := range m.features {
		if sf, ok := f.(StartableFeature); ok {
			if err := sf.Start(ctx); err != nil {
				return fmt.Errorf("start feature %q: %w", f.Name(), err)
			}
		}
	}
	return nil
}

// Stop stops startable features in reverse selection order and joins
// all errors so one failing feature does not block the rest.
func (m *Module) Stop(ctx context.Context) error {
	var errs []error
	for _, f := range slices.Backward(m.features) {
		if sf, ok := f.(StartableFeature); ok {
			if err := sf.Stop(ctx); err != nil {
				errs = append(errs, fmt.Errorf("stop feature %q: %w", f.Name(), err))
			}
		}
	}
	return errors.Join(errs...)
}
