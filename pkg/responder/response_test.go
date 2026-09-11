package responder

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteJSON(t *testing.T) {
	w := httptest.NewRecorder()
	WriteJSON(w, http.StatusOK, map[string]string{"ok": "yes"})

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "yes", body["ok"])
}

func TestNotFoundJSON(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/missing", nil)
	NotFoundJSON(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)

	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "not found", body["error"])
	assert.Equal(t, "/missing", body["path"])
}

func TestMethodNotAllowedJSON(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/users", nil)
	MethodNotAllowedJSON(w, req)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)

	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, http.MethodDelete, body["method"])
}

func TestBadRequestJSON(t *testing.T) {
	w := httptest.NewRecorder()
	BadRequestJSON(w, "name is required")

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "name is required", body["error"])
}
