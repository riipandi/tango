// Package scimsync is the SCIM 2.0 outbound provisioning surface:
// service-provider rows bind an OIDC client to a remote SCIM
// endpoint + bearer token; a sync pushes the client's allowed users
// and groups to the remote provider. Mirrors upstream v2.14
// (scim_service_providers table, /api/scim/service-provider*).
package scimsync

import (
	"errors"
	"strings"
	"time"

	"go.jetify.com/typeid"
)

// Typed IDs for the SCIM tables: UUIDv7 suffix, snake_case prefix
// matching the singular table name.
type (
	scimServiceProviderPrefix struct{}

	SCIMServiceProviderID = typeid.TypeID[scimServiceProviderPrefix]
)

func (scimServiceProviderPrefix) Prefix() string { return "scim_service_provider" }

// errors mapped to HTTP by the transport layer.
var (
	ErrNotFound      = errors.New("scimsync: service provider not found")
	ErrUnknownClient = errors.New("scimsync: OIDC client not found")
	ErrDuplicate     = errors.New("scimsync: client already has a SCIM service provider")
	ErrInvalidToken  = errors.New("scimsync: invalid bearer token")
	ErrSyncFailed    = errors.New("scimsync: sync failed")
)

// ServiceProvider is one configured remote SCIM provider.
type ServiceProvider struct {
	ID           SCIMServiceProviderID `json:"id"`
	Endpoint     string                `json:"endpoint"`
	Token        string                `json:"token"` // shown once on write; empty on read
	OIDCClientID string                `json:"oidc_client_id"`
	LastSyncedAt *time.Time            `json:"last_synced_at,omitzero"`
	CreatedAt    time.Time             `json:"created_at"`
}

// UpsertParams carries the fields a caller supplies.
type UpsertParams struct {
	Endpoint     string
	Token        string
	OIDCClientID string
}

// Validate enforces upstream DTO rules: URL endpoint, non-empty client.
func (p UpsertParams) Validate() error {
	p.Endpoint = strings.TrimSpace(p.Endpoint)
	p.OIDCClientID = strings.TrimSpace(p.OIDCClientID)
	if !strings.HasPrefix(p.Endpoint, "http://") && !strings.HasPrefix(p.Endpoint, "https://") {
		return errors.New("endpoint must be an absolute http(s) URL")
	}
	if p.OIDCClientID == "" {
		return errors.New("oidc client id is required")
	}
	return nil
}
