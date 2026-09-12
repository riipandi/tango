package transport

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/pkg/responder"
)

// HealthCheckHandler reports liveness. Readiness for real deps
// (db, cache) gets dedicated checks when they land.
func HealthCheckHandler(w http.ResponseWriter, r *http.Request) {
	responder.WriteJSON(w, http.StatusOK, map[string]string{
		"status": "healthy",
	})
}

// APIRootHandler is the /api index: build metadata from
// ldflags globals, no runtime config needed.
func APIRootHandler(w http.ResponseWriter, r *http.Request) {
	responder.WriteJSON(w, http.StatusOK, map[string]string{
		"name":     config.AppName,
		"version":  config.AppVersion,
		"platform": config.Platform,
		"build":    config.BuildDate,
		"hash":     config.BuildHash,
	})
}

// VersionHandler serves RFC 8615 /.well-known/version: the same
// build metadata as the /api index. Identity-provider discovery
// (openid-configuration, jwks.json) lives in the optional
// federation module, not here.
func VersionHandler(w http.ResponseWriter, r *http.Request) {
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
