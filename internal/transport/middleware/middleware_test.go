package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lltest "go.loglayer.dev/transports/testing/v3"
	"go.loglayer.dev/v3"
)

func testRouter(log *loglayer.LogLayer, handler http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		RequestLogger(log)(JSONRecoverer(CORS()(handler))).ServeHTTP(w, req)
	})
}

func TestRequestLoggerServesInfo(t *testing.T) {
	lib := &lltest.TestLoggingLibrary{}
	log := loglayer.New(loglayer.Config{Transport: lltest.New(lltest.Config{Library: lib})})

	w := httptest.NewRecorder()
	testRouter(log, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/users", nil))

	line := lib.GetLastLine()
	require.NotNil(t, line)
	assert.Equal(t, "info", line.Level.String())
	assert.Equal(t, "request served", line.Messages[0])
	assert.NotEmpty(t, line.Metadata)
}

func TestRequestLoggerEscalatesLevel(t *testing.T) {
	lib := &lltest.TestLoggingLibrary{}
	log := loglayer.New(loglayer.Config{Transport: lltest.New(lltest.Config{Library: lib})})

	// 4xx responses log at warning level.
	testRouter(log, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/missing", nil))
	assert.Equal(t, "warn", lib.GetLastLine().Level.String())

	// 5xx responses log at error level.
	testRouter(log, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/boom", nil))
	assert.Equal(t, "error", lib.GetLastLine().Level.String())
}

func TestJSONRecovererAPI(t *testing.T) {
	r := http.NewServeMux()
	r.Handle("/api/panic", JSONRecoverer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	})))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/panic", nil))

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "application/json")
	assert.Contains(t, w.Body.String(), "internal server error")
}

func TestJSONRecovererNonAPI(t *testing.T) {
	r := http.NewServeMux()
	r.Handle("/panic", JSONRecoverer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	})))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/panic", nil))

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), http.StatusText(http.StatusInternalServerError))
}

// TestCORSPreflight pins the preflight contract: a first-party RPC
// client must be able to send its credential and protocol headers
// cross-origin. The previous allowlist omitted Authorization and
// X-API-KEY, so a cross-origin browser client could not authenticate.
func TestCORSPreflight(t *testing.T) {
	handler := CORS()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	// The header set @connectrpc/connect sends, plus the harness
	// credential headers.
	requested := "content-type,connect-protocol-version,connect-timeout-ms," +
		"connect-accept-encoding,connect-content-encoding,authorization,x-api-key"

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/api", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", requested)
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	// Wildcard CORS returns a literal "*" when credentials are disabled.
	assert.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))
	// A rejected header aborts the preflight and writes no
	// Allow-Headers, which is exactly how the missing Authorization
	// header failed before.
	allowed := w.Header().Get("Access-Control-Allow-Headers")
	require.NotEmpty(t, allowed, "every requested header must be allowed")
	for _, header := range []string{
		"Authorization", "X-Api-Key", "Connect-Protocol-Version",
		"Connect-Timeout-Ms", "Connect-Accept-Encoding", "Connect-Content-Encoding",
	} {
		assert.Contains(t, allowed, header)
	}
}

// TestCORSPreflightRejectsUnknownHeader pins the allowlist is still a
// filter, not a wildcard: an unlisted header must abort the preflight.
func TestCORSPreflightRejectsUnknownHeader(t *testing.T) {
	handler := CORS()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/api", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "x-not-a-real-header")
	handler.ServeHTTP(w, req)

	assert.Empty(t, w.Header().Get("Access-Control-Allow-Headers"))
}
