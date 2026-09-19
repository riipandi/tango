package transport

// codec_contract_test.go pins the JSON field naming the Connect
// transport serves. REST and ConnectRPC are two views of one
// contract, so a client must never see camelCase on either.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rpcJSONCall posts a Connect unary procedure and returns the decoded
// response body.
func rpcJSONCall(t *testing.T, srv *HTTPServer, path, body string) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Connect-Protocol-Version", "1")
	w := httptest.NewRecorder()
	srv.Router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &decoded), "body: %s", w.Body.String())
	return decoded
}

// TestRPCJSONUsesSnakeCaseFieldNames pins the wire naming of a proto
// response with multi-word fields. protojson defaults to
// lowerCamelCase, so this fails if the snake_case codec stops being
// registered on the handler.
func TestRPCJSONUsesSnakeCaseFieldNames(t *testing.T) {
	srv := rpcTestServer(t)

	got := rpcJSONCall(t, srv, "/rpc/tango.system.v1.VersionService/Latest", "{}")
	assert.Contains(t, got, "latest_version", "proto field names must reach the wire")
	assert.NotContains(t, got, "latestVersion", "camelCase must not reach the wire")
}

// TestRPCJSONAcceptsBothRequestSpellings pins request tolerance: the
// codec serializes snake_case, but protojson still accepts the
// camelCase alias, so a client written against either spelling works.
func TestRPCJSONAcceptsBothRequestSpellings(t *testing.T) {
	srv := rpcTestServer(t)

	for _, body := range []string{`{"current_version":"x"}`, `{"currentVersion":"x"}`} {
		req := httptest.NewRequest(http.MethodPost, "/rpc/tango.system.v1.VersionService/Current", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Connect-Protocol-Version", "1")
		req.Header.Set("Authorization", "Bearer token-1")
		w := httptest.NewRecorder()
		srv.Router.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code, "body %s: %s", body, w.Body.String())
	}
}
