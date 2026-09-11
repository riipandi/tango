package registry

import (
	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/modules/identity/apiaccess"
	"github.com/riipandi/tango/modules/identity/apikey"
	"github.com/riipandi/tango/modules/identity/oidc"
	"github.com/riipandi/tango/modules/identity/password"
	"github.com/riipandi/tango/modules/identity/session"
	"github.com/riipandi/tango/modules/identity/webauthn"
)

// Identity feature selectors. Each helper constructs one authn/authz
// feature; adding or removing a feature from the composition is a
// single line in registry.New. Until implemented, constructors return
// inert placeholder features (no routes, no storage).
func withSession(deps Deps) identity.Feature   { return session.New() }
func withWebAuthn(deps Deps) identity.Feature  { return webauthn.New() }
func withPassword(deps Deps) identity.Feature  { return password.New() }
func withAPIKeys(deps Deps) identity.Feature   { return apikey.New() }
func withAPIAccess(deps Deps) identity.Feature { return apiaccess.New() }
func withOIDC(deps Deps) identity.Feature      { return oidc.New() }
