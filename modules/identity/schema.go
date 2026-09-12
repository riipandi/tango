// Package identity owns internal authn/authz: user accounts plus the
// selectable features wired at the composition root. The identity
// provider surface for other systems lives in the optional
// modules/federation package.
package identity

import (
	"github.com/go-chi/chi/v5"
	"github.com/riipandi/tango/internal/kernel"
)

// User is the core entity, shared by all features. The optional
// federation module consumes it via adapters.
type User struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Recorder receives audit events; the composition root adapts any
// sink (e.g. auditlog) so features never import it directly.
type Recorder func(event AuditEvent)

// AuditEvent is what features emit to a Recorder.
type AuditEvent struct {
	Action string
	Actor  string
	Target string
}

// Feature is one selectable unit chosen at the composition root. A
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

// StartableFeature holds resources with a lifecycle: started after
// the module core, stopped before it.
type StartableFeature interface {
	Feature
	kernel.Startable
}

// RootRoutableFeature mounts routes on the root router, outside /api.
type RootRoutableFeature interface {
	Feature
	Routes(r chi.Router)
}
