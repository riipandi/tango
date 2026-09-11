// Package identity owns user management: HTTP layer, business rules,
// and storage. Mounted inside the shared /api group.
package identity

import (
	"github.com/go-chi/chi/v5"

	"github.com/riipandi/tango/internal/kernel"
)

// ModuleName identifies the identity module in the registry.
const ModuleName = "identity"

// Module is the identity feature unit.
type Module struct {
	service *Service
}

// New builds the identity module on top of the given store. The
// optional recorder captures audit events (consumer-side interface —
// pass e.g. the auditlog module).
func New(store Store, recorder Recorder) kernel.Module {
	return &Module{service: NewService(store, recorder)}
}

func (m *Module) Name() string { return ModuleName }

// APIRoutes mounts the user management endpoints inside the shared
// /api group.
func (m *Module) APIRoutes(r chi.Router) {
	r.Get("/", m.apiRoot)
	r.Post("/users", m.createUser)
	r.Get("/users", m.listUsers)
	r.Get("/users/{id}", m.getUser)
}
