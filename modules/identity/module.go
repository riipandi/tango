// Package identity owns internal authn/authz: user accounts plus the
// features wired at the composition root. The provider surface for
// other systems lives in modules/federation.
package identity

import (
	"github.com/go-chi/chi/v5"
)

// Feature is one mounted unit inside the identity surface: a feature
// left out of New has no routes.
type Feature interface {
	Name() string
}

// APIFeature mounts endpoints inside the shared /api group.
type APIFeature interface {
	Feature
	APIRoutes(r chi.Router)
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
	}
}

// APIRoutes mounts the user core, then every wired feature's
// endpoints, in construction order. Unwired features stay unmounted.
func (m *Module) APIRoutes(r chi.Router) {
	m.core.APIRoutes(r)
	if m.account != nil {
		m.account.APIRoutes(r)
	}
	if m.sessions != nil {
		m.sessions.APIRoutes(r)
	}
	if m.groups != nil {
		m.groups.APIRoutes(r)
	}
	if m.claims != nil {
		m.claims.APIRoutes(r)
	}
	if m.passkeys != nil {
		m.passkeys.APIRoutes(r)
	}
	if m.devices != nil {
		m.devices.APIRoutes(r)
	}
	if m.onetime != nil {
		m.onetime.APIRoutes(r)
	}
	if m.emailv != nil {
		m.emailv.APIRoutes(r)
	}
	if m.signup != nil {
		m.signup.APIRoutes(r)
	}
	if m.apiaccess != nil {
		m.apiaccess.APIRoutes(r)
	}
	if m.apikeys != nil {
		m.apikeys.APIRoutes(r)
	}
}
