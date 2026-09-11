package identity

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/internal/kernel"
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

type stubStartableFeature struct {
	stubFeature
	events *[]string
}

func (f stubStartableFeature) Start(ctx context.Context) error {
	*f.events = append(*f.events, "start:"+f.name)
	return nil
}

func (f stubStartableFeature) Stop(ctx context.Context) error {
	*f.events = append(*f.events, "stop:"+f.name)
	return nil
}

func TestFeatureRoutesMounted(t *testing.T) {
	mod := New(stubCore{}, stubAPIFeature{stubFeature{name: "stub"}})

	r := chi.NewRouter()
	r.Route("/api", mod.(kernel.APIRoutable).APIRoutes)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/stub", nil))
	require.Equal(t, http.StatusOK, w.Code)

	// Core routes keep working next to feature routes.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/users", nil))
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestModuleAPIRoot(t *testing.T) {
	mod := New(stubCore{})

	r := chi.NewRouter()
	r.Route("/api", mod.(kernel.APIRoutable).APIRoutes)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/", nil))
	require.Equal(t, http.StatusOK, w.Code)

	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, config.AppName, body["name"])
	assert.Equal(t, config.AppVersion, body["version"])
}

func TestModuleUnknownPath(t *testing.T) {
	mod := New(stubCore{})

	r := chi.NewRouter()
	r.Route("/api", mod.(kernel.APIRoutable).APIRoutes)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/nope", nil))
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestModuleMethodNotAllowed(t *testing.T) {
	mod := New(stubCore{})

	r := chi.NewRouter()
	r.Route("/api", mod.(kernel.APIRoutable).APIRoutes)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/api/users", nil))
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestNilCorePanics(t *testing.T) {
	require.Panics(t, func() {
		New(nil)
	})
}

func TestFeatureWithoutRoutesMountsHarmlessly(t *testing.T) {
	mod := New(stubCore{}, stubFeature{name: "plain"})

	r := chi.NewRouter()
	r.Route("/api", mod.(kernel.APIRoutable).APIRoutes)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/", nil))
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestDuplicateFeaturePanics(t *testing.T) {
	require.Panics(t, func() {
		New(stubCore{}, stubFeature{name: "x"}, stubFeature{name: "x"})
	})
}

func TestNilFeaturePanics(t *testing.T) {
	require.Panics(t, func() {
		New(stubCore{}, nil)
	})
}

func TestFeatureLifecycleOrder(t *testing.T) {
	var events []string
	mod := New(stubCore{},
		stubStartableFeature{stubFeature{name: "a"}, &events},
		stubFeature{name: "plain"},
		stubStartableFeature{stubFeature{name: "b"}, &events},
	).(kernel.Startable)

	require.NoError(t, mod.Start(context.Background()))
	require.NoError(t, mod.Stop(context.Background()))

	assert.Equal(t, []string{"start:a", "start:b", "stop:b", "stop:a"}, events)
}

func TestFeatureRootRoutesMounted(t *testing.T) {
	var mounted bool
	rootFeature := rootStub{stubFeature{name: "root"}, &mounted}

	mod := New(stubCore{}, rootFeature)
	require.Implements(t, (*kernel.RootRoutable)(nil), mod)

	r := chi.NewRouter()
	mod.(kernel.RootRoutable).Routes(r)
	assert.True(t, mounted, "root-routable feature must be mounted")
}

type rootStub struct {
	stubFeature
	mounted *bool
}

func (f rootStub) Routes(r chi.Router) { *f.mounted = true }

type failingStartable struct {
	stubFeature
}

func (f failingStartable) Start(ctx context.Context) error { return nil }

func (f failingStartable) Stop(ctx context.Context) error {
	return errors.New("boom")
}

func TestFeatureStopErrorWrapped(t *testing.T) {
	mod := New(stubCore{}, failingStartable{stubFeature{name: "bad"}})

	err := mod.(kernel.Startable).Stop(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), `stop feature "bad"`)
}
