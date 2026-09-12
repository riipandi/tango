// Package usergroup groups users so OIDC client access can be
// granted in bulk. Membership references identity root users.
package usergroup

import "go.jetify.com/typeid"

// Typed IDs for the user group tables: UUIDv7 suffix, snake_case
// prefix matching the singular table name.
type (
	userGroupPrefix struct{}

	UserGroupID = typeid.TypeID[userGroupPrefix]
)

func (userGroupPrefix) Prefix() string { return "user_group" }
