package testutils

// http.go holds the endpoint parity harness: reusable request and
// response helpers so handler tests assert against the same envelope
// and header contracts. The package stays free of module imports.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	jsonv2 "encoding/json/v2"

	"github.com/stretchr/testify/require"
)

// Envelope is the standard response envelope shape.
type Envelope struct {
	Status   string          `json:"status"`
	Message  string          `json:"message"`
	Data     json.RawMessage `json:"data"`
	Error    json.RawMessage `json:"error"`
	Metadata struct {
		StatusCode int    `json:"status_code"`
		RequestID  string `json:"request_id"`
	} `json:"metadata"`
}

// Do sends one request with an optional JSON body and returns the
// recorder. An empty body sends no body at all.
func Do(t testing.TB, r http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// DoForm sends one form-encoded request (OAuth/OIDC endpoints).
func DoForm(t testing.TB, r http.Handler, method, path string, values url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// WithCookie attaches the session cookie to a request.
func WithCookie(req *http.Request, name, token string) *http.Request {
	req.AddCookie(&http.Cookie{Name: name, Value: token})
	return req
}

// WithAdminHeader marks a request for the stub admin guard used by
// handler tests.
func WithAdminHeader(req *http.Request) *http.Request {
	req.Header.Set("X-Admin", "1")
	return req
}

// DecodeEnvelope parses the standard response envelope.
func DecodeEnvelope(t testing.TB, w *httptest.ResponseRecorder) Envelope {
	t.Helper()
	var env Envelope
	require.NoError(t, jsonv2.Unmarshal(w.Body.Bytes(), &env), "body: %s", w.Body.String())
	return env
}

// DecodeData parses the envelope and returns the data object.
func DecodeData(t testing.TB, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	env := DecodeEnvelope(t, w)
	var data map[string]any
	require.NoError(t, jsonv2.Unmarshal(env.Data, &data), "data: %s", env.Data)
	return data
}
