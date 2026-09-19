package oidc

// handler_meta.go serves client metadata and claims preview endpoints.

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/riipandi/tango/pkg/responder"
)

// idTokenClaims mirrors signIDToken's private-claim assembly minus
// nonce/sid (a preview has no session).
func (s *Service) idTokenClaims(client Client, claims UserClaims, now time.Time, ttl time.Duration) map[string]any {
	out := map[string]any{
		"iss":       s.issuer,
		"sub":       claims.Subject,
		"aud":       []string{client.ID.String()},
		"azp":       client.ID.String(),
		"iat":       now.Unix(),
		"exp":       now.Add(ttl).Unix(),
		"auth_time": now.Unix(),
	}
	putProfileClaims(out, claims)
	return out
}

// accessTokenClaims mirrors the access-token private claims.
func (s *Service) accessTokenClaims(client Client, claims UserClaims, scope string, now time.Time, ttl time.Duration) map[string]any {
	return map[string]any{
		"iss":       s.issuer,
		"sub":       claims.Subject,
		"aud":       []string{client.ID.String()},
		"client_id": client.ID.String(),
		"scope":     scope,
		"jti":       "preview",
		"iat":       now.Unix(),
		"exp":       now.Add(ttl).Unix(),
	}
}

// profileClaimsMap builds the userinfo response shape from scoped
// claims (shared by handleUserInfo and the preview).
func profileClaimsMap(scope string, claims UserClaims) map[string]any {
	profile := map[string]any{"sub": claims.Subject}
	if scopeHas(scope, ScopeEmail) && claims.Email != "" {
		profile["email"] = claims.Email
		profile["email_verified"] = claims.EmailVerified
	}
	if scopeHas(scope, ScopeProfile) {
		if claims.Name != "" {
			profile["name"] = claims.Name
		}
		if claims.PreferredUsername != "" {
			profile["preferred_username"] = claims.PreferredUsername
		}
	}
	if scopeHas(scope, ScopeGroups) && len(claims.Groups) > 0 {
		profile["groups"] = claims.Groups
	}
	for key, value := range claims.Custom {
		profile[key] = value
	}
	return profile
}

// putProfileClaims adds the profile-shaped private claims (email,
// name, preferred_username, groups, custom) — the ID-token payload
// above `sub`.
func putProfileClaims(out map[string]any, claims UserClaims) {
	if claims.Email != "" {
		out["email"] = claims.Email
		out["email_verified"] = claims.EmailVerified
	}
	if claims.Name != "" {
		out["name"] = claims.Name
	}
	if claims.PreferredUsername != "" {
		out["preferred_username"] = claims.PreferredUsername
	}
	out["groups"] = claims.Groups
	for k, v := range claims.Custom {
		out[k] = v
	}
}

// metaView is the trimmed client payload for discovery/configuration
// surfaces use snake_case.

// clientIDParam parses the {clientId} URL param; invalid ids fail
// with a 400 like the rest of the client CRUD surface.
func clientIDParam(w http.ResponseWriter, r *http.Request) (OIDCClientID, bool) {
	id, err := OIDCParseClientID(chi.URLParam(r, "clientId"))
	if err != nil {
		responder.Fail(w, r, http.StatusBadRequest, "invalid client id")
		return id, false
	}
	return id, true
}

// writeClientError maps client lookups to the envelope.
func writeClientError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, ErrNotFound) {
		responder.NotFoundJSON(w, r)
		return
	}
	responder.Fail(w, r, http.StatusInternalServerError, "internal error")
}
