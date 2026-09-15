package customclaim

import (
	"context"
	"errors"
	"strconv"

	"github.com/riipandi/tango/internal/kernel"
	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/modules/identity/usergroup"
)

// Service holds the claim business rules and HTTP surface.
type Service struct {
	store    Store
	recorder identity.Recorder
	guard    kernel.Guard
}

// ServiceOption configures the claim feature.
type ServiceOption func(*Service)

// WithAdminGuard protects the routes; without it they stay open
// (tests, isolated tooling).
func WithAdminGuard(g kernel.Guard) ServiceOption {
	return func(s *Service) { s.guard = g }
}

// NewService builds the claim feature on the given store.
func NewService(store Store, recorder identity.Recorder, opts ...ServiceOption) *Service {
	s := &Service{store: store, recorder: recorder}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Name names the feature for logs.
func (s *Service) Name() string { return "customclaim" }

// CreateForUser attaches a claim to one user.
func (s *Service) CreateForUser(ctx context.Context, userID user.UserID, params UpsertParams) (CustomClaim, error) {
	owner := userID.UUID()
	params.UserID = &owner
	return s.create(ctx, params)
}

// CreateForGroup attaches a claim to one group.
func (s *Service) CreateForGroup(ctx context.Context, groupID usergroup.UserGroupID, params UpsertParams) (CustomClaim, error) {
	owner := groupID.UUID()
	params.UserGroupID = &owner
	return s.create(ctx, params)
}

// create enforces validation plus the duplicate check (the DB UNIQUE
// constraint treats NULLs as distinct, so it cannot catch this).
func (s *Service) create(ctx context.Context, params UpsertParams) (CustomClaim, error) {
	if err := params.Validate(); err != nil {
		return CustomClaim{}, err
	}

	dup, err := s.store.ExistsForOwner(ctx, params)
	if err != nil {
		return CustomClaim{}, err
	}
	if dup {
		return CustomClaim{}, ErrDuplicate
	}

	claim, err := s.store.Create(ctx, params)
	if err != nil {
		return CustomClaim{}, err
	}
	if s.recorder != nil {
		s.recorder(ctx, identity.AuditEvent{Action: "custom_claim.created", Actor: params.Key, Target: claim.ID.String()})
	}
	return claim, nil
}

// UpdateValue replaces one claim's value.
func (s *Service) UpdateValue(ctx context.Context, id CustomClaimID, value string) (CustomClaim, error) {
	if value == "" {
		return CustomClaim{}, errors.New("custom claim: value is required")
	}
	claim, err := s.store.Update(ctx, id, value)
	if err != nil {
		return CustomClaim{}, err
	}
	if s.recorder != nil {
		s.recorder(ctx, identity.AuditEvent{Action: "custom_claim.updated", Actor: claim.Key, Target: claim.ID.String()})
	}
	return claim, nil
}

// Delete removes one claim.
func (s *Service) Delete(ctx context.Context, id CustomClaimID) error {
	if err := s.store.Delete(ctx, id); err != nil {
		return err
	}
	if s.recorder != nil {
		s.recorder(ctx, identity.AuditEvent{Action: "custom_claim.deleted", Target: id.String()})
	}
	return nil
}

// ListByUser returns one user's claims.
func (s *Service) ListByUser(ctx context.Context, userID user.UserID) ([]CustomClaim, error) {
	return s.store.ListByUser(ctx, userID)
}

// ReplaceForUser swaps a user's whole claim set. Owner scoping is
// applied to every item; duplicate
// keys inside one call are rejected before touching the database.
func (s *Service) ReplaceForUser(ctx context.Context, userID user.UserID, params []UpsertParams) ([]CustomClaim, error) {
	scoped, err := s.scopeParams(params, func(p UpsertParams) UpsertParams {
		owner := userID.UUID()
		p.UserID = &owner
		return p
	})
	if err != nil {
		return nil, err
	}

	replaced, err := s.store.ReplaceForUser(ctx, userID, scoped)
	if err != nil {
		return nil, err
	}
	s.record(ctx, "custom_claim.replaced", userID.String(), strconv.Itoa(len(replaced)))
	return replaced, nil
}

// ReplaceForGroup swaps a group's whole claim set.
func (s *Service) ReplaceForGroup(ctx context.Context, groupID usergroup.UserGroupID, params []UpsertParams) ([]CustomClaim, error) {
	scoped, err := s.scopeParams(params, func(p UpsertParams) UpsertParams {
		owner := groupID.UUID()
		p.UserGroupID = &owner
		return p
	})
	if err != nil {
		return nil, err
	}
	return s.store.ReplaceForGroup(ctx, groupID, scoped)
}

// scopeParams stamps the owner onto every item and rejects
// duplicate keys (UNIQUE treats NULLs as distinct — the dup check
// must be explicit).
func (s *Service) scopeParams(params []UpsertParams, scope func(UpsertParams) UpsertParams) ([]UpsertParams, error) {
	seen := map[string]bool{}
	out := make([]UpsertParams, 0, len(params))
	for _, param := range params {
		if seen[param.Key] {
			return nil, ErrDuplicate
		}
		seen[param.Key] = true
		out = append(out, scope(param))
	}
	return out, nil
}

// record emits an audit event through the adapter.
func (s *Service) record(ctx context.Context, action, target, detail string) {
	if s.recorder != nil {
		s.recorder(ctx, identity.AuditEvent{Action: action, Actor: detail, Target: target})
	}
}

// ListByGroup returns one group's claims.
func (s *Service) ListByGroup(ctx context.Context, groupID usergroup.UserGroupID) ([]CustomClaim, error) {
	return s.store.ListByGroup(ctx, groupID)
}

// SuggestedKeys lists distinct claim keys already in use.
func (s *Service) SuggestedKeys(ctx context.Context) ([]string, error) {
	return s.store.SuggestedKeys(ctx)
}
