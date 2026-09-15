// Package kernel defines small shared auth and runtime contracts.
package kernel

import (
	"context"

	"github.com/go-chi/chi/v5"
)

// Module is the minimal contract every runtime member satisfies.
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

// Startable is implemented by modules that own resources.
type Startable interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}
