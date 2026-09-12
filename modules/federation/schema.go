// Package federation is the optional identity provider surface this
// application exposes to other systems (OIDC/OAuth 2.0, SCIM,
// discovery). It has no mandatory core: it is exactly the features
// the composition root selects. Remove its registration line to
// build a pure internal-identity binary.
package federation

import (
	"github.com/go-chi/chi/v5"
	"go.jetify.com/typeid"

	"github.com/riipandi/tango/internal/kernel"
)

// Typed IDs for the federation tables: a UUIDv7 suffix plus a
// snake_case prefix matching the singular table name (lowercase
// letters and underscores only, per the TypeID spec).
type (
	jwkPrefix struct{}

	// JWKID identifies a jwks row.
	JWKID = typeid.TypeID[jwkPrefix]

	oidcClientPrefix struct{}

	// OIDCClientID identifies an oidc_clients row (stored as the full
	// TypeID string, matching the Pocket ID opaque client id).
	OIDCClientID = typeid.TypeID[oidcClientPrefix]

	customClaimPrefix struct{}

	// CustomClaimID identifies a custom_claims row.
	CustomClaimID = typeid.TypeID[customClaimPrefix]

	authorizationCodePrefix struct{}

	// AuthorizationCodeID identifies an oidc_authorization_codes row.
	AuthorizationCodeID = typeid.TypeID[authorizationCodePrefix]

	oidcRefreshTokenPrefix struct{}

	// OIDCRefreshTokenID identifies an oidc_refresh_tokens row.
	OIDCRefreshTokenID = typeid.TypeID[oidcRefreshTokenPrefix]

	deviceCodePrefix struct{}

	// DeviceCodeID identifies an oidc_device_codes row.
	DeviceCodeID = typeid.TypeID[deviceCodePrefix]

	oauthSessionPrefix struct{}

	// OAuth2SessionID identifies an oauth2_sessions row.
	OAuth2SessionID = typeid.TypeID[oauthSessionPrefix]

	oauthJtiPrefix struct{}

	// OAuth2JTIID identifies an oauth2_jtis row.
	OAuth2JTIID = typeid.TypeID[oauthJtiPrefix]

	interactionSessionPrefix struct{}

	// InteractionSessionID identifies an interaction_sessions row.
	InteractionSessionID = typeid.TypeID[interactionSessionPrefix]

	scimServiceProviderPrefix struct{}

	// SCIMServiceProviderID identifies a scim_service_providers row.
	SCIMServiceProviderID = typeid.TypeID[scimServiceProviderPrefix]
)

func (jwkPrefix) Prefix() string                 { return "jwk" }
func (oidcClientPrefix) Prefix() string          { return "oidc_client" }
func (customClaimPrefix) Prefix() string         { return "custom_claim" }
func (authorizationCodePrefix) Prefix() string   { return "authorization_code" }
func (oidcRefreshTokenPrefix) Prefix() string    { return "oidc_refresh_token" }
func (deviceCodePrefix) Prefix() string          { return "device_code" }
func (oauthSessionPrefix) Prefix() string        { return "oauth_session" }
func (oauthJtiPrefix) Prefix() string            { return "oauth_jti" }
func (interactionSessionPrefix) Prefix() string  { return "interaction_session" }
func (scimServiceProviderPrefix) Prefix() string { return "scim_service_provider" }

// Feature is one selectable unit chosen at the composition root. A
// feature left out of New does not exist: no routes, no storage, no
// lifecycle.
type Feature interface {
	Name() string
}

// APIFeature mounts endpoints inside the shared /api group.
type APIFeature interface {
	Feature
	APIRoutes(r chi.Router)
}

// StartableFeature holds resources with a lifecycle: started in
// selection order, stopped in reverse.
type StartableFeature interface {
	Feature
	kernel.Startable
}

// RootRoutableFeature mounts routes on the root router, outside /api
// — e.g. the OIDC /authorize endpoint.
type RootRoutableFeature interface {
	Feature
	Routes(r chi.Router)
}
