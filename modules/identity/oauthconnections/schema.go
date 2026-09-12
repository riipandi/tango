// Package oauthconnections will own linked external OAuth providers
// ("sign in with Google/GitHub") for local accounts.
package oauthconnections

import "go.jetify.com/typeid"

// Typed IDs for the oauth connections tables: UUIDv7 suffix,
// snake_case prefix matching the singular table name.
type (
	oauthConnectionPrefix struct{}

	OAuthConnectionID = typeid.TypeID[oauthConnectionPrefix]
)

func (oauthConnectionPrefix) Prefix() string { return "oauth_connection" }
