// Package customclaim manages per-user and per-group key/value
// claims injected into OIDC tokens. The federation oidc package
// consumes them at token time via a consumer-side adapter.
package customclaim

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/go-ozzo/ozzo-validation/v4"
	"go.jetify.com/typeid"

	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/modules/identity/usergroup"
)

// Typed IDs for the custom claim tables: UUIDv7 suffix, snake_case
// prefix matching the singular table name.
type (
	customClaimPrefix struct{}

	CustomClaimID = typeid.TypeID[customClaimPrefix]
)

func (customClaimPrefix) Prefix() string { return "custom_claim" }

// CustomClaim is one key/value claim attached to a user or a group
// (exactly one owner, enforced by the database CHECK). Keys are
// case-sensitive; uniqueness is (key, user, group) — Postgres treats
// NULLs as distinct in the UNIQUE constraint, so the service checks
// duplicates up front.
type CustomClaim struct {
	ID    CustomClaimID `json:"id"`
	Key   string        `json:"key"`
	Value string        `json:"value"`

	UserID      *string `json:"user_id,omitzero"`
	UserGroupID *string `json:"user_group_id,omitzero"`

	CreatedAt time.Time `json:"created_at"`
}

// UpsertParams carries the claim fields for one owner scope.
type UpsertParams struct {
	Key         string
	Value       string
	UserID      *string
	UserGroupID *string
}

// Validate normalizes and enforces the claim shape.
func (p *UpsertParams) Validate() error {
	p.Key = strings.TrimSpace(p.Key)
	p.Value = strings.TrimSpace(p.Value)
	return validation.ValidateStruct(p,
		validation.Field(&p.Key, validation.Required, validation.Length(1, 100)),
		validation.Field(&p.Value, validation.Required),
	)
}

// Errors surfaced by the store.
var (
	ErrNotFound  = errors.New("custom claim not found")
	ErrDuplicate = errors.New("custom claim already exists for this owner")
)

// Store persists custom claims scoped to users or groups.
type Store interface {
	Create(ctx context.Context, params UpsertParams) (CustomClaim, error)
	ExistsForOwner(ctx context.Context, params UpsertParams) (bool, error)
	Update(ctx context.Context, id CustomClaimID, value string) (CustomClaim, error)
	Delete(ctx context.Context, id CustomClaimID) error
	ListByUser(ctx context.Context, userID user.UserID) ([]CustomClaim, error)
	ListByGroup(ctx context.Context, groupID usergroup.UserGroupID) ([]CustomClaim, error)
	SuggestedKeys(ctx context.Context) ([]string, error)
}

// Feature is the wireable custom claim unit.
type Feature struct{}

// New returns the placeholder feature.
func New() Feature { return Feature{} }

// Name implements identity.Feature.
func (Feature) Name() string { return "customclaim" }
