package transport_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"

	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/internal/kernel"
	"github.com/riipandi/tango/internal/transport"
	"github.com/riipandi/tango/internal/transport/middleware"
)

// countingLimiter records every check the limiter is asked for, so a test can
// tell a throttled route from one the limiter never saw.
type countingLimiter struct{ calls int }

func (c *countingLimiter) Allow(context.Context, string) (middleware.Result, error) {
	c.calls++
	return middleware.Result{Limit: 60, Remaining: 59}, nil
}

// apiFeature mounts its own route under /api, the way an application-API
// feature does, and a protocol endpoint on the router's root.
type apiFeature struct{}

func (apiFeature) Name() string { return "api-feature" }

func (apiFeature) Mount(r chi.Router) {
	r.Get("/api/feature/thing", func(w http.ResponseWriter, _ *http.Request) {})
	r.Get("/.well-known/thing", func(w http.ResponseWriter, _ *http.Request) {})
}

// TestAModuleRouteIsThrottled is the regression this pins: the limiter used to
// be attached to the /api subrouter, which a module's own routes never pass
// through, so every feature route was exempt from the policy the API's own
// routes were held to.
func TestAModuleRouteIsThrottled(t *testing.T) {
	cases := []struct {
		name string
		path string
	}{
		{"the API's own route", "/api/"},
		{"a feature route under /api", "/api/feature/thing"},
		{"a protocol endpoint on the root", "/.well-known/thing"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			limiter := &countingLimiter{}
			router := transport.NewRouter(transport.Options{
				Config:      config.Default(),
				RateLimiter: limiter,
				Modules:     []kernel.Module{apiFeature{}},
			})

			router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, tc.path, nil))

			assert.Equal(t, 1, limiter.calls, "%s must be throttled", tc.path)
		})
	}
}

// TestTheSPAAndMetricsAreNotThrottled covers the other side of the boundary:
// the limiter guards the routes a client calls, not the assets it loads or a
// scrape. Those paths are outside the group the limiter wraps.
func TestTheSPAAndMetricsAreNotThrottled(t *testing.T) {
	limiter := &countingLimiter{}
	metrics := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {})
	cfg := config.Default()
	cfg.OTEL.Metrics.PrometheusPath = "/metrics"

	router := transport.NewRouter(transport.Options{
		Config:      cfg,
		RateLimiter: limiter,
		Metrics:     metrics,
	})

	for _, path := range []string{"/metrics", "/"} {
		router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
	}

	assert.Zero(t, limiter.calls, "a metrics scrape and an SPA load spend no rate-limit check")
}

// TestAnExcludedModulePathIsSpared keeps the exclusion list meaningful now
// that it covers module routes too.
func TestAnExcludedModulePathIsSpared(t *testing.T) {
	limiter := &countingLimiter{}
	router := transport.NewRouter(transport.Options{
		Config:      config.Default(),
		RateLimiter: limiter,
	})

	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/healthz", nil))

	assert.Zero(t, limiter.calls, "the health probe is excluded, so it reaches no check")
}
