//go:build release

package web

import (
	"embed"
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/riipandi/tango/pkg/responder"
)

//go:embed all:output
var webFS embed.FS

func SetupStatic(r chi.Router) {
	r.NotFound(spaHandler())
}

func spaHandler() http.HandlerFunc {
	webArtifact, _ := fs.Sub(webFS, "output")
	fileServer := http.FileServer(http.FS(webArtifact))

	return func(w http.ResponseWriter, r *http.Request) {
		// API, Well-Known, and Static endpoints should return JSON 404
		if strings.HasPrefix(r.URL.Path, "/api") ||
			strings.HasPrefix(r.URL.Path, "/.well-known") ||
			strings.HasPrefix(r.URL.Path, "/static") {
			responder.NotFoundJSON(w, r)
			return
		}

		reqPath := strings.TrimPrefix(r.URL.Path, "/")

		if reqPath != "" {
			cleanPath := filepath.Clean(reqPath)
			if !strings.HasPrefix(cleanPath, ".") {
				if f, err := webArtifact.Open(cleanPath); err == nil {
					f.Close()
					fileServer.ServeHTTP(w, r)
					return
				}
			}
		}

		http.ServeFileFS(w, r, webArtifact, "index.html")
	}
}
