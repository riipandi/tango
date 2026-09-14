package transport

import (
	"net/http"
	"os"
	"path/filepath"

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
	responder.Success(w, r, http.StatusOK, map[string]string{
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

// RootHealthzHandler serves upstream-parity /healthz: bare 204, no
// body. The JSON variant stays at /api/healthz (tango extension).
func RootHealthzHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

// VersionCurrentHandler serves GET /api/version/current (upstream
// parity: bare build metadata object).
func VersionCurrentHandler(w http.ResponseWriter, r *http.Request) {
	responder.WriteJSON(w, http.StatusOK, map[string]string{
		"version": config.AppVersion,
	})
}

// VersionLatestHandler serves GET /api/version/latest. The lookup
// of newer releases is an outbound job (phase 7); for now the
// deployed version is the freshest known.
func VersionLatestHandler(w http.ResponseWriter, r *http.Request) {
	responder.WriteJSON(w, http.StatusOK, map[string]string{
		"version": config.AppVersion,
	})
}

// staticRoot is where the Vite build lands (public/ files copied
// under web/output). Served by the /static/* route in dev and when
// the SPA is not embedded.
const staticRoot = "web/output"

// StaticAssetsHandler serves files from the Vite output directory
// under /static/* (e.g. /static/images/logo.png). Falls back to the
// JSON 404 when the file does not exist.
func StaticAssetsHandler(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "*")
	clean := filepath.Clean("/" + name) // forces relative, no ".."
	full := filepath.Join(staticRoot, clean)

	info, err := os.Stat(full)
	if err != nil || info.IsDir() {
		responder.NotFoundJSON(w, r)
		return
	}
	http.ServeFile(w, r, full)
}
