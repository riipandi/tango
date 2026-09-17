package transport

import (
	"context"
	"net/http"
	"time"

	"github.com/alexliesenfeld/health"
)

// HealthCheck probes one dependency for the readiness endpoint.
// Check implementations belong to the composition root; transport
// only owns the HTTP surface.
type HealthCheck struct {
	Name  string
	Check func(ctx context.Context) error
}

// Probe budgets: each dependency check is bounded, and results are
// cached so probes cannot hammer the datastore.
const (
	healthCheckTimeout = 2 * time.Second
	healthCacheTTL     = 5 * time.Second
)

// newHealthHandler builds the readiness handler. The response is the
// library's bare JSON document — {"status":"up"} with per-check
// details — and 503 when any check fails; no responder envelope.
func newHealthHandler(checks []HealthCheck) http.Handler {
	opts := []health.CheckerOption{
		health.WithCacheDuration(healthCacheTTL),
		health.WithTimeout(2 * healthCheckTimeout),
	}
	for _, c := range checks {
		check := c
		opts = append(opts, health.WithCheck(health.Check{
			Name:    check.Name,
			Timeout: healthCheckTimeout,
			Check:   check.Check,
		}))
	}
	return health.NewHandler(health.NewChecker(opts...))
}
