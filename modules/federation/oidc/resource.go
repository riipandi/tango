package oidc

import (
	"context"
	"slices"
	"strings"
)

// Subject types mirror the apiaccess grant sides; plain strings
// keep this package free of the identity tree.
const (
	SubjectUser   = "user"
	SubjectClient = "client"
)

// resolveResource maps an RFC 8707 resource (may be empty) to the
// audience stamped on issued tokens plus the subset of requested
// scopes that may be granted. An empty resource is a plain login
// token bound to the requesting client and yields only identity
// scopes; a resource resolves through the API access provider.
// Returns OIDC error names for the token/authorize error paths.
func (s *Service) resolveResource(ctx context.Context, clientID, resource, requestedScope, subjectType string) (audience string, granted []string, errName string) {
	requested := strings.Fields(requestedScope)
	if resource == "" {
		// Plain login: identity scopes only, audience = client.
		audience = clientID
		for _, scope := range requested {
			if isStandardScope(scope) {
				granted = append(granted, scope)
			}
		}
		return audience, granted, ""
	}

	if s.apiAccess == nil {
		return "", nil, "invalid_target"
	}
	// Trailing-slash variants resolve to the same canonical resource.
	resource = strings.TrimRight(resource, "/")

	allowed, apiExists, hasAccess, err := s.apiAccess.AllowedScopesForAudience(ctx, clientID, resource, subjectType)
	if err != nil || !apiExists {
		return "", nil, "invalid_target"
	}
	if !hasAccess {
		return "", nil, "access_denied"
	}
	audience = resource

	grantable := map[string]bool{}
	for _, scope := range requested {
		if isStandardScope(scope) {
			grantable[scope] = true
		}
	}
	for _, scope := range allowed {
		grantable[scope] = true
	}

	// A client-credentials request without explicit scopes gets
	// everything the client is granted for the API.
	if subjectType == SubjectClient && len(requested) == 0 {
		return audience, allowed, ""
	}

	for _, scope := range requested {
		if !grantable[scope] {
			return "", nil, "invalid_scope"
		}
		if !contains(granted, scope) {
			granted = append(granted, scope)
		}
	}
	return audience, granted, ""
}

// isStandardScope reports whether the scope is an OIDC identity
// scope grantable without an API grant.
func isStandardScope(scope string) bool {
	switch scope {
	case ScopeOpenID, ScopeProfile, ScopeEmail, ScopeGroups:
		return true
	}
	return false
}

func contains(list []string, value string) bool {
	return slices.Contains(list, value)
}
