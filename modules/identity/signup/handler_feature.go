package signup

import (
	"net/http"

	"github.com/riipandi/tango/internal/kernel"
)

// The signup surface serves ConnectRPC exclusively below /rpc; the
// email-link token exchange stays on its one-time access REST route.
// The module keeps the feature wired for its Connect registration.

// Feature marks the signup feature inside the identity module.
type Feature struct {
	service *Service
	access  kernel.AccessAuthenticator
}

// New wires the feature to its service.
func New(service *Service) Feature { return Feature{service: service} }

// Name names the feature for logs.
func (Feature) Name() string { return "signup" }

// WithAccessAuthenticator wires the bearer resolver the admin token
// procedures guard with.
func (f Feature) WithAccessAuthenticator(access kernel.AccessAuthenticator) Feature {
	f.access = access
	return f
}

// RPCService returns the Connect registration for the signup surface.
func (f Feature) RPCService() (string, http.Handler) {
	return f.service.RPCService(f.access)
}
