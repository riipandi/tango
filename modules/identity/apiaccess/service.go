package apiaccess

import (
	"context"

	"github.com/riipandi/tango/internal/kernel"
	"github.com/riipandi/tango/modules/identity"
)

// Service holds the API registry business rules and HTTP surface.
type Service struct {
	store    Store
	recorder identity.Recorder
	guard    kernel.Guard
}

var _ identity.APIFeature = (*Service)(nil)

// ServiceOption configures the API access feature.
type ServiceOption func(*Service)

// WithAdminGuard protects the routes; without it they stay open
// (tests, isolated tooling).
func WithAdminGuard(g kernel.Guard) ServiceOption {
	return func(s *Service) { s.guard = g }
}

// NewService builds the feature on the given store.
func NewService(store Store, recorder identity.Recorder, opts ...ServiceOption) *Service {
	s := &Service{store: store, recorder: recorder}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Name implements identity.Feature.
func (s *Service) Name() string { return "apiaccess" }

func (s *Service) record(action, actor, target string) {
	if s.recorder == nil {
		return
	}
	s.recorder(context.Background(), identity.AuditEvent{Action: action, Actor: actor, Target: target})
}

// Create validates and persists a new API.
func (s *Service) Create(ctx context.Context, params CreateParams) (API, error) {
	if err := params.Validate(); err != nil {
		return API{}, err
	}
	a, err := s.store.Create(ctx, params)
	if err != nil {
		return API{}, err
	}
	s.record("api.created", a.ID.String(), a.ID.String())
	return a, nil
}

// GetByID resolves one API with permissions.
func (s *Service) GetByID(ctx context.Context, id APIID) (API, error) {
	return s.store.GetByID(ctx, id)
}

// List returns matching APIs with pagination.
func (s *Service) List(ctx context.Context, params ListParams) ([]API, int, error) {
	return s.store.List(ctx, params)
}

// Update patches an API.
func (s *Service) Update(ctx context.Context, id APIID, params UpdateParams) (API, error) {
	if err := params.Validate(); err != nil {
		return API{}, err
	}
	a, err := s.store.Update(ctx, id, params)
	if err != nil {
		return API{}, err
	}
	s.record("api.updated", a.ID.String(), a.ID.String())
	return a, nil
}

// Delete removes an API; grants and permissions cascade.
func (s *Service) Delete(ctx context.Context, id APIID) error {
	if err := s.store.Delete(ctx, id); err != nil {
		return err
	}
	s.record("api.deleted", id.String(), id.String())
	return nil
}

// SetPermissions list-replaces the API's permission set.
func (s *Service) SetPermissions(ctx context.Context, id APIID, perms []PermissionInput) ([]Permission, error) {
	for i := range perms {
		if err := perms[i].Validate(); err != nil {
			return nil, err
		}
	}
	out, err := s.store.SetPermissions(ctx, id, perms)
	if err != nil {
		return nil, err
	}
	s.record("api.permissions_updated", id.String(), id.String())
	return out, nil
}

// ClientsWithAccess pages the clients holding a grant.
func (s *Service) ClientsWithAccess(ctx context.Context, id APIID, params ListParams) ([]ClientRef, int, error) {
	return s.store.ListClientsWithAccess(ctx, id, params)
}

// AssignableClients pages the clients without a grant.
func (s *Service) AssignableClients(ctx context.Context, id APIID, params ListParams) ([]ClientRef, int, error) {
	return s.store.ListAssignableClients(ctx, id, params)
}

// GrantFor resolves one client's grant on an API.
func (s *Service) GrantFor(ctx context.Context, id APIID, clientID string) (Grant, error) {
	return s.store.GrantFor(ctx, id, clientID)
}

// UpsertGrant writes one client's grant on an API.
func (s *Service) UpsertGrant(ctx context.Context, id APIID, clientID string, params GrantParams) (Grant, error) {
	g, err := s.store.UpsertGrant(ctx, id, clientID, params)
	if err != nil {
		return Grant{}, err
	}
	s.record("api.client_grant_updated", clientID, id.String())
	return g, nil
}

// DeleteGrant removes one client's grant on an API.
func (s *Service) DeleteGrant(ctx context.Context, id APIID, clientID string) error {
	if err := s.store.DeleteGrant(ctx, id, clientID); err != nil {
		return err
	}
	s.record("api.client_grant_deleted", clientID, id.String())
	return nil
}

// GrantsForClient lists every grant held by one client.
func (s *Service) GrantsForClient(ctx context.Context, clientID string) ([]Grant, error) {
	return s.store.ListGrantsForClient(ctx, clientID)
}

// AssignableAPIs pages the APIs a client may still be granted.
func (s *Service) AssignableAPIs(ctx context.Context, clientID string, params ListParams) ([]API, int, error) {
	return s.store.ListAssignableAPIs(ctx, clientID, params)
}

// SetCIMDAccess toggles the API-level flag plus allowlist.
func (s *Service) SetCIMDAccess(ctx context.Context, id APIID, enabled bool, permissionIDs []string) (API, error) {
	a, err := s.store.SetCIMDAccess(ctx, id, enabled, permissionIDs)
	if err != nil {
		return API{}, err
	}
	s.record("api.cimd_access_updated", id.String(), id.String())
	return a, nil
}

// AllowedScopesForAudience resolves an RFC 8707 resource to the
// permission keys one client may use on it. Consumed by the OIDC
// token paths via the provider adapter in the registry.
func (s *Service) AllowedScopesForAudience(ctx context.Context, clientID, resource string, subjectType string) ([]string, bool, bool, error) {
	return s.store.AllowedScopesForAudience(ctx, clientID, resource, subjectType)
}
