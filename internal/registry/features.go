package registry

import (
	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/modules/identity/apiaccess"
	"github.com/riipandi/tango/modules/identity/apikey"
	"github.com/riipandi/tango/modules/identity/ldapsync"
	"github.com/riipandi/tango/modules/identity/password"
	"github.com/riipandi/tango/modules/identity/session"
	"github.com/riipandi/tango/modules/identity/webauthn"

	"github.com/riipandi/tango/modules/federation"
	"github.com/riipandi/tango/modules/federation/discovery"
	"github.com/riipandi/tango/modules/federation/oidc"
	"github.com/riipandi/tango/modules/federation/scimsync"
)

// Identity feature selectors, one line each in New. Placeholders
// until implemented: no routes, no storage.
func withSession(deps Deps) identity.Feature   { return session.New() }
func withPassword(deps Deps) identity.Feature  { return password.New() }
func withWebAuthn(deps Deps) identity.Feature  { return webauthn.New() }
func withAPIKeys(deps Deps) identity.Feature   { return apikey.New() }
func withAPIAccess(deps Deps) identity.Feature { return apiaccess.New() }
func withLDAPSync(deps Deps) identity.Feature  { return ldapsync.New() }

// Identity-provider feature selectors, one line each in
// federation.New. Placeholders until implemented: no routes,
// no storage. The federation module is optional: removing its
// registration (and this file's federation imports) yields a
// pure internal-identity binary.
func withOIDC(deps Deps) federation.Feature      { return oidc.New() }
func withSCIMSync(deps Deps) federation.Feature  { return scimsync.New() }
func withDiscovery(deps Deps) federation.Feature { return discovery.New() }
