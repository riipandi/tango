package session

import (
	"github.com/go-chi/chi/v5"

	"github.com/riipandi/tango/modules/identity"
)

// APIRoutes mounts the retained auth endpoints inside the shared /api
// group: the stateless token refresh. Sign-in, sign-out, and session
// reads serve ConnectRPC exclusively — see handler_rpc.go.
func (s *Service) APIRoutes(r chi.Router, _ identity.RouteGroups) {
	r.Post("/auth/token", s.tokenBridge)
}
