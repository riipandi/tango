package auditlog

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	jsonv2 "encoding/json/v2"

	"github.com/go-chi/chi/v5"
	"github.com/riipandi/tango/internal/kernel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var fixedTime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// snapshot reads the module's events under lock (same package).
func snapshot(m *Module) []Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Event, len(m.events))
	copy(out, m.events)
	return out
}

func TestRecordSetsTimestamp(t *testing.T) {
	mod := New()
	mod.Record(Event{Action: "user.created"})

	events := snapshot(mod)
	require.Len(t, events, 1)
	assert.False(t, events[0].Timestamp.IsZero())
}

func TestRecordKeepsTimestamp(t *testing.T) {
	mod := New()
	mod.Record(Event{Action: "a", Timestamp: fixedTime})

	events := snapshot(mod)
	assert.True(t, events[0].Timestamp.Equal(fixedTime))
}

func TestListEventsEndpoint(t *testing.T) {
	mod := New()
	mod.Record(Event{Action: "user.created", Actor: "1"})

	reg := kernel.NewRegistry()
	reg.Register(mod)
	r := chi.NewRouter()
	r.Route("/api", reg.ApplyAPI)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/audit-logs", nil))

	require.Equal(t, http.StatusOK, w.Code)

	var events []Event
	require.NoError(t, jsonv2.Unmarshal(w.Body.Bytes(), &events))
	require.Len(t, events, 1)
	assert.Equal(t, "user.created", events[0].Action)
}

func TestModuleContracts(t *testing.T) {
	mod := New()
	assert.Equal(t, ModuleName, mod.Name())

	// Mounts as a kernel module.
	reg := kernel.NewRegistry()
	reg.Register(mod)
	r := chi.NewRouter()
	r.Route("/api", reg.ApplyAPI)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/audit-logs", nil))
	assert.Equal(t, http.StatusOK, w.Code)
}
