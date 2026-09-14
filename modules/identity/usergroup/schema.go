// Package usergroup groups users so OIDC client access can be
// granted in bulk. Membership references identity root users.
package usergroup

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/go-ozzo/ozzo-validation/v4"
	"go.jetify.com/typeid"

	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/pkg/responder"
)

// Typed IDs for the user group tables: UUIDv7 suffix, snake_case
// prefix matching the singular table name.
type (
	userGroupPrefix struct{}

	UserGroupID = typeid.TypeID[userGroupPrefix]
)

func (userGroupPrefix) Prefix() string { return "user_group" }

// UserGroup is one grantable group of users.
type UserGroup struct {
	ID          UserGroupID `json:"id"`
	Name        string      `json:"name"`
	DisplayName string      `json:"display_name"`
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   *time.Time  `json:"updated_at,omitzero"`
}

// CreateParams carries the fields a caller supplies.
type CreateParams struct {
	Name        string
	DisplayName string
}

// Validate normalizes and enforces the database constraints up front.
func (p *CreateParams) Validate() error {
	p.Name = strings.TrimSpace(p.Name)
	p.DisplayName = strings.TrimSpace(p.DisplayName)
	return validation.ValidateStruct(p,
		validation.Field(&p.Name, validation.Required, validation.Match(user.UsernamePattern)),
		validation.Field(&p.DisplayName, validation.Required),
	)
}

// UpdateParams patches fields; nil keeps the column.
type UpdateParams struct {
	Name        *string
	DisplayName *string
}

// Store abstracts group persistence. SetMembers replaces the whole
// membership of one group; ReplaceGroupsForUser is the inverse
// (upstream PUT /users/{id}/user-groups).
type Store interface {
	Create(ctx context.Context, params CreateParams) (UserGroup, error)
	GetByID(ctx context.Context, id UserGroupID) (UserGroup, error)
	Update(ctx context.Context, id UserGroupID, params UpdateParams) (UserGroup, error)
	Delete(ctx context.Context, id UserGroupID) error
	List(ctx context.Context, params ListParams) ([]UserGroup, int, error)
	SetMembers(ctx context.Context, id UserGroupID, memberIDs []user.UserID) error
	MemberIDs(ctx context.Context, id UserGroupID) ([]user.UserID, error)
	GroupIDsForUser(ctx context.Context, id user.UserID) ([]UserGroup, error)
	ReplaceGroupsForUser(ctx context.Context, id user.UserID, groupIDs []UserGroupID) error
	// ReplaceAllowedClients swaps the group-side OIDC client
	// allowlist (upstream PUT /user-groups/{id}/allowed-oidc-clients).
	// Client IDs are opaque typeid strings here — identity never
	// imports federation; the store's existence check + FK enforce
	// validity.
	ReplaceAllowedClients(ctx context.Context, id UserGroupID, clientIDs []string) error
	AllowedClientIDs(ctx context.Context, id UserGroupID) ([]string, error)
}

// ListParams narrows and pages the admin listing.
type ListParams struct {
	Query string
	responder.PaginationParams
}

// Errors surfaced by the store.
var (
	ErrNotFound   = errors.New("user group not found")
	ErrDuplicate  = errors.New("user group name already exists")
	ErrInvalidIDs = errors.New("user group: unknown member ids")
)
