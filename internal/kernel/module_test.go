package kernel

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

	assert.Len(t, reg.Modules(), 2)
	require.NotNil(t, reg.Get("a"))
	assert.Nil(t, reg.Get("missing"))

	r := chi.NewRouter()
	reg.Apply(r)

	for _, path := range []string{"/api/a", "/b"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		assert.Equal(t, 200, w.Code, path)
	}
}

func TestRegistryDuplicateNamePanics(t *testing.T) {
	require.Panics(t, func() {
		reg := NewRegistry()
		reg.Register(&stubModule{name: "dup"})
		reg.Register(&stubModule{name: "dup"})
	})
}

// bareModule has no route or lifecycle capability.
type bareModule struct{}

func (m bareModule) Name() string { return "bare" }

func TestRegisterRejectsModuleWithoutRoutes(t *testing.T) {
	require.Panics(t, func() {
		reg := NewRegistry()
		reg.Register(bareModule{})
	})
}

type mwModule struct {
	stubModule
}

func (m *mwModule) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Module", m.name)
			next.ServeHTTP(w, r)
		})
	}
}

func TestApplyWiresModuleMiddleware(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&mwModule{stubModule{name: "mw"}})
	reg.Register(&stubModule{name: "a", mount: func(r chi.Router) {
		r.Get("/a", func(w http.ResponseWriter, r *http.Request) {})
	}})

	r := chi.NewRouter()
	reg.Apply(r)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/a", nil))
	require.Equal(t, 200, w.Code)
	assert.Equal(t, "mw", w.Header().Get("X-Module"))
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

	require.NoError(t, reg.Start(context.Background()))
	require.NoError(t, reg.Stop(context.Background()))

	assert.Equal(t, []string{"start:a", "start:b", "stop:b", "stop:a"}, events)
}
