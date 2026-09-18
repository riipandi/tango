package discovery

// handler.go is the HTTP surface of the discovery feature: bare
// documents (no responder envelope) with the JWKS cache policy.

import (
	"net/http"

	"github.com/riipandi/tango/pkg/responder"
)

// jwks publishes the public halves of every currently published
// signing key, including retired keys inside the rotation overlap.
// The set is built from public PEMs only, so no private material
// can leak; no envelope — clients expect a bare JWKS document.
func (f Feature) jwks(w http.ResponseWriter, r *http.Request) {
	set, err := f.provider.VerifyKeySet(r.Context())
	if err != nil {
		responder.Fail(w, r, http.StatusInternalServerError, "failed to load signing keys")
		return
	}

	w.Header().Set("Cache-Control", jwksCacheControl)
	responder.WriteJSON(w, http.StatusOK, set)
}

// openIDConfiguration serves the OIDC discovery document.
func (f Feature) openIDConfiguration(w http.ResponseWriter, r *http.Request) {
	responder.WriteJSON(w, http.StatusOK, newDiscoveryDocument(f.issuer))
}

// oauthServerMetadata serves the RFC 8414 authorization-server
// metadata — the same capability set under the OAuth 2.0 name.
func (f Feature) oauthServerMetadata(w http.ResponseWriter, r *http.Request) {
	responder.WriteJSON(w, http.StatusOK, newDiscoveryDocument(f.issuer))
}
