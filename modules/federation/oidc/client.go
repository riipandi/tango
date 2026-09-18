package oidc

// client.go holds the relying-party client logic: secret hashing
// (SHA-256 at rest, raw secret shown once at create), allowlist
// grants, and the callback-pattern contract. HTTP shells live in
// handler.go.

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/riipandi/tango/internal/datastore"
)

// errNotCIMD marks a refresh attempt against a non-metadata client.
var errNotCIMD = errors.New("client is not a metadata document client")

// createClient validates the request, hashes the secret, and
// persists the client with its group allowlist. Public clients get
// no secret. Returns the raw secret when one was generated. A
// metadata_url (CIMD-lite) fetches + materializes the document
// instead: name/redirect URIs come from the document and the client
// is public (auth method "none").
func (s *Service) createClient(ctx context.Context, request clientRequest) (Client, string, error) {
	if request.MetadataURL != "" {
		return s.createCIMDClient(ctx, request)
	}

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

// createCIMDClient registers a metadata-document client: fetch the
// document now, materialize its columns, store the source URL.
func (s *Service) createCIMDClient(ctx context.Context, request clientRequest) (Client, string, error) {
	if allowErr := s.cimdAllowed(ctx, request.MetadataURL); allowErr != nil {
		return Client{}, "", allowErr
	}

	doc, fetchErr := fetchMetadataDocument(ctx, s.metadataFetcher, request.MetadataURL)
	if fetchErr != nil {
		return Client{}, "", fetchErr
	}
	if validateErr := validateMetadataDocument(doc); validateErr != nil {
		return Client{}, "", validateErr
	}

	params := ClientCreateParams{
		Name:                        doc.ClientName,
		Description:                 doc.ClientDescription,
		CallbackURLs:                doc.RedirectURIs,
		LogoutCallbackURLs:          doc.PostLogoutRedirectURIs,
		IsPublic:                    true, // auth method "none"
		AccessTokenDurationMinutes:  request.AccessTokenDurationMinutes,
		RefreshTokenDurationMinutes: request.RefreshTokenDurationMinutes,
		MetadataURL:                 request.MetadataURL,
		ClientType:                  "cimd",
		MetadataGrantTypes:          doc.GrantTypes,
	}

	client, err := s.store.CreateClient(ctx, params)
	if err != nil {
		return Client{}, "", err
	}
	if err := s.store.SetClientGroups(ctx, client.ID, request.AllowedGroupIDs); err != nil {
		return Client{}, "", err
	}
	s.record(ctx, "oidc_client_metadata_created", map[string]any{"client_id": client.ID.String(), "metadata_url": request.MetadataURL})
	return client, "", nil
}

// refreshClientMetadata re-fetches a CIMD client's document and
// rewrites the document-owned columns (bypassing the admin-update
// guard, which protects the same columns from stale admin snapshots).
func (s *Service) refreshClientMetadata(ctx context.Context, client Client) (Client, error) {
	if client.ClientType != "cimd" || client.MetadataURL == nil {
		return Client{}, errNotCIMD
	}
	if err := s.cimdAllowed(ctx, *client.MetadataURL); err != nil {
		return Client{}, err
	}

	doc, err := fetchMetadataDocument(ctx, s.metadataFetcher, *client.MetadataURL)
	if err != nil {
		return Client{}, err
	}
	if err := validateMetadataDocument(doc); err != nil {
		return Client{}, err
	}

	description := doc.ClientDescription
	params := ClientUpdateParams{
		Name:               &doc.ClientName,
		Description:        &description,
		CallbackURLs:       doc.RedirectURIs,
		LogoutCallbackURLs: doc.PostLogoutRedirectURIs,
		IsPublic:           datastore.Ptr(true),
		MetadataGrantTypes: doc.GrantTypes,
	}
	if err := s.store.RefreshClientMetadata(ctx, client.ID, params); err != nil {
		return Client{}, err
	}
	s.record(ctx, "oidc_client_metadata_refreshed", map[string]any{"client_id": client.ID.String()})

	return s.store.GetClient(ctx, client.ID)
}

// cimdAllowed enforces the operator allowlist (default deny) and the
// fetcher presence.
func (s *Service) cimdAllowed(ctx context.Context, documentURL string) error {
	if s.metadataFetcher == nil {
		return metadataError{errors.New("client metadata documents are not enabled")}
	}
	if s.cimdAllowlist == nil {
		return metadataError{errors.New("metadata document allowlist is not configured")}
	}
	if !urlAllowed(s.cimdAllowlist(), documentURL) {
		return metadataError{errors.New("metadata document URL is not in the allowlist")}
	}
	return nil
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

// hasUsableSecret reports whether at least one credentials entry
// can still authenticate (active, unexpired).
func hasUsableSecret(c Client) bool {
	for _, entry := range c.Secrets {
		if !entry.IsActive || (entry.ExpiresAt != nil && entry.ExpiresAt.Before(time.Now().UTC())) {
			continue
		}
		return true
	}
	return false
}

// addSecret creates a new client secret (generated or caller
// supplied) and returns the view plus the raw value — the only time
// the value is visible.
func (s *Service) addSecret(ctx context.Context, id OIDCClientID, req createSecretRequest) (map[string]any, error) {
	raw := req.Secret
	if raw == "" {
		generated, err := randomToken()
		if err != nil {
			return nil, err
		}
		raw = generated
	}

	entry := ClientSecret{
		// Sub-entity without its own table: a random ID suffices, no TypeID.
		ID:        randomHex(16),
		CreatedAt: time.Now().UTC(),
		ExpiresAt: req.ExpiresAt,
		IsActive:  true,
	}
	if err := s.store.AddClientSecret(ctx, id, entry, sha256Hex(raw)); err != nil {
		return nil, err
	}

	return map[string]any{
		"id":         entry.ID,
		"created_at": entry.CreatedAt,
		"expires_at": entry.ExpiresAt,
		"is_active":  entry.IsActive,
		"secret":     raw,
	}, nil
}
