package scimsync

import (
	"context"
	"fmt"
	"net/url"
	"time"

	jsonv2 "encoding/json/v2"
)

// ResourceData is the common SCIM resource envelope.
type ResourceData struct {
	ID         string        `json:"id,omitempty"`
	ExternalID string        `json:"externalId,omitempty"`
	Schemas    []string      `json:"schemas"`
	Meta       *ResourceMeta `json:"meta,omitempty"`
}

// GetID implements Resource.
func (r ResourceData) GetID() string { return r.ID }

// GetExternalID implements Resource.
func (r ResourceData) GetExternalID() string { return r.ExternalID }

// GetSchemas implements Resource.
func (r ResourceData) GetSchemas() []string { return r.Schemas }

// GetMeta implements Resource.
func (r ResourceData) GetMeta() ResourceMeta {
	if r.Meta == nil {
		return ResourceMeta{}
	}
	return *r.Meta
}

// ResourceMeta carries location and timestamps.
type ResourceMeta struct {
	Location     string    `json:"location,omitempty"`
	ResourceType string    `json:"resourceType,omitempty"`
	Created      time.Time `json:"created"`
	LastModified time.Time `json:"lastModified"`
	Version      string    `json:"version,omitempty"`
}

// Resource is the generic SCIM resource contract.
type Resource interface {
	GetID() string
	GetExternalID() string
	GetSchemas() []string
	GetMeta() ResourceMeta
}

// ScimUser is a SCIM Users resource (request and response).
type ScimUser struct {
	ResourceData
	UserName string      `json:"userName"`
	Name     *ScimName   `json:"name,omitempty"`
	Display  string      `json:"displayName,omitempty"`
	Active   bool        `json:"active"`
	Emails   []ScimEmail `json:"emails,omitempty"`
}

// GetExternalID shadows the embedded method to keep pointer/uniform use.
func (u ScimUser) GetExternalID() string { return u.ResourceData.GetExternalID() }

// ScimName splits given/family names.
type ScimName struct {
	GivenName  string `json:"givenName,omitempty"`
	FamilyName string `json:"familyName,omitempty"`
}

// ScimEmail is one email entry.
type ScimEmail struct {
	Value   string `json:"value"`
	Primary bool   `json:"primary,omitempty"`
}

// ScimGroup is a SCIM Groups resource.
type ScimGroup struct {
	ResourceData
	Display string            `json:"displayName"`
	Members []ScimGroupMember `json:"members,omitempty"`
}

// GetExternalID shadows the embedded method.
func (g ScimGroup) GetExternalID() string { return g.ResourceData.GetExternalID() }

// ScimGroupMember references one user by id.
type ScimGroupMember struct {
	Value string `json:"value"`
}

// ListResponse is the SCIM list envelope.
type ListResponse[T any] struct {
	Resources    []T `json:"Resources"`
	TotalResults int `json:"totalResults"`
	StartIndex   int `json:"startIndex"`
	ItemsPerPage int `json:"itemsPerPage"`
}

// listResources pages through a remote collection.
func listResources[T Resource](s *Service, ctx context.Context, provider ServiceProvider, path string) (ListResponse[T], error) {
	var result ListResponse[T]
	startIndex := 1
	const page = 100
	for {
		query := url.Values{"startIndex": []string{"1"}, "count": []string{"100"}}
		query.Set("startIndex", fmt.Sprint(startIndex))
		query.Set("count", fmt.Sprint(page))

		resp, err := s.request(ctx, provider, "GET", path, nil, query)
		if err != nil {
			return result, err
		}
		if resp.StatusCode/100 != 2 {
			return result, fmt.Errorf("scim list %s: status %d: %s", path, resp.StatusCode, resp.Body)
		}
		var pageResp ListResponse[T]
		if err := jsonv2.Unmarshal(resp.Body, &pageResp); err != nil {
			return result, fmt.Errorf("decode scim list %s: %w", path, err)
		}
		result.Resources = append(result.Resources, pageResp.Resources...)
		result.TotalResults = pageResp.TotalResults
		startIndex += len(pageResp.Resources)
		if len(pageResp.Resources) < page || startIndex > pageResp.TotalResults {
			result.StartIndex, result.ItemsPerPage = 1, len(result.Resources)
			return result, nil
		}
	}
}

// createResource POSTs a new resource and returns the remote view.
func createResource[T Resource](s *Service, ctx context.Context, provider ServiceProvider, path string, payload T) (T, error) {
	var zero T
	resp, err := s.request(ctx, provider, "POST", path, payload, nil)
	if err != nil {
		return zero, err
	}
	if resp.StatusCode/100 != 2 {
		return zero, fmt.Errorf("scim create %s: status %d: %s", path, resp.StatusCode, resp.Body)
	}
	var created T
	if err := jsonv2.Unmarshal(resp.Body, &created); err != nil {
		return zero, fmt.Errorf("decode scim create %s: %w", path, err)
	}
	return created, nil
}

// updateResource PUTs the full resource.
func updateResource[T Resource](s *Service, ctx context.Context, provider ServiceProvider, path string, payload T) (T, error) {
	var zero T
	resp, err := s.request(ctx, provider, "PUT", path, payload, nil)
	if err != nil {
		return zero, err
	}
	if resp.StatusCode/100 != 2 {
		return zero, fmt.Errorf("scim update %s: status %d: %s", path, resp.StatusCode, resp.Body)
	}
	var updated T
	if err := jsonv2.Unmarshal(resp.Body, &updated); err != nil {
		return zero, fmt.Errorf("decode scim update %s: %w", path, err)
	}
	return updated, nil
}

// deleteResource removes a remote resource.
func (s *Service) deleteResource(ctx context.Context, provider ServiceProvider, path string) error {
	resp, err := s.request(ctx, provider, "DELETE", path, nil, nil)
	if err != nil {
		return err
	}
	if resp.StatusCode/100 != 2 && resp.StatusCode != 404 {
		return fmt.Errorf("scim delete %s: status %d: %s", path, resp.StatusCode, resp.Body)
	}
	return nil
}
