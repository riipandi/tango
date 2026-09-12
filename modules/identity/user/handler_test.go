package user

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	jsonv2 "encoding/json/v2"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestRouter mounts the user core inside the shared /api group,
// the way the identity module does.
func newTestRouter() chi.Router {
	svc := NewService(NewMemoryStore(), nil)

	r := chi.NewRouter()
	r.Route("/api", svc.APIRoutes)
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
	require.NoError(t, jsonv2.Unmarshal(w.Body.Bytes(), &body))
	return body
}

// decodeData returns the payload inside the standard response envelope.
func decodeData(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	body := decodeBody(t, w)
	data, ok := body["data"].(map[string]any)
	require.True(t, ok, "envelope data is not an object: %v", body)
	return data
}

func TestCreateUser(t *testing.T) {
	r := newTestRouter()
	w := do(r, http.MethodPost, "/api/users", `{"name":"John"}`)

	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	body := decodeBody(t, w)
	assert.Equal(t, "success", body["status"])

	data := decodeData(t, w)
	assert.NotEmpty(t, data["id"])
	assert.True(t, strings.HasPrefix(data["id"].(string), "user_"))
	assert.Equal(t, "John", data["name"])
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
	assert.Equal(t, "error", body["status"])
	assert.Equal(t, ErrInvalidName.Error(), body["message"])
}

func TestListUsers(t *testing.T) {
	r := newTestRouter()
	do(r, http.MethodPost, "/api/users", `{"name":"A"}`)

	w := do(r, http.MethodGet, "/api/users", "")
	require.Equal(t, http.StatusOK, w.Code)

	var payload struct {
		Status string `json:"status"`
		Data   []struct {
			Name string `json:"name"`
		} `json:"data"`
	}
	require.NoError(t, jsonv2.Unmarshal(w.Body.Bytes(), &payload))
	assert.Equal(t, "success", payload.Status)
	require.Len(t, payload.Data, 1)
	assert.Equal(t, "A", payload.Data[0].Name)
}

func TestGetUser(t *testing.T) {
	r := newTestRouter()
	w := do(r, http.MethodPost, "/api/users", `{"name":"John"}`)
	id, _ := decodeData(t, w)["id"].(string)
	require.NotEmpty(t, id)

	w = do(r, http.MethodGet, "/api/users/"+id, "")
	require.Equal(t, http.StatusOK, w.Code)

	data := decodeData(t, w)
	assert.Equal(t, "John", data["name"])
}

func TestGetUserNotFound(t *testing.T) {
	r := newTestRouter()
	w := do(r, http.MethodGet, "/api/users/99", "")

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestUserMethodNotAllowed(t *testing.T) {
	r := newTestRouter()
	w := do(r, http.MethodDelete, "/api/users/1", "")

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}
