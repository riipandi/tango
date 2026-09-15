package chiorder_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

// throwaway probe: which of two same-pattern groups answers?
func TestChiDuplicateRouteOrder(t *testing.T) {
	r := chi.NewRouter()
	name := func(n string) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200); _, _ = w.Write([]byte(n)) }
	}
	r.Group(func(ar chi.Router) {
		ar.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, rq *http.Request) {
				w.Header().Set("X-Hit", "first"); next.ServeHTTP(w, rq)
			})
		})
		ar.Get("/probe", name("first"))
	})
	r.Group(func(ar chi.Router) {
		ar.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, rq *http.Request) {
				w.Header().Set("X-Hit", "second"); next.ServeHTTP(w, rq)
			})
		})
		ar.Get("/probe", name("second"))
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/probe", nil))
	t.Logf("status=%d x-hit=%s body=%s", w.Code, w.Header().Get("X-Hit"), w.Body.String())
}
