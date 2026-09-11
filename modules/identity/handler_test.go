package identity

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/internal/kernel"
)

// newTestRouter mounts the identity module the same way the
// application does: inside the shared /api group.
func newTestRouter() chi.Router {
	reg := kernel.NewRegistry()
	reg.Register(New(NewMemoryStore(), nil))

	r := chi.NewRouter()
	r.Route("/api", reg.ApplyAPI)
	return r
}

func do(r chi.Router, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func decodeBody(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	return body
}

func TestAPIRoot(t *testing.T) {
	r := newTestRouter()
	w := do(r, http.MethodGet, "/api/", "")

	require.Equal(t, http.StatusOK, w.Code)
	body := decodeBody(t, w)
	assert.Equal(t, config.AppName, body["name"])
	assert.Equal(t, config.AppVersion, body["version"])
}

func TestCreateUser(t *testing.T) {
	r := newTestRouter()
	w := do(r, http.MethodPost, "/api/users", `{"name":"John"}`)

	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	body := decodeBody(t, w)
	assert.NotEmpty(t, body["id"])
	assert.Equal(t, "John", body["name"])
}

func TestCreateUserInvalidJSON(t *testing.T) {
	r := newTestRouter()
	w := do(r, http.MethodPost, "/api/users", `{invalid`)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateUserEmptyName(t *testing.T) {
	r := newTestRouter()
	w := do(r, http.MethodPost, "/api/users", `{"name":""}`)

	require.Equal(t, http.StatusBadRequest, w.Code)
	body := decodeBody(t, w)
	assert.Equal(t, ErrInvalidName.Error(), body["error"])
}

func TestListUsers(t *testing.T) {
	r := newTestRouter()
	do(r, http.MethodPost, "/api/users", `{"name":"A"}`)

	w := do(r, http.MethodGet, "/api/users", "")
	require.Equal(t, http.StatusOK, w.Code)

	var users []map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &users))
	require.Len(t, users, 1)
	assert.Equal(t, "A", users[0]["name"])
}

func TestGetUser(t *testing.T) {
	r := newTestRouter()
	w := do(r, http.MethodPost, "/api/users", `{"name":"John"}`)
	body := decodeBody(t, w)
	id, _ := body["id"].(string)
	require.NotEmpty(t, id)

	w = do(r, http.MethodGet, "/api/users/"+id, "")
	require.Equal(t, http.StatusOK, w.Code)

	body = decodeBody(t, w)
	assert.Equal(t, "John", body["name"])
}

func TestGetUserNotFound(t *testing.T) {
	r := newTestRouter()
	w := do(r, http.MethodGet, "/api/users/99", "")

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestAPIUnknownPath(t *testing.T) {
	r := newTestRouter()
	w := do(r, http.MethodGet, "/api/nope", "")

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestAPIMethodNotAllowed(t *testing.T) {
	r := newTestRouter()
	w := do(r, http.MethodDelete, "/api/users/1", "")

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}
