package transport

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/pkg/responder"
)

// HealthCheckHandler reports liveness: the process is up and
// serving. Readiness probes for real dependencies (database, cache)
// belong in dedicated checks as those dependencies land.
func HealthCheckHandler(w http.ResponseWriter, r *http.Request) {
	responder.WriteJSON(w, http.StatusOK, map[string]string{
		"status": "healthy",
	})
}

// APIRootHandler is the shared /api group's index: application build
// metadata. Build-meta values are ldflags-injected package globals,
// so no runtime config is needed.
func APIRootHandler(w http.ResponseWriter, r *http.Request) {
	responder.WriteJSON(w, http.StatusOK, map[string]string{
		"name":     config.AppName,
		"version":  config.AppVersion,
		"platform": config.Platform,
		"build":    config.BuildDate,
		"hash":     config.BuildHash,
	})
}

func StaticAssetsHandler(w http.ResponseWriter, r *http.Request) {
	path := chi.URLParam(r, "*")
	responder.WriteJSON(w, http.StatusOK, map[string]string{
		"path": path,
	})
}
