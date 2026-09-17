package devicelogin

// handler_test.go pins the public error contract of the exchange and
// inspect surfaces: failures answer the fixed generic message, never
// internal wrapped error text.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	jsonv2 "encoding/json/v2"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/riipandi/tango/modules/identity"
)

func newHandlerRouter(t *testing.T) (chi.Router, *Service) {
	t.Helper()
	service, _, _ := testStack(t)

	r := chi.NewRouter()
	r.Route("/api", func(r chi.Router) {
		New(service).APIRoutes(r, identity.RouteGroups{})
	})
	return r, service
}

// TestExchangeErrorIsGeneric drives the anonymous exchange endpoint:
// failures carry the fixed public message only — no internal error
// text, no store detail.
func TestExchangeErrorIsGeneric(t *testing.T) {
	r, _ := newHandlerRouter(t)

	// A real request gives a valid device token (pairing cookie); the
	// request id stays unknown, which exercises the not-found path.
	createBody := `{}`
	req := httptest.NewRequest(http.MethodPost, "/api/device-login/requests", strings.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())

	cookies := w.Result().Cookies()
	require.NotEmpty(t, cookies)
	deviceToken := cookies[0].Value

	var created struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, jsonv2.Unmarshal(w.Body.Bytes(), &created))

	// Tamper the device token: the token check fails first.
	exchange := func(id, token string) *httptest.ResponseRecorder {
		body := `{}`
		req := httptest.NewRequest(http.MethodPost, "/api/device-login/requests/"+id+"/exchange", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.AddCookie(&http.Cookie{Name: "tango_device_login", Value: token})
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	// A tampered pairing token and an unknown request id are
	// indistinguishable: both answer the fixed not-found message,
	// never wrapped store detail.
	w = exchange(created.Data.ID, "wrong-token")
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "device login request is invalid or expired")
	assert.NotContains(t, w.Body.String(), "devicelogin:")

	w = exchange("00000000-0000-0000-0000-000000000000", deviceToken)
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "device login request is invalid or expired")
	assert.NotContains(t, w.Body.String(), "devicelogin:")
	assert.NotContains(t, w.Body.String(), "sql")
}
