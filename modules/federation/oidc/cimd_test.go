package oidc

// cimd_test.go drives the CIMD-lite surface against a stub metadata
// document server: create-with-url materializes the document, refresh
// rewrites document-owned columns, the allowlist denies unknown URLs,
// and admin updates never clobber document-owned columns.

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testTLSInsecure accepts the stub server's self-signed cert.
var testTLSInsecure = &tls.Config{InsecureSkipVerify: true} //nolint:gosec — test stub only

// metadataServer serves one document (TLS — the fetcher enforces
// https) and counts fetches.
func metadataServer(t *testing.T, doc map[string]any) (*httptest.Server, *int) {
	t.Helper()
	fetches := 0
	mux := http.NewServeMux()
	mux.HandleFunc("GET /metadata.json", func(w http.ResponseWriter, _ *http.Request) {
		fetches++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(doc)
	})
	server := httptest.NewTLSServer(mux)
	t.Cleanup(server.Close)
	return server, &fetches
}

// cimdStack wires the service with the stub fetcher + allowlist.
func cimdStack(t *testing.T, docServer *httptest.Server, allowAll bool) (*Service, Store) {
	t.Helper()
	service, store, _ := testStack(t)
	service.metadataFetcher = fetcherAdapter{}
	if allowAll {
		service.cimdAllowlist = func() []string { return []string{docServer.URL + "/*"} }
	}
	return service, store
}

// fetcherAdapter adapts the fetcher package's SendRaw without
// importing it (the test uses a TLS-skip client for the stub server).
type fetcherAdapter struct{}

func (fetcherAdapter) SendRaw(ctx context.Context, method, url string, _ map[string]string, _ []byte) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return 0, nil, err
	}
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: testTLSInsecure}}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	buf := make([]byte, MaxMetadataDocument+1)
	n, _ := resp.Body.Read(buf)
	return resp.StatusCode, buf[:n], nil
}

func TestCIMDClientLifecycle(t *testing.T) {
	// One shared document map: the server encodes THIS instance, so
	// later mutations are visible to the fetcher.
	docMap := map[string]any{
		"client_id":                  "https://rp.example/metadata.json",
		"client_name":                "Metadata RP",
		"redirect_uris":              []string{"https://rp.example/callback"},
		"token_endpoint_auth_method": "none",
		"grant_types":                []string{"authorization_code", "refresh_token"},
	}
	server, fetches := metadataServer(t, docMap)
	service, store := cimdStack(t, server, false) // allowlist NOT configured yet

	createBody := `{"metadata_url":"` + server.URL + `/metadata.json"}`
	req := signInRequest(http.MethodPost, clientsAPIPrefix, createBody)
	rec := httptest.NewRecorder()
	router := newRouter(t, service, &fakeAuthenticator{validToken: "session-token-1", principal: principalFixture("01a00000-0000-0000-0000-000000000000")})
	router.ServeHTTP(rec, req)

	// Default deny: the allowlist getter is nil → 422.
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())

	// Now allow it: create materializes the document.
	service.cimdAllowlist = func() []string { return []string{server.URL + "/*"} }
	req = signInRequest(http.MethodPost, clientsAPIPrefix, createBody)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	var created struct {
		Data struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			ClientType  string `json:"client_type"`
			MetadataURL string `json:"metadata_url"`
			IsPublic    bool   `json:"is_public"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
	assert.Equal(t, "Metadata RP", created.Data.Name)
	assert.Equal(t, "cimd", created.Data.ClientType)
	assert.Equal(t, server.URL+"/metadata.json", created.Data.MetadataURL)
	assert.True(t, created.Data.IsPublic)
	assert.Equal(t, 1, *fetches)

	// Update the document server-side, then refresh (admin endpoint).
	docMap["client_name"] = "Refreshed RP"
	req = signInRequest(http.MethodPost, clientsAPIPrefix+"/"+created.Data.ID+"/refresh")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "Refreshed RP")
	assert.Equal(t, 2, *fetches)

	// Admin update must NOT write back document-owned columns: an
	// update carrying a stale name leaves the refreshed name intact.
	req = signInRequest(http.MethodPut, clientsAPIPrefix+"/"+created.Data.ID, `{"name":"Stale Admin Name","description":"Admin note"}`)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	clientID, err := OIDCParseClientID(created.Data.ID)
	require.NoError(t, err)
	fresh, err := store.GetClient(t.Context(), clientID)
	require.NoError(t, err)
	assert.Equal(t, "Refreshed RP", fresh.Name, "metadata owns name")
	assert.Equal(t, "Admin note", fresh.Description, "description stays locally managed")

	// Refresh on a standard client → 422.
	standard := clientFixture(t.Context(), t, store, "std-"+stamp())
	req = signInRequest(http.MethodPost, clientsAPIPrefix+"/"+standard.ID.String()+"/refresh")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}
