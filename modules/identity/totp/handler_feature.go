package totp

import (
	"net/http"

	"github.com/riipandi/tango/internal/kernel"
)

// The MFA surface serves ConnectRPC exclusively below /rpc. The
// pending verification carries its bridge token in the request body;
// the module keeps the feature wired for its Connect registration.

// Feature is the wireable TOTP unit.
type Feature struct {
	service *Service
	access  kernel.AccessAuthenticator
}

// NewFeature wires the TOTP service into the surface.
func NewFeature(service *Service) Feature {
	return Feature{service: service}
}

// WithAccessAuthenticator wires the bearer resolver the self
// procedures guard with.
func (f Feature) WithAccessAuthenticator(access kernel.AccessAuthenticator) Feature {
	f.access = access
	return f
}

// Name names the feature for logs.
func (Feature) Name() string { return "totp" }

// RPCService returns the Connect registration for the MFA surface.
func (f Feature) RPCService() (string, http.Handler) {
	return f.service.RPCService(f.access)
}
