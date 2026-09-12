package registry

import (
	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/modules/identity/apiaccess"
	"github.com/riipandi/tango/modules/identity/apikey"
	"github.com/riipandi/tango/modules/identity/ldapsync"
	"github.com/riipandi/tango/modules/identity/oidc"
	"github.com/riipandi/tango/modules/identity/password"
	"github.com/riipandi/tango/modules/identity/scimsync"
	"github.com/riipandi/tango/modules/identity/session"
	"github.com/riipandi/tango/modules/identity/webauthn"
)

// Identity feature selectors, one line each in New. Placeholders
// until implemented: no routes, no storage.
func withSession(deps Deps) identity.Feature   { return session.New() }
func withWebAuthn(deps Deps) identity.Feature  { return webauthn.New() }
func withPassword(deps Deps) identity.Feature  { return password.New() }
func withAPIKeys(deps Deps) identity.Feature   { return apikey.New() }
func withAPIAccess(deps Deps) identity.Feature { return apiaccess.New() }
func withOIDC(deps Deps) identity.Feature      { return oidc.New() }
func withLDAPSync(deps Deps) identity.Feature  { return ldapsync.New() }
func withSCIMSync(deps Deps) identity.Feature  { return scimsync.New() }
