package wellknown

import (
	"net/http"

	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/pkg/responder"
)

func (m *Module) jwks(w http.ResponseWriter, r *http.Request) {
	// TODO: serve real JWKS from the configured signing keys.
	responder.WriteJSON(w, http.StatusOK, map[string]any{
		"message": "Not yet implemented",
	})
}

func (m *Module) version(w http.ResponseWriter, r *http.Request) {
	responder.WriteJSON(w, http.StatusOK, map[string]string{
		"name":     config.AppName,
		"version":  config.AppVersion,
		"platform": config.Platform,
		"build":    config.BuildDate,
		"hash":     config.BuildHash,
	})
}

func (m *Module) openIDConfiguration(w http.ResponseWriter, r *http.Request) {
	// TODO: full OIDC discovery document once the identity module
	// exposes its token endpoints.
	responder.WriteJSON(w, http.StatusOK, map[string]any{
		"message": "Not yet implemented",
	})
}
