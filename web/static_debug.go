//go:build !release

package web

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/riipandi/tango/pkg/responder"
)

func SetupStatic(r chi.Router) {
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		// These endpoints should return JSON or plain text output
		if strings.HasPrefix(r.URL.Path, "/.well-known") ||
			strings.HasPrefix(r.URL.Path, "/api") ||
			strings.HasPrefix(r.URL.Path, "/rpc") ||
			strings.HasPrefix(r.URL.Path, "/metrics") ||
			strings.HasPrefix(r.URL.Path, "/static") {
			responder.NotFoundJSON(w, r)
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	})
}
