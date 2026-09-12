// Package auditlog captures domain events from other modules and
// exposes them via /api/audit-logs. Storage is abstracted behind
// Store; the composition root picks memory (tests) or Postgres.
package auditlog

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/riipandi/tango/pkg/responder"
)

// ModuleName identifies the audit log module in the registry.
const ModuleName = "auditlog"

// Module is the audit log feature: a Store plus a read-only listing
// endpoint.
type Module struct {
	store Store
}

// New builds the module on top of store; nil store panics.
func New(store Store) *Module {
	if store == nil {
		panic("auditlog: nil store")
	}
	return &Module{store: store}
}

func (m *Module) Name() string { return ModuleName }

// Record stores an audit entry through the configured store.
func (m *Module) Record(ctx context.Context, entry *Entry) error {
	return m.store.Record(ctx, entry)
}

// APIRoutes mounts the audit log read API inside the shared /api group.
func (m *Module) APIRoutes(r chi.Router) {
	r.Get("/audit-logs", m.listEntries)
}

func (m *Module) listEntries(w http.ResponseWriter, r *http.Request) {
	params, err := responder.ParsePagination(r)
	if err != nil {
		responder.BadRequestJSON(w, r, responder.ErrInvalidPagination.Error())
		return
	}

	entries, total, err := m.store.List(r.Context(), params)
	if err != nil {
		responder.Fail(w, r, http.StatusInternalServerError, "internal error")
		return
	}

	responder.Success(w, r, http.StatusOK, entries, responder.WithPaginationFrom(params, total))
}
