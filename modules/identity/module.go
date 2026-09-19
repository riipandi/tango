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
	claims    Feature
	passkeys  APIFeature
	devices   APIFeature
	onetime   Feature
	emailv    Feature
	signup    Feature
	apiaccess Feature
	apikeys   Feature
	recovery  APIFeature
	totp      Feature
}

// New wires the identity surface. The user core is mandatory; a nil
// one is a wiring bug. Any other feature left out (nil) simply has no
// routes.
func New(
	core APIFeature,
	account Feature,
	sessions APIFeature,
	groups Feature,
	claims Feature,
	passkeys APIFeature,
	devices APIFeature,
	onetime Feature,
	emailv Feature,
	signup Feature,
	apiaccess Feature,
	apikeys Feature,
	recovery APIFeature,
	totp Feature,
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

// notFoundRPC is the stub for unwired surfaces; the prefix stays a
// valid chi pattern that never matches real traffic, and the handler
// answers a Connect-style 404 document.
func notFoundRPC() (string, http.Handler) {
	return "/tango.void.v1.NotFound/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"code":"not_found","message":"procedure not found"}`))
	})
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

// SignupRPCService returns the signup Connect registration; the
// composition root authenticates the mount and the service guards
// its admin procedures per procedure.
func (m *Module) SignupRPCService() (string, http.Handler) {
	if provider, ok := m.signup.(rpcServiceProvider); ok {
		return provider.RPCService()
	}
	return notFoundRPC()
}

// MfaRPCService returns the MFA Connect registration; the service
// mixes the anonymous pending verification with self procedures.
func (m *Module) MfaRPCService() (string, http.Handler) {
	if provider, ok := m.totp.(rpcServiceProvider); ok {
		return provider.RPCService()
	}
	return notFoundRPC()
}

// OneTimeAccessRPCService returns the one-time access Connect
// registration; the admin procedures guard themselves.
func (m *Module) OneTimeAccessRPCService() (string, http.Handler) {
	if provider, ok := m.onetime.(rpcServiceProvider); ok {
		return provider.RPCService()
	}
	return notFoundRPC()
}

// EmailVerificationRPCService returns the email verification Connect
// registration; every procedure is self-service.
func (m *Module) EmailVerificationRPCService() (string, http.Handler) {
	if provider, ok := m.emailv.(rpcServiceProvider); ok {
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

// APIAccessRPCService returns the API registry Connect registration,
// or the not-found stub when the feature is unwired.
func (m *Module) APIAccessRPCService() (string, http.Handler) {
	if provider, ok := m.apiaccess.(rpcServiceProvider); ok && m.apiaccess != nil {
		return provider.RPCService()
	}
	return "", http.NotFoundHandler()
}

// CustomClaimRPCService returns the custom claim Connect registration,
// or the not-found stub when the feature is unwired.
func (m *Module) CustomClaimRPCService() (string, http.Handler) {
	if provider, ok := m.claims.(rpcServiceProvider); ok && m.claims != nil {
		return provider.RPCService()
	}
	return "", http.NotFoundHandler()
}

// DeviceApprovalRPCService returns the device approval Connect
// registration, or the not-found stub when the feature is unwired.
func (m *Module) DeviceApprovalRPCService() (string, http.Handler) {
	if provider, ok := m.devices.(rpcServiceProvider); ok && m.devices != nil {
		return provider.RPCService()
	}
	return "", http.NotFoundHandler()
}

// APIRoutes mounts the user core, then every wired feature's
// endpoints, in construction order. Unwired features stay unmounted.
// Claims and API access serve ConnectRPC exclusively; the REST
// surfaces left are the user core, sessions, passkeys, device login,
// and the retained email-link/protocol routes.
func (m *Module) APIRoutes(r chi.Router, g RouteGroups) {
	m.core.APIRoutes(r, g)
	if m.sessions != nil {
		m.sessions.APIRoutes(r, g)
	}
	if m.passkeys != nil {
		m.passkeys.APIRoutes(r, g)
	}
	if m.devices != nil {
		m.devices.APIRoutes(r, g)
	}
	if m.recovery != nil {
		m.recovery.APIRoutes(r, g)
	}
}
