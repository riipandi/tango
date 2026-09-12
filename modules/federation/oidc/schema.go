// Package oidc is the OpenID Connect / OAuth 2.0 provider surface:
// /authorize + PAR (root router), /api/oidc/token, userinfo,
// introspect, revoke, end_session. One flat package, file-per-area;
// split into subpackages only if a coherent boundary emerges.
//
// Files: schema.go (contracts), client.go (relying-party clients),
// token.go (codes, tokens, JWKS), device.go (RFC 8628),
// store_*.go (persistence).
//
// Authentication happens in identity features (passkeys, sessions);
// this package consumes identity contracts via consumer-side adapters.
package oidc

import (
	"go.jetify.com/typeid"

	"github.com/riipandi/tango/modules/federation"
)

// Typed IDs for the OIDC/OAuth 2.0 tables: UUIDv7 suffix, snake_case
// prefix matching the singular table name (lowercase letters and
// underscores only, per the TypeID spec).
type (
	oidcClientPrefix struct{}

	OIDCClientID = typeid.TypeID[oidcClientPrefix]

	authorizationCodePrefix struct{}

	AuthorizationCodeID = typeid.TypeID[authorizationCodePrefix]

	oidcRefreshTokenPrefix struct{}

	OIDCRefreshTokenID = typeid.TypeID[oidcRefreshTokenPrefix]

	deviceCodePrefix struct{}

	DeviceCodeID = typeid.TypeID[deviceCodePrefix]

	oauthSessionPrefix struct{}

	OAuth2SessionID = typeid.TypeID[oauthSessionPrefix]

	oauthJtiPrefix struct{}

	OAuth2JTIID = typeid.TypeID[oauthJtiPrefix]

	interactionSessionPrefix struct{}

	InteractionSessionID = typeid.TypeID[interactionSessionPrefix]
)

func (oidcClientPrefix) Prefix() string         { return "oidc_client" }
func (authorizationCodePrefix) Prefix() string  { return "authorization_code" }
func (oidcRefreshTokenPrefix) Prefix() string   { return "oidc_refresh_token" }
func (deviceCodePrefix) Prefix() string         { return "device_code" }
func (oauthSessionPrefix) Prefix() string       { return "oauth_session" }
func (oauthJtiPrefix) Prefix() string           { return "oauth_jti" }
func (interactionSessionPrefix) Prefix() string { return "interaction_session" }

// Feature is the wireable oidc unit.
type Feature struct{}

// New returns the placeholder feature. /authorize mounts at the
// root router via the federation RootRoutableFeature capability.
func New() Feature { return Feature{} }

// Name implements federation.Feature.
func (Feature) Name() string { return "oidc" }

var _ federation.Feature = Feature{}
