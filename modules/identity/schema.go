// Package identity owns internal authn/authz: user accounts plus the
// selectable features wired at the composition root. The provider
// surface for other systems lives in modules/federation.
package identity

import (
	"context"

	"github.com/go-chi/chi/v5"
	"go.jetify.com/typeid"

	"github.com/riipandi/tango/internal/kernel"
)

// NewID generates a UUIDv7-backed typed ID. Panics only on an invalid
// prefix, impossible for the prefixes declared in feature packages.
func NewID[T typeid.Subtype, PT typeid.SubtypePtr[T]]() T {
	return typeid.Must(typeid.New[T, PT]())
}

// ParseID decodes a typed ID string, rejecting prefix mismatches.
func ParseID[T typeid.Subtype, PT typeid.SubtypePtr[T]](s string) (T, error) {
	return typeid.Parse[T, PT](s)
}

// Recorder receives audit events; the composition root adapts the sink
// so features never import auditlog directly.
type Recorder func(ctx context.Context, event AuditEvent)

type AuditEvent struct {
	Action string
	Actor  string
	Target string
}

// Feature is one selectable unit: a feature left out of New has no
// routes, storage, or lifecycle.
type Feature interface {
	Name() string
}

// APIFeature mounts endpoints inside the shared /api group.
type APIFeature interface {
	Feature
	APIRoutes(r chi.Router)
}

// StartableFeature holds resources with a lifecycle: started after the
// module core, stopped before it.
type StartableFeature interface {
	Feature
	kernel.Startable
}

// RootRoutableFeature mounts routes on the root router, outside /api.
type RootRoutableFeature interface {
	Feature
	Routes(r chi.Router)
}
