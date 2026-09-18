package totp

import (
	"net/http"

	"github.com/riipandi/tango/internal/kernel"
)

// The MFA surface serves ConnectRPC exclusively below /rpc. The
// pending-auth verification keeps the pending cookie as its
// credential; the module keeps the feature wired for its Connect
// registration.

// Feature is the wireable TOTP unit.
type Feature struct {
	service      *Service
	cookieSecure bool
	access       kernel.AccessAuthenticator
}

// NewFeature wires the TOTP service into the surface.
func NewFeature(service *Service) Feature {
	return Feature{service: service}
}

// WithCookie mirrors the session cookie Secure flag for the cookies
// the Connect verify response rides.
func (f Feature) WithCookie(secure bool) Feature {
	f.cookieSecure = secure
	return f
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
	return f.service.RPCService(f.cookieSecure, f.access)
}
