// Package auditlog captures domain events from other modules and
// exposes them via /api/audit-logs. Storage is abstracted behind
// Store; the composition root picks Postgres (production) — no
// memory store.
package auditlog

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/riipandi/tango/pkg/responder"
)

// ModuleName identifies the audit log module in the registry.
const ModuleName = "auditlog"

// Module is the audit log feature: a Store plus a read-only listing
// API. The listing mounts behind the admin guard wired by the
// composition root.
type Module struct {
	store Store
	guard func(http.Handler) http.Handler
}

// New builds the module on top of store; nil store panics.
func New(store Store) *Module {
	if store == nil {
		panic("auditlog: nil store")
	}
	return &Module{store: store}
}

// MountAdminAPI wires the admin guard for the listing routes. It
// must be called before the registry applies routes (the guard
// depends on the session feature built after this module).
func (m *Module) MountAdminAPI(guard func(http.Handler) http.Handler) {
	m.guard = guard
}

func (m *Module) Name() string { return ModuleName }

// Record stores an audit entry through the configured store.
func (m *Module) Record(ctx context.Context, entry *Entry) error {
	return m.store.Record(ctx, entry)
}

// APIRoutes mounts the audit log read API inside the shared /api
// group.
func (m *Module) APIRoutes(r chi.Router) {
	mount := func(ar chi.Router) {
		ar.Get("/audit-logs", m.listEntries)
		ar.Get("/audit-logs/filters/users", m.userFilters)
		ar.Get("/audit-logs/filters/client-names", m.clientNameFilters)
	}

	if m.guard == nil {
		mount(r)
		return
	}
	r.Group(func(ar chi.Router) {
		ar.Use(m.guard)
		mount(ar)
	})
}

func (m *Module) listEntries(w http.ResponseWriter, r *http.Request) {
	params, err := responder.ParsePagination(r)
	if err != nil {
		responder.BadRequestJSON(w, r, responder.ErrInvalidPagination.Error())
		return
	}

	filters := ListFilters{
		UserID: r.URL.Query().Get("user_id"),
		Event:  r.URL.Query().Get("event"),
	}
	if raw := r.URL.Query().Get("from"); raw != "" {
		parsed, parseErr := time.Parse(time.RFC3339, raw)
		if parseErr != nil {
			responder.BadRequestJSON(w, r, "from must be RFC3339")
			return
		}
		filters.From = &parsed
	}
	if raw := r.URL.Query().Get("to"); raw != "" {
		parsed, parseErr := time.Parse(time.RFC3339, raw)
		if parseErr != nil {
			responder.BadRequestJSON(w, r, "to must be RFC3339")
			return
		}
		filters.To = &parsed
	}

	entries, total, err := m.store.List(r.Context(), filters, params)
	if err != nil {
		responder.Fail(w, r, http.StatusInternalServerError, "internal error")
		return
	}

	responder.Success(w, r, http.StatusOK, entries, responder.WithPaginationFrom(params, total))
}

// userFilters lists distinct users appearing in the log (filter
// dropdown values upstream).
func (m *Module) userFilters(w http.ResponseWriter, r *http.Request) {
	values, err := m.store.UserFilterValues(r.Context())
	if err != nil {
		responder.Fail(w, r, http.StatusInternalServerError, "internal error")
		return
	}
	responder.Success(w, r, http.StatusOK, values)
}

// clientNameFilters lists distinct client names from payloads.
func (m *Module) clientNameFilters(w http.ResponseWriter, r *http.Request) {
	values, err := m.store.ClientNameFilterValues(r.Context())
	if err != nil {
		responder.Fail(w, r, http.StatusInternalServerError, "internal error")
		return
	}
	responder.Success(w, r, http.StatusOK, values)
}
