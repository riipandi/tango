package oidc

// cimd_test.go drives the CIMD-lite surface through the generated
// Connect contract against a stub metadata document server: create-
// with-url materializes the document, refresh rewrites document-owned
// columns, the allowlist denies unknown URLs, and admin updates never
// clobber document-owned columns.

import (
	"context"
	"crypto/tls"
	jsonv2 "encoding/json/v2"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	federationv1 "github.com/riipandi/tango/codegen/proto/go/tango/federation/v1"
)

// testTLSInsecure accepts the stub server's self-signed cert.
var testTLSInsecure = &tls.Config{InsecureSkipVerify: true} //nolint:gosec — test stub only

func connectError(t *testing.T, err error) *connect.Error {
	t.Helper()
	require.Error(t, err)
	var cerr *connect.Error
	require.True(t, errors.As(err, &cerr), "must decode as *connect.Error")
	return cerr
}

// metadataServer serves one document (TLS — the fetcher enforces
// https) and counts fetches.
func metadataServer(t *testing.T, doc map[string]any) (*httptest.Server, *int) {
	t.Helper()
	fetches := 0
	mux := http.NewServeMux()
	mux.HandleFunc("GET /metadata.json", func(w http.ResponseWriter, _ *http.Request) {
		fetches++
		w.Header().Set("Content-Type", "application/json")
		_ = jsonv2.MarshalWrite(w, doc)
	})
	server := httptest.NewTLSServer(mux)
	t.Cleanup(server.Close)
	server.TLS = testTLSInsecure
	return server, &fetches
}

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

// TestRPCIMDClientLifecycle drives create-with-url, refresh, and the
// document-ownership rule through the generated contract.
func TestRPCIMDClientLifecycle(t *testing.T) {
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
	h := &clientRPC{service: service}
	ctx := t.Context()

	createURL := server.URL + "/metadata.json"

	// Default deny: the allowlist getter is nil → invalid_argument.
	_, err := h.CreateClient(ctx, connect.NewRequest(&federationv1.CreateOidcClientRequest{
		MetadataUrl: &createURL,
	}))
	require.Error(t, err)
	assert.Equal(t, connect.CodeInvalidArgument, connectError(t, err).Code())

	// Now allow it: create materializes the document.
	service.cimdAllowlist = func() []string { return []string{server.URL + "/*"} }
	created, err := h.CreateClient(ctx, connect.NewRequest(&federationv1.CreateOidcClientRequest{
		MetadataUrl: &createURL,
	}))
	require.NoError(t, err)
	assert.Equal(t, "Metadata RP", created.Msg.GetClient().GetName())
	assert.Equal(t, "cimd", created.Msg.GetClient().GetClientType())
	assert.Equal(t, server.URL+"/metadata.json", created.Msg.GetClient().GetMetadataUrl())
	assert.True(t, created.Msg.GetClient().GetIsPublic())
	assert.Equal(t, 1, *fetches)

	// Update the document server-side, then refresh.
	docMap["client_name"] = "Refreshed RP"
	refreshed, err := h.RefreshClient(ctx, connect.NewRequest(&federationv1.GetOidcClientRequest{
		ClientId: created.Msg.GetClient().GetId(),
	}))
	require.NoError(t, err)
	assert.Equal(t, "Refreshed RP", refreshed.Msg.GetName())
	assert.Equal(t, 2, *fetches)

	// Admin update must NOT write back document-owned columns: an
	// update carrying a stale name leaves the refreshed name intact.
	updated, err := h.UpdateClient(ctx, connect.NewRequest(&federationv1.UpdateOidcClientRequest{
		ClientId:    created.Msg.GetClient().GetId(),
		Name:        "Stale Admin Name",
		Description: &[]string{"Admin note"}[0],
	}))
	require.NoError(t, err)

	clientID, err := OIDCParseClientID(created.Msg.GetClient().GetId())
	require.NoError(t, err)
	fresh, err := store.GetClient(ctx, clientID)
	require.NoError(t, err)
	assert.Equal(t, "Refreshed RP", fresh.Name, "metadata owns name")
	assert.Equal(t, "Admin note", fresh.Description, "description stays locally managed")
	_ = updated

	// Refresh on a standard client → invalid_argument.
	standard := clientFixture(ctx, t, store, "std-"+stamp())
	_, err = h.RefreshClient(ctx, connect.NewRequest(&federationv1.GetOidcClientRequest{
		ClientId: standard.ID.String(),
	}))
	assert.Equal(t, connect.CodeInvalidArgument, connectError(t, err).Code())
}
