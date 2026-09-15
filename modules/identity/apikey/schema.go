// Package apikey issues machine credentials sent via the X-API-KEY
// header, with expiry and last-used tracking. Keys are the user's
// own API keys; resource-server scopes live in the apiaccess module.
package apikey

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

// Typed IDs for the api key tables: UUIDv7 suffix, snake_case prefix
// matching the singular table name.
type (
	apiKeyPrefix struct{}

	APIKeyID = typeid.TypeID[apiKeyPrefix]
)

func (apiKeyPrefix) Prefix() string { return "api_key" }

// APIKey is one machine credential row.
type APIKey struct {
	ID                    APIKeyID   `json:"id"`
	Name                  string     `json:"name"`
	Description           *string    `json:"description,omitzero"`
	ExpiresAt             time.Time  `json:"expires_at"`
	CreatedAt             time.Time  `json:"created_at"`
	LastUsedAt            *time.Time `json:"last_used_at,omitzero"`
	ExpirationEmailSentAt *time.Time `json:"-"`
}

// CreateParams carries the POST /api-keys fields.
type CreateParams struct {
	Name        string
	Description *string
	ExpiresAt   time.Time
}

// Validate checks the name and expiry.
func (p *CreateParams) Validate() error {
	p.Name = strings.TrimSpace(p.Name)
	return validation.ValidateStruct(p,
		validation.Field(&p.Name, validation.Required, validation.Length(3, 50)),
		validation.Field(&p.ExpiresAt, validation.Required, validation.Min(time.Now()).Exclusive()),
	)
}

// RenewParams carries the POST /api-keys/{id}/renew fields.
type RenewParams struct {
	ExpiresAt time.Time
}

// Validate enforces the future-expiry constraint.
func (p *RenewParams) Validate() error {
	return validation.ValidateStruct(p,
		validation.Field(&p.ExpiresAt, validation.Required, validation.Min(time.Now()).Exclusive()),
	)
}

// ListParams narrows and pages the listing; keys are scoped to the
// authenticated user.
type ListParams struct {
	responder.PaginationParams
}

// Errors surfaced by the store and service; the handler maps them
// to statuses.
var (
	ErrNotFound     = errors.New("apikey: key not found")
	ErrDuplicate    = errors.New("apikey: key name already exists")
	ErrNotExpired   = errors.New("apikey: key is not expired yet")
	ErrInvalidCreds = errors.New("apikey: key is invalid or expired")
)

// Store abstracts API key persistence.
type Store interface {
	Create(ctx context.Context, userID string, keyHash string, params CreateParams) (APIKey, error)
	ListForUser(ctx context.Context, userID string, params ListParams) ([]APIKey, int, error)
	Revoke(ctx context.Context, userID string, id APIKeyID) error
	// Renew rotates the token hash and expiry; allowed only when
	// the key is already expired.
	Renew(ctx context.Context, userID string, id APIKeyID, keyHash string, expiresAt time.Time) (APIKey, error)
	// ValidByHash resolves a hash to the owning user, atomically
	// bumping last_used_at (UPDATE ... RETURNING).
	ValidByHash(ctx context.Context, keyHash string) (APIKey, user.User, error)
}
