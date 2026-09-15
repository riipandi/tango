package auditlog

// handler.go owns the audit log HTTP endpoints.

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"go.jetify.com/typeid"

	"github.com/riipandi/tango/internal/transport/middleware"
	"github.com/riipandi/tango/pkg/responder"
)

// APIRoutes mounts the audit log read API inside the shared /api
// group. Without a guard wired the admin routes are skipped (fail
// closed); without a self authenticator the self listing is skipped.
func (m *Module) APIRoutes(r chi.Router) {
	if m.selfAuth != nil {
		r.With(middleware.RequireAuth(m.selfAuth, m.cookie)).Get("/audit-logs", m.listSelf)
	}

	if m.adminGuard == nil {
		return
	}
	admin := r.With(m.adminGuard)
	admin.Get("/audit-logs/all", m.listAll)
	admin.Get("/audit-logs/filters/users", m.userFilters)
	admin.Get("/audit-logs/filters/client-names", m.clientNameFilters)
}

// listSelf serves entries for the current user.
func (m *Module) listSelf(w http.ResponseWriter, r *http.Request) {
	principal, ok := middleware.PrincipalFromContext(r.Context())
	if !ok {
		responder.Fail(w, r, http.StatusUnauthorized, "authentication required")
		return
	}
	m.list(w, r, ListFilters{UserID: m.uuidUserID(principal.UserID)})
}

// listAll serves GET /api/audit-logs/all: every user's entries,
// admin only.
func (m *Module) listAll(w http.ResponseWriter, r *http.Request) {
	m.list(w, r, ListFilters{})
}

// list runs the shared listing with caller-provided scope filters.
func (m *Module) list(w http.ResponseWriter, r *http.Request, scope ListFilters) {
	params, err := responder.ParsePagination(r)
	if err != nil {
		responder.BadRequestJSON(w, r, responder.ErrInvalidPagination.Error())
		return
	}

	filters := scope
	filters.Event = r.URL.Query().Get("event")
	if raw := r.URL.Query().Get("user_id"); scope.UserID == "" && raw != "" {
		filters.UserID = m.uuidUserID(raw)
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
// dropdown values.
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

// uuidUserID normalizes a typed ID string (user_…) or a bare UUID
// to the UUID column form, keeping the module decoupled from the
// user package.
func (m *Module) uuidUserID(raw string) string {
	if id, err := typeid.FromString(raw); err == nil && !id.IsZero() {
		return id.UUID()
	}
	return raw
}
