// Package apiaccess manages machine authorization: API definitions
// with scopes and client grants. The federation module evaluates
// machine-token access via a consumer-side adapter, so this package
// never imports it.
package apiaccess

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/go-ozzo/ozzo-validation/v4"
	"go.jetify.com/typeid"

	"github.com/riipandi/tango/pkg/responder"
)

// Typed IDs: only URL-facing IDs carry a TypeID; permission IDs are
// UUIDs surfaced verbatim (upstream DTOs treat them as opaque).
type (
	apiPrefix struct{}

	APIID = typeid.TypeID[apiPrefix]
)

func (apiPrefix) Prefix() string { return "api" }

// API is one resource server definition.
type API struct {
	ID               APIID        `json:"id"`
	Name             string       `json:"name"`
	Resource         string       `json:"resource"`
	AllowCIMDClients bool         `json:"allow_cimd_clients"`
	Permissions      []Permission `json:"permissions"`
	CreatedAt        time.Time    `json:"created_at"`
	UpdatedAt        *time.Time   `json:"updated_at,omitzero"`
}

// Permission is one scope on an API. The wire scope string is
// "<resource>:<key>".
type Permission struct {
	ID                    string    `json:"id"`
	APIID                 string    `json:"-"`
	Key                   string    `json:"key"`
	Name                  string    `json:"name"`
	Description           *string   `json:"description,omitzero"`
	AllowedForCIMDClients bool      `json:"allowed_for_cimd_clients"`
	CreatedAt             time.Time `json:"-"`
}

// CreateParams carries the POST /apis fields.
type CreateParams struct {
	Name     string
	Resource string
}

// Validate enforces the database constraints up front.
func (p *CreateParams) Validate() error {
	p.Name = strings.TrimSpace(p.Name)
	p.Resource = strings.TrimSpace(p.Resource)
	return validation.ValidateStruct(p,
		validation.Field(&p.Name, validation.Required, validation.Length(1, 50)),
		validation.Field(&p.Resource, validation.Required, validation.Length(1, 350)),
	)
}

// UpdateParams patches the display name (upstream apiUpdateDto).
type UpdateParams struct {
	Name string
}

// Validate enforces the name constraints.
func (p *UpdateParams) Validate() error {
	p.Name = strings.TrimSpace(p.Name)
	return validation.ValidateStruct(p,
		validation.Field(&p.Name, validation.Required, validation.Length(1, 50)),
	)
}

// PermissionInput is one permission in the list-replace payload.
type PermissionInput struct {
	Key         string
	Name        string
	Description *string
}

// Validate enforces the column constraints.
func (p PermissionInput) Validate() error {
	return validation.ValidateStruct(&p,
		validation.Field(&p.Key, validation.Required, validation.Length(1, 128)),
		validation.Field(&p.Name, validation.Required, validation.Length(1, 50)),
		validation.Field(&p.Description, validation.Length(0, 200)),
	)
}

// GrantParams is the PUT /apis/{id}/clients/{clientId} payload:
// access flags plus the granted permission IDs per subject.
type GrantParams struct {
	UserDelegatedAccess        bool
	UserDelegatedPermissionIDs []string
	ClientAccess               bool
	ClientPermissionIDs        []string
}

// Grant is one stored grant row with its per-subject permission IDs.
// CIMD access is computed, not stored: an API-level flag plus the
// per-permission allowlist (see SetCIMDAccess).
type Grant struct {
	APIID                      string
	ClientID                   string
	UserDelegatedAccess        bool
	UserDelegatedPermissionIDs []string
	ClientAccess               bool
	ClientPermissionIDs        []string
}

// ListParams narrows and pages the admin listing.
type ListParams struct {
	Query string
	responder.PaginationParams
}

// Errors surfaced by the store.
var (
	ErrNotFound       = errors.New("apiaccess: api not found")
	ErrDuplicate      = errors.New("apiaccess: api resource already exists")
	ErrUnknownClient  = errors.New("apiaccess: unknown oidc client")
	ErrUnknownPerms   = errors.New("apiaccess: unknown permission ids")
	ErrInvalidSubject = errors.New("apiaccess: invalid grant subject")
)

// Store abstracts API persistence. Permissions are list-replaced
// per API; grants are upserted per (api, client) pair.
type Store interface {
	Create(ctx context.Context, params CreateParams) (API, error)
	GetByID(ctx context.Context, id APIID) (API, error)
	Update(ctx context.Context, id APIID, params UpdateParams) (API, error)
	Delete(ctx context.Context, id APIID) error
	List(ctx context.Context, params ListParams) ([]API, int, error)

	SetPermissions(ctx context.Context, id APIID, perms []PermissionInput) ([]Permission, error)

	// Client listing/lookup joins public.oidc_clients; the client
	// ID is the oidc_clients TEXT primary key.
	ListClientsWithAccess(ctx context.Context, id APIID, params ListParams) ([]ClientRef, int, error)
	ListAssignableClients(ctx context.Context, id APIID, params ListParams) ([]ClientRef, int, error)

	GrantFor(ctx context.Context, id APIID, clientID string) (Grant, error)
	UpsertGrant(ctx context.Context, id APIID, clientID string, params GrantParams) (Grant, error)
	DeleteGrant(ctx context.Context, id APIID, clientID string) error
	ListGrantsForClient(ctx context.Context, clientID string) ([]Grant, error)
	ListAssignableAPIs(ctx context.Context, clientID string, params ListParams) ([]API, int, error)

	SetCIMDAccess(ctx context.Context, id APIID, enabled bool, permissionIDs []string) (API, error)

	// AllowedScopesForAudience resolves an RFC 8707 resource to the
	// permission keys one client may use on it, for the OIDC token
	// paths. apiExists false means the resource is unknown. The
	// subject type is a plain string ("user" / "client") to keep
	// this package decoupled from the OIDC surface.
	AllowedScopesForAudience(ctx context.Context, clientID, resource string, subjectType string) (scopes []string, apiExists bool, hasAccess bool, err error)
}

// SubjectType selects which grant side applies: user-delegated
// flows read user grants; client-credentials reads client grants.
// String form so the store method satisfies decoupled interfaces.
type SubjectType string

const (
	SubjectUser   SubjectType = "user"
	SubjectClient SubjectType = "client"
)

// ClientRef is the apiClientDto shape: the OIDC client subset the
// API admin UI renders.
type ClientRef struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ClientType  string `json:"client_type"`
	IsPublic    bool   `json:"is_public"`
	HasLogo     bool   `json:"has_logo"`
	HasDarkLogo bool   `json:"has_dark_logo"`
}
