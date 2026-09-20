package transport

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-chi/chi/v5"

	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/pkg/responder"
)

// APIRootHandler returns build metadata.
func APIRootHandler(w http.ResponseWriter, r *http.Request) {
	responder.Success(w, r, http.StatusOK, map[string]string{
		"name":     config.AppName,
		"version":  config.AppVersion,
		"platform": config.Platform,
		"build":    config.BuildDate,
		"hash":     config.BuildHash,
	})
}

// VersionHandler returns build metadata at /.well-known/version.
func VersionHandler(w http.ResponseWriter, r *http.Request) {
	responder.WriteJSON(w, http.StatusOK, map[string]string{
		"name":     config.AppName,
		"version":  config.AppVersion,
		"platform": config.Platform,
		"build":    config.BuildDate,
		"hash":     config.BuildHash,
	})
}

// RootHealthzHandler returns an empty 204 response.
func RootHealthzHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

// LatestVersionSource exposes the cached newest release for the RPC
// version surface.
type LatestVersionSource interface {
	Latest() string
}

// staticRoot is the Vite output directory.
const staticRoot = "web/output"

// StaticAssetsHandler serves files from the Vite output directory.
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
