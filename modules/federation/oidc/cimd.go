package oidc

// cimd.go implements CIMD-lite for clients backed by a Client ID
// Metadata Document.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	jsonv2 "encoding/json/v2"
)

// MetadataDocumentTTL controls the metadata refresh window.
const MetadataDocumentTTL = 24 * time.Hour

// MaxMetadataDocument bounds one metadata document fetch.
const MaxMetadataDocument = 1 << 20 // 1 MiB, matches fosite's default

// DocumentFetcher downloads raw documents (consumer-side adapter —
// *fetcher.Fetcher satisfies it); kept one-way so this module never
// imports the fetcher package.
type DocumentFetcher interface {
	SendRaw(ctx context.Context, method, url string, headers map[string]string, body []byte) (status int, respBody []byte, err error)
}

// MetadataDocument is the RFC 7591-shaped subset tango consumes.
type MetadataDocument struct {
	ClientID                string   `json:"client_id"`
	ClientName              string   `json:"client_name"`
	ClientDescription       string   `json:"client_description"`
	RedirectURIs            []string `json:"redirect_uris"`
	PostLogoutRedirectURIs  []string `json:"post_logout_redirect_uris"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	GrantTypes              []string `json:"grant_types"`
}

// metadataError wraps CIMD fetch/validate/policy failures: caller
// facing (422 with the message), distinct from storage failures.
type metadataError struct{ err error }

func (e metadataError) Error() string { return e.err.Error() }
func (e metadataError) Unwrap() error { return e.err }

// fetchMetadataDocument downloads + parses one document.
func fetchMetadataDocument(ctx context.Context, fetcher DocumentFetcher, documentURL string) (*MetadataDocument, error) {
	if !strings.HasPrefix(documentURL, "https://") {
		return nil, metadataError{errors.New("metadata document URL must be https")}
	}

	status, body, err := fetcher.SendRaw(ctx, http.MethodGet, documentURL, map[string]string{"Accept": "application/json"}, nil)
	if err != nil {
		return nil, metadataError{fmt.Errorf("metadata document fetch: %w", err)}
	}
	if status != http.StatusOK {
		return nil, metadataError{fmt.Errorf("metadata document fetch: unexpected status %d", status)}
	}
	if len(body) > MaxMetadataDocument {
		return nil, metadataError{errors.New("metadata document exceeds 1 MiB")}
	}

	var doc MetadataDocument
	if err := jsonv2.Unmarshal(body, &doc); err != nil {
		return nil, metadataError{fmt.Errorf("metadata document parse: %w", err)}
	}
	return &doc, nil
}

// validateMetadataDocument enforces the subset tango supports.
func validateMetadataDocument(doc *MetadataDocument) error {
	if doc.ClientName == "" {
		return errors.New("metadata document requires client_name")
	}
	if len(doc.RedirectURIs) == 0 {
		return errors.New("metadata document requires redirect_uris")
	}
	// Pocket ID never persists CIMD key material: public clients only.
	if method := doc.TokenEndpointAuthMethod; method != "none" {
		return fmt.Errorf("token_endpoint_auth_method must be %q, got %q", "none", method)
	}
	for _, grant := range doc.GrantTypes {
		if grant != "authorization_code" && grant != "refresh_token" {
			return fmt.Errorf("unsupported grant_type %q", grant)
		}
	}
	return nil
}

// urlAllowed matches the allowlist: exact match, or prefix when the
// pattern ends with '*' (same contract as callback URLs).
func urlAllowed(allowlist []string, documentURL string) bool {
	for _, pattern := range allowlist {
		if pattern == documentURL {
			return true
		}
		if after, ok := strings.CutSuffix(pattern, "*"); ok && strings.HasPrefix(documentURL, after) {
			return true
		}
	}
	return false
}
