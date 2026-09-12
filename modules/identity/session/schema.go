// Package session manages sign-in sessions: issue, refresh,
// validate, and revoke.
package session

import (
	"go.jetify.com/typeid"

	"github.com/riipandi/tango/modules/identity"
)

// Typed IDs for the session and token tables: UUIDv7 suffix,
// snake_case prefix matching the singular table name.
type (
	sessionPrefix struct{}

	SessionID = typeid.TypeID[sessionPrefix]

	authTokenPrefix struct{}

	AuthTokenID = typeid.TypeID[authTokenPrefix]

	refreshTokenPrefix struct{}

	RefreshTokenID = typeid.TypeID[refreshTokenPrefix]
)

func (sessionPrefix) Prefix() string      { return "session" }
func (authTokenPrefix) Prefix() string    { return "auth_token" }
func (refreshTokenPrefix) Prefix() string { return "refresh_token" }

// Feature is the wireable session unit.
type Feature struct{}

// New returns the placeholder feature.
func New() Feature { return Feature{} }

// Name implements identity.Feature.
func (Feature) Name() string { return "session" }

var _ identity.Feature = Feature{}
