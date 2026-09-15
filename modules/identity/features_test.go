package identity

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubFeature struct {
	name string
}

func (f stubFeature) Name() string { return f.name }

type stubAPIFeature struct {
	stubFeature
}

func (f stubAPIFeature) APIRoutes(r chi.Router) {
	r.Get("/stub", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
}

type stubCore struct {
	stubFeature
}

func (f stubCore) APIRoutes(r chi.Router) {
	r.Get("/users", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
}

// newTestModule fills the mandatory feature slots with stubs; the
// five optional slots (passkeys..signup) come from the caller.
func newTestModule(core APIFeature, optional ...APIFeature) *Module {
	mandatory := []APIFeature{
		core,
		stubAPIFeature{stubFeature{name: "account"}},
		stubAPIFeature{stubFeature{name: "sessions"}},
		stubAPIFeature{stubFeature{name: "groups"}},
		stubAPIFeature{stubFeature{name: "claims"}},
	}
	optionalSlots := make([]APIFeature, 5)
	copy(optionalSlots, optional)
	tail := []APIFeature{
		stubAPIFeature{stubFeature{name: "apiaccess"}},
		stubAPIFeature{stubFeature{name: "apikeys"}},
	}
	all := append(append(mandatory, optionalSlots...), tail...)
	return New(all[0], all[1], all[2], all[3], all[4], all[5], all[6], all[7], all[8], all[9], all[10], all[11])
}

func TestFeatureRoutesMounted(t *testing.T) {
	mod := newTestModule(stubCore{}, stubAPIFeature{stubFeature{name: "stub"}})

	r := chi.NewRouter()
	r.Route("/api", mod.APIRoutes)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/stub", nil))
	require.Equal(t, http.StatusOK, w.Code)

	// Core routes keep working next to feature routes.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/users", nil))
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestModuleUnknownPath(t *testing.T) {
	mod := newTestModule(stubCore{})

	r := chi.NewRouter()
	r.Route("/api", mod.APIRoutes)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/nope", nil))
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestModuleMethodNotAllowed(t *testing.T) {
	mod := newTestModule(stubCore{})

	r := chi.NewRouter()
	r.Route("/api", mod.APIRoutes)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/api/users", nil))
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestNilCorePanics(t *testing.T) {
	require.Panics(t, func() {
		newTestModule(nil)
	})
}
