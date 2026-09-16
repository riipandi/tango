package apikey

// handler_test.go pins the HTTP surface: every API-key route is
// session-guarded, so an API-key-authenticated caller can neither
// create nor renew keys (upstream's authWithoutApiKey rule).

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"

	"github.com/riipandi/tango/modules/identity"
)

func TestKeyRoutesAreSessionGuarded(t *testing.T) {
	denying := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		})
	}

	r := chi.NewRouter()
	r.Route("/api", func(api chi.Router) {
		NewService(nil, nil).APIRoutes(api, identity.RouteGroups{Self: denying})
	})

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/api-keys"},
		{http.MethodPost, "/api/api-keys"},
		{http.MethodDelete, "/api/api-keys/api_key_01abc"},
		{http.MethodPost, "/api/api-keys/api_key_01abc/renew"},
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
		assert.Equal(t, http.StatusUnauthorized, w.Code, tc.path)
	}
}
