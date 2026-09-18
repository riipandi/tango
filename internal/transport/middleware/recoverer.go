package middleware

import (
	"net/http"
	"strings"

	"github.com/riipandi/tango/pkg/responder"
)

func JSONRecoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				if strings.HasPrefix(r.URL.Path, "/api") {
					responder.Fail(w, r, http.StatusInternalServerError, "internal server error")
					return
				}
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
