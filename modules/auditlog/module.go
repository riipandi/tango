// Package auditlog provides the audit trail module: it captures
// domain events from other modules and exposes them via /api/audit-logs.
package auditlog

import (
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/riipandi/tango/pkg/responder"
)

// ModuleName identifies the audit log module in the registry.
const ModuleName = "auditlog"

// Module is the audit log feature: an in-memory sink plus a read-only
// listing endpoint. Swap the sink for a DB-backed implementation
// when persistence lands.
type Module struct {
	mu     sync.Mutex
	events []Event
}

func New() *Module {
	return &Module{}
}

func (m *Module) Name() string { return ModuleName }

// Record stores an audit event. Safe for concurrent use.
func (m *Module) Record(event Event) {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, event)
}

// APIRoutes mounts the audit log read API inside the shared /api group.
func (m *Module) APIRoutes(r chi.Router) {
	r.Get("/audit-logs", m.listEvents)
}

func (m *Module) listEvents(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	events := make([]Event, len(m.events))
	copy(events, m.events)
	m.mu.Unlock()

	responder.WriteJSON(w, http.StatusOK, events)
}
