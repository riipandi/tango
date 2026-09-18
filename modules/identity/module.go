// Package identity owns internal authn/authz: user accounts plus the
// features wired at the composition root. The provider surface for
// other systems lives in modules/federation.
package identity

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/riipandi/tango/internal/kernel"
)

// Feature is one mounted unit inside the identity surface: a feature
// left out of New has no routes.
type Feature interface {
	Name() string
}

// RouteGroups carries the authentication middleware chains a feature
// applies at mount time; the composition root builds each chain once.
// A nil group leaves its guarded routes unmounted (fail closed).
type RouteGroups struct {
	Admin  kernel.Guard // session auth + admin requirement
	Self   kernel.Guard // session auth (cookie)
	APIKey kernel.Guard // API-key auth
}

// APIFeature mounts endpoints inside the shared /api group.
type APIFeature interface {
	Feature
	APIRoutes(r chi.Router, g RouteGroups)
}

// Module is the identity surface: the user core plus each explicitly
// wired feature, mounted in construction order. There is no feature
// discovery or late registration.
type Module struct {
	core      APIFeature
	account   Feature
	sessions  APIFeature
	groups    Feature
	claims    APIFeature
	passkeys  APIFeature
	devices   APIFeature
	onetime   APIFeature
	emailv    APIFeature
	signup    APIFeature
	apiaccess APIFeature
	apikeys   APIFeature
	recovery  APIFeature
	totp      APIFeature
}

// New wires the identity surface. The user core is mandatory; a nil
// one is a wiring bug. Any other feature left out (nil) simply has no
// routes.
func New(
	core APIFeature,
	account Feature,
	sessions APIFeature,
	groups Feature,
	claims APIFeature,
	passkeys APIFeature,
	devices APIFeature,
	onetime APIFeature,
	emailv APIFeature,
	signup APIFeature,
	apiaccess APIFeature,
	apikeys APIFeature,
	recovery APIFeature,
	totp APIFeature,
) *Module {
	if core == nil {
		panic("identity: nil user core")
	}
	return &Module{
		core:      core,
		account:   account,
		sessions:  sessions,
		groups:    groups,
		claims:    claims,
		passkeys:  passkeys,
		devices:   devices,
		onetime:   onetime,
		emailv:    emailv,
		signup:    signup,
		apiaccess: apiaccess,
		apikeys:   apikeys,
		recovery:  recovery,
		totp:      totp,
	}
}

// rpcServiceProvider is implemented by features that expose a Connect
// service beside their REST routes.
type rpcServiceProvider interface {
	RPCService() (string, http.Handler)
}

// userRPCProvider is the user core's Connect registration; it takes
// the shared access authenticator because the surface mixes admin
// and self procedures.
type userRPCProvider interface {
	RPCService(auth kernel.AccessAuthenticator) (string, http.Handler)
}

// notFoundRPC is the stub for unwired surfaces; unknown procedures
// already answer Connect 404s at the mount.
func notFoundRPC() (string, http.Handler) {
	return "", http.NotFoundHandler()
}

// UserRPCService returns the user-core Connect registration.
func (m *Module) UserRPCService(auth kernel.AccessAuthenticator) (string, http.Handler) {
	if provider, ok := m.core.(userRPCProvider); ok {
		return provider.RPCService(auth)
	}
	return notFoundRPC()
}

// GroupRPCService returns the group Connect registration.
func (m *Module) GroupRPCService() (string, http.Handler) {
	if provider, ok := m.groups.(rpcServiceProvider); ok {
		return provider.RPCService()
	}
	return notFoundRPC()
}

// AccountRPCService returns the self-service account Connect
// registration.
func (m *Module) AccountRPCService() (string, http.Handler) {
	if provider, ok := m.account.(rpcServiceProvider); ok {
		return provider.RPCService()
	}
	return notFoundRPC()
}

// APIKeyRPCService returns the API key Connect registration, or the
// not-found stub when the feature is unwired.
func (m *Module) APIKeyRPCService() (string, http.Handler) {
	if provider, ok := m.apikeys.(rpcServiceProvider); ok && m.apikeys != nil {
		return provider.RPCService()
	}
	return "", http.NotFoundHandler()
}

// APIRoutes mounts the user core, then every wired feature's
// endpoints, in construction order. Unwired features stay unmounted.
// Account and groups carry no REST routes anymore — they serve
// ConnectRPC exclusively.
func (m *Module) APIRoutes(r chi.Router, g RouteGroups) {
	m.core.APIRoutes(r, g)
	if m.sessions != nil {
		m.sessions.APIRoutes(r, g)
	}
	if m.claims != nil {
		m.claims.APIRoutes(r, g)
	}
	if m.passkeys != nil {
		m.passkeys.APIRoutes(r, g)
	}
	if m.devices != nil {
		m.devices.APIRoutes(r, g)
	}
	if m.onetime != nil {
		m.onetime.APIRoutes(r, g)
	}
	if m.emailv != nil {
		m.emailv.APIRoutes(r, g)
	}
	if m.signup != nil {
		m.signup.APIRoutes(r, g)
	}
	if m.apiaccess != nil {
		m.apiaccess.APIRoutes(r, g)
	}
	if m.apikeys != nil {
		m.apikeys.APIRoutes(r, g)
	}
	if m.recovery != nil {
		m.recovery.APIRoutes(r, g)
	}
	if m.totp != nil {
		m.totp.APIRoutes(r, g)
	}
}
