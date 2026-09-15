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

	"github.com/riipandi/tango/modules/identity"
)

// newTestRouter mounts the user core (real Postgres store) inside
// the shared /api group, the way the identity module does.
func newTestRouter(t *testing.T) chi.Router {
	r := chi.NewRouter()
	r.Route("/api", func(r chi.Router) {
		NewService(newTestStore(t), nil).APIRoutes(r, identity.RouteGroups{})
	})
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

// johnPayload is unique per call: the test container is shared
// across tests and runs, so usernames/emails must not collide.
func johnPayload() string {
	stamp := uniqueStamp()
	return `{"username":"john` + stamp + `","email":"john` + stamp + `@example.com","first_name":"John"}`
}

func TestCreateUser(t *testing.T) {
	r := newTestRouter(t)
	w := do(r, http.MethodPost, "/api/users", johnPayload())

	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	body := decodeBody(t, w)
	assert.Equal(t, "success", body["status"])

	data := decodeData(t, w)
	assert.True(t, strings.HasPrefix(data["id"].(string), "user_"))
	assert.True(t, strings.HasPrefix(data["username"].(string), "john"))
	assert.True(t, strings.HasPrefix(data["email"].(string), "john"))
}

func TestCreateUserInvalidJSON(t *testing.T) {
	r := newTestRouter(t)
	w := do(r, http.MethodPost, "/api/users", `{invalid`)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateUserValidation(t *testing.T) {
	r := newTestRouter(t)

	cases := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{"short username", `{"username":"ab","email":"a@b.co"}`, http.StatusBadRequest},
		{"bad email", `{"username":"john","email":"nope"}`, http.StatusBadRequest},
	}

	for _, tc := range cases {
		w := do(r, http.MethodPost, "/api/users", tc.body)
		assert.Equal(t, tc.wantStatus, w.Code, tc.name)
	}

	// The SAME payload twice → unique violation → 409.
	payload := johnPayload()
	w := do(r, http.MethodPost, "/api/users", payload)
	require.Equal(t, http.StatusCreated, w.Code)
	w = do(r, http.MethodPost, "/api/users", payload)
	assert.Equal(t, http.StatusConflict, w.Code)
	body := decodeBody(t, w)
	assert.Equal(t, ErrDuplicate.Error(), body["message"])
}

func TestListUsers(t *testing.T) {
	r := newTestRouter(t)
	w := do(r, http.MethodPost, "/api/users", johnPayload())
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())

	w = do(r, http.MethodGet, "/api/users", "")
	require.Equal(t, http.StatusOK, w.Code)

	var payload struct {
		Status string `json:"status"`
		Data   []struct {
			Username string `json:"username"`
		} `json:"data"`
	}
	require.NoError(t, jsonv2.Unmarshal(w.Body.Bytes(), &payload))
	assert.Equal(t, "success", payload.Status)
	require.NotEmpty(t, payload.Data)
	assert.True(t, strings.HasPrefix(payload.Data[0].Username, "john"))
}

func TestGetUser(t *testing.T) {
	r := newTestRouter(t)
	w := do(r, http.MethodPost, "/api/users", johnPayload())
	id, _ := decodeData(t, w)["id"].(string)
	require.NotEmpty(t, id)

	w = do(r, http.MethodGet, "/api/users/"+id, "")
	require.Equal(t, http.StatusOK, w.Code)

	data := decodeData(t, w)
	assert.True(t, strings.HasPrefix(data["username"].(string), "john"))
}

func TestGetUserNotFound(t *testing.T) {
	r := newTestRouter(t)
	w := do(r, http.MethodGet, "/api/users/99", "")

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestGetUserInvalidID(t *testing.T) {
	r := newTestRouter(t)
	// A valid UUID suffix without the user_ prefix must 404, not 500.
	w := do(r, http.MethodGet, "/api/users/0199aaaa-bbbb-cccc-dddd-eeeeeeeeeeee", "")

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestUserMethodNotAllowed(t *testing.T) {
	r := newTestRouter(t)
	// PATCH is not part of the admin surface (PUT is).
	w := do(r, http.MethodPatch, "/api/users/1", "")

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}
