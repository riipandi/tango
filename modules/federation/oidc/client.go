package oidc

// client.go holds the relying-party client logic: secret hashing
// (SHA-256 at rest, raw secret shown once at create), allowlist
// grants, and the callback-pattern contract. HTTP shells live in
// handler.go.

import (
	"context"
	"strings"
)

// createClient validates the request, hashes the secret, and
// persists the client with its group allowlist. Public clients get
// no secret. Returns the raw secret when one was generated.
func (s *Service) createClient(ctx context.Context, request clientRequest) (Client, string, error) {
	secret := ""
	switch {
	case request.IsPublic:
		// Public clients authenticate by identity only.
	case request.Secret != nil && *request.Secret != "":
		secret = *request.Secret
	default:
		generated, err := randomToken()
		if err != nil {
			return Client{}, "", err
		}
		secret = generated
	}

	params := ClientCreateParams{
		Name:                        strings.TrimSpace(request.Name),
		Description:                 request.Description,
		CallbackURLs:                request.CallbackURLs,
		LogoutCallbackURLs:          request.LogoutCallbackURLs,
		LaunchURL:                   request.LaunchURL,
		IsPublic:                    request.IsPublic,
		PKCEEnabled:                 request.PKCEEnabled,
		PKCESupported:               request.PKCESupported,
		SkipConsent:                 request.SkipConsent,
		IsGroupRestricted:           request.IsGroupRestricted,
		AccessTokenDurationMinutes:  request.AccessTokenDurationMinutes,
		RefreshTokenDurationMinutes: request.RefreshTokenDurationMinutes,
		SecretHash:                  secretHash(secret),
	}

	client, err := s.store.CreateClient(ctx, params)
	if err != nil {
		return Client{}, "", err
	}
	if err := s.store.SetClientGroups(ctx, client.ID, request.AllowedGroupIDs); err != nil {
		return Client{}, "", err
	}
	client.AllowedGroupIDs = request.AllowedGroupIDs
	if client.AllowedGroupIDs == nil {
		client.AllowedGroupIDs = []string{}
	}
	return client, secret, nil
}

// updateClient patches the client; empty allowed list clears the
// restriction grants.
func (s *Service) updateClient(ctx context.Context, id OIDCClientID, request clientRequest) (Client, error) {
	params := ClientUpdateParams{
		Name:                        &request.Name,
		Description:                 &request.Description,
		CallbackURLs:                request.CallbackURLs,
		LogoutCallbackURLs:          request.LogoutCallbackURLs,
		LaunchURL:                   &request.LaunchURL,
		IsPublic:                    &request.IsPublic,
		PKCEEnabled:                 &request.PKCEEnabled,
		PKCESupported:               &request.PKCESupported,
		SkipConsent:                 &request.SkipConsent,
		IsGroupRestricted:           &request.IsGroupRestricted,
		AccessTokenDurationMinutes:  &request.AccessTokenDurationMinutes,
		RefreshTokenDurationMinutes: &request.RefreshTokenDurationMinutes,
	}
	if request.Secret != nil && *request.Secret != "" {
		hash := secretHash(*request.Secret)
		params.SecretHash = &hash
	}

	client, err := s.store.UpdateClient(ctx, id, params)
	if err != nil {
		return Client{}, err
	}
	if err := s.store.SetClientGroups(ctx, id, request.AllowedGroupIDs); err != nil {
		return Client{}, err
	}
	client.AllowedGroupIDs = request.AllowedGroupIDs
	if client.AllowedGroupIDs == nil {
		client.AllowedGroupIDs = []string{}
	}
	return client, nil
}

// deleteClient removes the client (cascades to grants and codes).
func (s *Service) deleteClient(ctx context.Context, id OIDCClientID) error {
	if err := s.store.DeleteClient(ctx, id); err != nil {
		return err
	}
	s.record(ctx, "oidc_client_deleted", map[string]any{
		"client_id": id.String(),
	})
	return nil
}

// secretHash derives the at-rest digest; empty stays empty (public
// clients).
func secretHash(secret string) string {
	if secret == "" {
		return ""
	}
	return sha256Hex(secret)
}
