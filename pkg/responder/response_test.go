package responder

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	jsonv2 "encoding/json/v2"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// decodeEnvelope decodes the response body.
func decodeEnvelope(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	require.NoError(t, jsonv2.Unmarshal(w.Body.Bytes(), &body))
	return body
}

func TestWriteJSON(t *testing.T) {
	w := httptest.NewRecorder()
	WriteJSON(w, http.StatusOK, map[string]string{"ok": "yes"})

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var body map[string]string
	require.NoError(t, jsonv2.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "yes", body["ok"])
}

func TestSuccessEnvelope(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/users", nil)

	Success(w, r, http.StatusOK, map[string]string{"name": "alice"})

	assert.Equal(t, http.StatusOK, w.Code)

	body := decodeEnvelope(t, w)
	assert.Equal(t, "success", body["status"])
	assert.NotContains(t, body, "message")
	assert.NotContains(t, body, "error")
	assert.NotContains(t, body, "links")

	data, ok := body["data"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "alice", data["name"])

	meta, ok := body["metadata"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, float64(http.StatusOK), meta["status_code"])
	assert.NotEmpty(t, meta["request_id"])
	assert.NotContains(t, meta, "trace_id")
	assert.NotContains(t, meta, "page")
}

func TestSuccessEchoesRequestID(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/users", nil)
	r.Header.Set("X-Request-Id", "req-123")

	Success(w, r, http.StatusOK, nil)

	assert.Equal(t, "req-123", w.Header().Get("X-Request-Id"))
	meta := decodeEnvelope(t, w)["metadata"].(map[string]any)
	assert.Equal(t, "req-123", meta["request_id"])
}

func TestSuccessGeneratesRequestID(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/users", nil)

	Success(w, r, http.StatusOK, nil)

	id := w.Header().Get("X-Request-Id")
	assert.True(t, strings.HasPrefix(id, "req_"), id)
	assert.Len(t, id, len("req_")+26)
	meta := decodeEnvelope(t, w)["metadata"].(map[string]any)
	assert.Equal(t, id, meta["request_id"])
}

func TestSuccessWithTraceIDAndRateLimitHeaders(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/users", nil)
	r.Header.Set("X-Trace-Id", "trace-abc")
	w.Header().Set("X-RateLimit-Limit", "100")
	w.Header().Set("X-RateLimit-Remaining", "42")
	w.Header().Set("X-RateLimit-Reset", "1700000000")

	Success(w, r, http.StatusOK, nil)

	meta := decodeEnvelope(t, w)["metadata"].(map[string]any)
	assert.Equal(t, "trace-abc", meta["trace_id"])
	assert.Equal(t, float64(100), meta["rate_limit"].(map[string]any)["limit"])
	assert.Equal(t, float64(42), meta["rate_limit"].(map[string]any)["remaining"])
	assert.Equal(t, float64(1700000000), meta["rate_limit"].(map[string]any)["reset"])
}

func TestSuccessWithOptions(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/users", nil)

	Success(w, r, http.StatusCreated, nil,
		WithMessage("user created"),
		WithLinks(Links{"self": new("/api/users/1"), "next": nil}),
		WithPagination(NewPagination(PaginationParams{Page: 2, Limit: 10}, 35)),
	)

	body := decodeEnvelope(t, w)
	assert.Equal(t, "user created", body["message"])

	links := body["links"].(map[string]any)
	assert.Equal(t, "/api/users/1", links["self"])
	assert.Nil(t, links["next"])

	meta := body["metadata"].(map[string]any)
	assert.Equal(t, float64(2), meta["page"])
	assert.Equal(t, float64(10), meta["limit"])
	assert.Equal(t, float64(4), meta["total_pages"])
	assert.Equal(t, float64(35), meta["total_items"])
	assert.Equal(t, float64(10), meta["first_item_index"])
	assert.Equal(t, float64(19), meta["last_item_index"])
}

func TestWithLinkAppends(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/users", nil)

	Success(w, r, http.StatusCreated, nil,
		WithLink("self", "/api/users/1"),
		WithLink("related", "/api/users/1/profile"),
	)

	links := decodeEnvelope(t, w)["links"].(map[string]any)
	assert.Len(t, links, 2)
	assert.Equal(t, "/api/users/1", links["self"])
}

func TestFailEnvelope(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/users", nil)

	Fail(w, r, http.StatusUnprocessableEntity, "validation failed",
		WithError(map[string]any{"field": "email", "reason": "invalid format"}))

	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)

	body := decodeEnvelope(t, w)
	assert.Equal(t, "error", body["status"])
	assert.Equal(t, "validation failed", body["message"])
	assert.NotContains(t, body, "data")

	errDetail := body["error"].(map[string]any)
	assert.Equal(t, "email", errDetail["field"])

	meta := body["metadata"].(map[string]any)
	assert.Equal(t, float64(http.StatusUnprocessableEntity), meta["status_code"])
	assert.NotEmpty(t, meta["request_id"])
}

func TestFailWithoutErrorDetail(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/users", nil)

	Fail(w, r, http.StatusInternalServerError, "internal server error")

	body := decodeEnvelope(t, w)
	assert.NotContains(t, body, "error")
	assert.Equal(t, "internal server error", body["message"])
}

func TestNotFoundJSON(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/missing", nil)
	NotFoundJSON(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)

	body := decodeEnvelope(t, w)
	assert.Equal(t, "error", body["status"])
	assert.Equal(t, "not found", body["message"])
}

func TestMethodNotAllowedJSON(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/users", nil)
	MethodNotAllowedJSON(w, req)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)

	body := decodeEnvelope(t, w)
	assert.Equal(t, "error", body["status"])
	assert.Equal(t, "method not allowed", body["message"])
}

func TestBadRequestJSON(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/users", nil)
	BadRequestJSON(w, req, "name is required")

	assert.Equal(t, http.StatusBadRequest, w.Code)

	body := decodeEnvelope(t, w)
	assert.Equal(t, "error", body["status"])
	assert.Equal(t, "name is required", body["message"])
}
