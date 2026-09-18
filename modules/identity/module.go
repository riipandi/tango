// Package identity owns internal authn/authz: user accounts plus the
// features wired at the composition root. The provider surface for
// other systems lives in modules/federation.
package identity

import (
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
	account   APIFeature
	sessions  APIFeature
	groups    APIFeature
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
	account APIFeature,
	sessions APIFeature,
	groups APIFeature,
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

// APIRoutes mounts the user core, then every wired feature's
// endpoints, in construction order. Unwired features stay unmounted.
func (m *Module) APIRoutes(r chi.Router, g RouteGroups) {
	m.core.APIRoutes(r, g)
	if m.account != nil {
		m.account.APIRoutes(r, g)
	}
	if m.sessions != nil {
		m.sessions.APIRoutes(r, g)
	}
	if m.groups != nil {
		m.groups.APIRoutes(r, g)
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
