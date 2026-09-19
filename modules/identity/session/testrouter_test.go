package session

import (
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/modules/identity/password"
	"github.com/riipandi/tango/modules/identity/user"
)

// newTestRouter mounts the retained auth endpoints (the worker's
// cookie bridge and the sign-out fallback) over the shared test
// container. Returns the router and the services for provisioning.
func newTestRouter(t *testing.T, opts ...ServiceOption) (chi.Router, *Service, *password.Service, user.Store) {
	sessions, passwords, users := newTestStack(t, opts...)

	r := chi.NewRouter()
	r.Route("/api", func(r chi.Router) {
		sessions.APIRoutes(r, identity.RouteGroups{})
	})
	return r, sessions, passwords, users
}
