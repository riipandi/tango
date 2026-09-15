package scimsync

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"

	jsonv2 "encoding/json/v2"
	"strconv"
	"strings"
	"time"
)

// scimContentType is the SCIM 2.0 media type.
const scimContentType = "application/scim+json"

// retryAttempts bounds retries for a rate-limited request.
const retryAttempts = 3

// Service drives the outbound provisioning.
type Service struct {
	store  *PostgresStore
	source SnapshotSource
	http   *http.Client
	logger *slog.Logger
}

// NewService builds the sync service.
func NewService(store *PostgresStore, source SnapshotSource, log *slog.Logger) *Service {
	return &Service{
		store:  store,
		source: source,
		http:   &http.Client{Timeout: 30 * time.Second},
		logger: log,
	}
}

// SyncAll pushes every configured provider (recurring tick path).
func (s *Service) SyncAll(ctx context.Context) error {
	providers, err := s.store.List(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, p := range providers {
		if err := s.SyncProvider(ctx, p); err != nil {
			errs = append(errs, fmt.Errorf("provider %s: %w", p.ID.String(), err))
		}
	}
	return errors.Join(errs...)
}

// SyncProvider pushes one provider's snapshot: list remote resources,
// create missing, update changed, delete remote-only entries.
func (s *Service) SyncProvider(ctx context.Context, provider ServiceProvider) error {
	remoteUsers, err := listResources[ScimUser](s, ctx, provider, "/Users")
	if err != nil {
		return fmt.Errorf("list users: %w", err)
	}
	remoteGroups, err := listResources[ScimGroup](s, ctx, provider, "/Groups")
	if err != nil {
		return fmt.Errorf("list groups: %w", err)
	}

	users, err := s.source.UsersForClient(ctx, provider.OIDCClientID)
	if err != nil {
		return fmt.Errorf("snapshot users: %w", err)
	}
	groups, err := s.source.GroupsForClient(ctx, provider.OIDCClientID)
	if err != nil {
		return fmt.Errorf("snapshot groups: %w", err)
	}

	// Users first so groups can reference them.
	userIDsByExternal := make(map[string]string, len(users))
	for _, u := range users {
		payload := scimUserPayload(u)
		remote := findByExternalID(remoteUsers.Resources, u.ID)
		if remote == nil {
			created, err := createResource(s, ctx, provider, "/Users", payload)
			if err != nil {
				return fmt.Errorf("create user %s: %w", u.Username, err)
			}
			userIDsByExternal[u.ID] = created.ID
			continue
		}
		if _, err := updateResource(s, ctx, provider, "/Users/"+url.PathEscape(remote.ID), payload); err != nil {
			return fmt.Errorf("update user %s: %w", u.Username, err)
		}
		userIDsByExternal[u.ID] = remote.ID
	}

	// Delete remote users no longer in the snapshot.
	for _, ru := range remoteUsers.Resources {
		if _, ok := userIDsByExternal[ru.ExternalID]; !ok && ru.ExternalID != "" {
			if err := s.deleteResource(ctx, provider, "/Users/"+url.PathEscape(ru.ID)); err != nil {
				return fmt.Errorf("delete user %s: %w", ru.ExternalID, err)
			}
		}
	}

	// Groups mirror the same create/update/delete flow.
	groupIDsByExternal := make(map[string]string, len(groups))
	for _, g := range groups {
		payload := scimGroupPayload(g, userIDsByExternal)
		remote := findByExternalID(remoteGroups.Resources, g.ID)
		if remote == nil {
			created, err := createResource(s, ctx, provider, "/Groups", payload)
			if err != nil {
				return fmt.Errorf("create group %s: %w", g.Name, err)
			}
			groupIDsByExternal[g.ID] = created.ID
			continue
		}
		if _, err := updateResource(s, ctx, provider, "/Groups/"+url.PathEscape(remote.ID), payload); err != nil {
			return fmt.Errorf("update group %s: %w", g.Name, err)
		}
		groupIDsByExternal[g.ID] = remote.ID
	}
	for _, rg := range remoteGroups.Resources {
		if _, ok := groupIDsByExternal[rg.ExternalID]; !ok && rg.ExternalID != "" {
			if err := s.deleteResource(ctx, provider, "/Groups/"+url.PathEscape(rg.ID)); err != nil {
				return fmt.Errorf("delete group %s: %w", rg.ExternalID, err)
			}
		}
	}

	return s.store.MarkSynced(ctx, provider.ID, time.Now().UTC())
}

// scimUserPayload maps a snapshot row onto the SCIM Users schema.
func scimUserPayload(u ScimUserRow) ScimUser {
	payload := ScimUser{
		ResourceData: ResourceData{
			Schemas:    []string{"urn:ietf:params:scim:schemas:core:2.0:User"},
			ExternalID: u.ID,
		},
		UserName: u.Username,
		Display:  u.DisplayName,
		Active:   u.Active,
		Name: &ScimName{
			GivenName:  u.FirstName,
			FamilyName: u.LastName,
		},
	}
	if u.Email != "" {
		payload.Emails = []ScimEmail{{Value: u.Email, Primary: true}}
	}
	return payload
}

// scimGroupPayload maps a snapshot group onto the SCIM Groups schema.
func scimGroupPayload(g ScimGroupRow, userIDsByExternal map[string]string) ScimGroup {
	members := make([]ScimGroupMember, 0, len(g.Members))
	for _, external := range g.Members {
		if id, ok := userIDsByExternal[external]; ok {
			members = append(members, ScimGroupMember{Value: id})
		}
	}
	return ScimGroup{
		ResourceData: ResourceData{
			Schemas:    []string{"urn:ietf:params:scim:schemas:core:2.0:Group"},
			ExternalID: g.ID,
		},
		Display: g.Name,
		Members: members,
	}
}

// findByExternalID locates a remote resource by externalId.
func findByExternalID[T Resource](resources []T, externalID string) *T {
	for i := range resources {
		if resources[i].GetExternalID() == externalID {
			return &resources[i]
		}
	}
	return nil
}

// request sends one SCIM request with 429-aware retry.
func (s *Service) request(ctx context.Context, provider ServiceProvider, method, p string, payload any, query url.Values) (*http.Response, error) {
	endpoint := strings.TrimRight(provider.Endpoint, "/") + p
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}

	var body []byte
	if payload != nil {
		encoded, err := jsonv2.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("encode payload: %w", err)
		}
		body = encoded
	}

	for attempt := 1; ; attempt++ {
		var reader io.Reader
		if body != nil {
			reader = bytes.NewReader(body)
		}
		req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", scimContentType)
		if payload != nil {
			req.Header.Set("Content-Type", scimContentType)
		}
		if provider.Token != "" {
			req.Header.Set("Authorization", "Bearer "+provider.Token)
		}

		resp, err := s.http.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusTooManyRequests || attempt >= retryAttempts {
			return resp, nil
		}
		delay := retryDelay(resp.Header.Get("Retry-After"), attempt)
		resp.Body.Close()
		s.logger.WarnContext(ctx, "SCIM provider rate-limited, retrying",
			slog.String("provider", provider.ID.String()),
			slog.Duration("retry_after", delay))
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}
}

// retryDelay honors Retry-After seconds, falling back to backoff.
func retryDelay(after string, attempt int) time.Duration {
	if seconds, err := strconv.Atoi(strings.TrimSpace(after)); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	return time.Duration(attempt) * 2 * time.Second
}
