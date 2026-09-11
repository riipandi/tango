package kernel

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

type stubModule struct {
	name  string
	mount func(r chi.Router)
}

func (m *stubModule) Name() string { return m.name }
func (m *stubModule) Routes(r chi.Router) {
	if m.mount != nil {
		m.mount(r)
	}
}

func TestRegistryRegisterAndApply(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&stubModule{name: "a", mount: func(r chi.Router) {
		r.Get("/api/a", func(w http.ResponseWriter, r *http.Request) {})
	}})
	reg.Register(&stubModule{name: "b", mount: func(r chi.Router) {
		r.Get("/b", func(w http.ResponseWriter, r *http.Request) {})
	}})

	if got := len(reg.Modules()); got != 2 {
		t.Fatalf("expected 2 modules, got %d", got)
	}
	if reg.Get("a") == nil || reg.Get("missing") != nil {
		t.Fatal("Get lookup failed")
	}

	r := chi.NewRouter()
	reg.Apply(r)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/a", nil))
	if w.Code != 200 {
		t.Fatalf("/api/a status = %d", w.Code)
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/b", nil))
	if w.Code != 200 {
		t.Fatalf("/b status = %d", w.Code)
	}
}

func TestRegistryDuplicateNamePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on duplicate module name")
		}
	}()

	reg := NewRegistry()
	reg.Register(&stubModule{name: "dup"})
	reg.Register(&stubModule{name: "dup"})
}

type startableModule struct {
	stubModule
	events *[]string
}

func (m *startableModule) Start(ctx context.Context) error {
	*m.events = append(*m.events, "start:"+m.name)
	return nil
}

func (m *startableModule) Stop(ctx context.Context) error {
	*m.events = append(*m.events, "stop:"+m.name)
	return nil
}

func TestRegistryLifecycleOrder(t *testing.T) {
	var events []string
	reg := NewRegistry()
	reg.Register(&startableModule{stubModule{name: "a"}, &events})
	reg.Register(&startableModule{stubModule{name: "b"}, &events})
	reg.Register(&stubModule{name: "plain"}) // no lifecycle

	if err := reg.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := reg.Stop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}

	want := []string{"start:a", "start:b", "stop:b", "stop:a"}
	if len(events) != len(want) {
		t.Fatalf("expected %v, got %v", want, events)
	}
	for i := range want {
		if events[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, events)
		}
	}
}
