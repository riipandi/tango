package fetcher

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/riipandi/tango/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lltest "go.loglayer.dev/transports/testing/v3"
	"go.loglayer.dev/v3"
	"resty.dev/v3"
)

func TestGetJSONDecodes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"tango"}`))
	}))
	defer server.Close()

	f := New(Options{BaseURL: "http://localhost:3000"})
	defer f.Close()

	var out struct{ Name string }
	resp, err := f.GetJSON(t.Context(), server.URL, &out)
	require.NoError(t, err)
	assert.True(t, resp.IsStatusSuccess())
	assert.Equal(t, "tango", out.Name)
}

func TestGetJSONSurfacesNon2xx(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	f := New(Options{})
	defer f.Close()

	var out any
	resp, err := f.GetJSON(t.Context(), server.URL, &out)
	require.ErrorContains(t, err, "unexpected status 503")
	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode())
}

func TestPostJSONSendsBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"action":"created"}`))
	}))
	defer server.Close()

	f := New(Options{})
	defer f.Close()

	var out struct{ Action string }
	_, err := f.PostJSON(t.Context(), server.URL, map[string]string{"action": "created"}, &out)
	require.NoError(t, err)
	assert.Equal(t, "created", out.Action)
}

func TestUserAgentHeader(t *testing.T) {
	var received string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = r.UserAgent()
	}))
	defer server.Close()

	f := New(Options{})
	defer f.Close()

	_, err := f.GetJSON(t.Context(), server.URL, nil)
	require.NoError(t, err)

	want := fmt.Sprintf("%s/%s", config.AppName, config.AppVersion)
	assert.Equal(t, want, received)
}

func TestBaseURLPrefixesRequests(t *testing.T) {
	var path string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	// A dedicated Fetcher for one upstream: call sites send paths.
	f := New(Options{BaseURL: server.URL})
	defer f.Close()

	_, err := f.GetJSON(t.Context(), "/users/42", nil)
	require.NoError(t, err)
	assert.Equal(t, "/users/42", path)
}

func TestTimeoutBoundsRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
	}))
	defer server.Close()

	f := New(Options{Timeout: 20 * time.Millisecond})
	defer f.Close()

	_, err := f.GetJSON(t.Context(), server.URL, nil)
	require.Error(t, err, "request must be bounded by the timeout")
}

// newLoggedFetcher builds a fetcher whose entries are captured by
// the LogLayer testing transport.
func newLoggedFetcher(t *testing.T, opts Options) (*Fetcher, *lltest.TestLoggingLibrary) {
	t.Helper()
	lib := &lltest.TestLoggingLibrary{}
	opts.Logger = loglayer.New(loglayer.Config{Transport: lltest.New(lltest.Config{Library: lib})})
	return New(opts), lib
}

func TestResponseMiddlewareLevels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/rejected":
			w.WriteHeader(http.StatusNotFound)
		case "/failed":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusOK)
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	cases := []struct {
		path       string
		status     int
		wantLevel  string
		wantPhrase string
	}{
		{"/ok", http.StatusOK, "debug", "outbound request served"},
		{"/rejected", http.StatusNotFound, "warn", "outbound request rejected"},
		{"/failed", http.StatusInternalServerError, "error", "outbound request failed"},
	}

	for _, tc := range cases {
		f, lib := newLoggedFetcher(t, Options{BaseURL: "http://localhost:3000"})

		var out map[string]any
		resp, gerr := f.GetJSON(t.Context(), server.URL+tc.path, &out)
		if tc.status < 400 {
			require.NoError(t, gerr, tc.path)
			assert.True(t, resp.IsStatusSuccess(), tc.path)
		} else {
			require.Error(t, gerr, tc.path)
		}

		line := lib.GetLastLine()
		assert.Equal(t, tc.wantLevel, line.Level.String(), tc.path)
		assert.Equal(t, tc.wantPhrase, line.Messages[0], tc.path)

		metadata, ok := line.Metadata.(loglayer.Metadata)
		require.True(t, ok, tc.path)
		assert.True(t, strings.HasSuffix(metadata["url"].(string), tc.path), tc.path)
		assert.Equal(t, tc.status, metadata["status"], tc.path)

		require.NoError(t, f.Close())
	}
}

func TestResponseMiddlewareLogsTransportFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := server.URL
	server.Close() // guarantee connection failure

	f, lib := newLoggedFetcher(t, Options{})

	var out any
	_, err := f.GetJSON(t.Context(), url, &out)
	require.Error(t, err)

	// Transport failures never reach the response middleware; the
	// OnError hook logs them.
	line := lib.GetLastLine()
	require.NotNil(t, line, "transport failure must be logged")
	assert.Equal(t, "error", line.Level.String())
	assert.Equal(t, "outbound request failed", line.Messages[0])

	require.NoError(t, f.Close())
}

func TestNilLoggerStaysSilent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer server.Close()

	f := New(Options{}) // no logger: silent mock

	var out any
	_, err := f.GetJSON(t.Context(), server.URL, &out)
	require.NoError(t, err)
	require.NoError(t, f.Close())
}

func TestRetriesIdempotentRequests(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&attempts, 1) < 3 { // fail twice, succeed third
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	f, lib := newLoggedFetcher(t, Options{
		Retries:          2,
		RetryWaitTime:    time.Millisecond,
		RetryMaxWaitTime: 2 * time.Millisecond,
	})
	defer f.Close()

	var out map[string]any
	resp, err := f.GetJSON(t.Context(), server.URL, &out)
	require.NoError(t, err, "third attempt must succeed")
	assert.Equal(t, int32(3), atomic.LoadInt32(&attempts))
	assert.Equal(t, 3, resp.Request.Attempt)

	// Retry hooks logged the two failed attempts (the final
	// response is logged separately by the response middleware).
	var retryLines int
	require.Eventually(t, func() bool {
		retryLines = 0
		for _, line := range lib.Lines() {
			if line.Messages[0] == "retrying outbound request" {
				retryLines++
			}
		}
		return retryLines == 2
	}, time.Second, 10*time.Millisecond)
	for _, line := range lib.Lines() {
		if line.Messages[0] == "retrying outbound request" {
			assert.Equal(t, "debug", line.Level.String())
		}
	}
}

func TestDoesNotRetryNonIdempotentByDefault(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	f, _ := newLoggedFetcher(t, Options{
		Retries:          3,
		RetryWaitTime:    time.Millisecond,
		RetryMaxWaitTime: time.Millisecond,
	})
	defer f.Close()

	_, err := f.PostJSON(t.Context(), server.URL, map[string]string{"k": "v"}, nil)
	require.Error(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(&attempts), "POST must not be retried by default")
}

func TestCircuitBreakerOpensFast(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	f, lib := newLoggedFetcher(t, Options{CircuitBreaker: true})
	defer f.Close()

	for range DefaultBreakerThreshold {
		_, _ = f.GetJSON(t.Context(), server.URL, nil)
	}
	require.Equal(t, int32(DefaultBreakerThreshold), atomic.LoadInt32(&attempts))

	// The open breaker rejects before any network call.
	_, err := f.GetJSON(t.Context(), server.URL, nil)
	require.ErrorIs(t, err, resty.ErrCircuitBreakerOpen)
	assert.Equal(t, int32(DefaultBreakerThreshold), atomic.LoadInt32(&attempts))

	// State transitions and triggers are observable in the log.
	var foundTrigger, foundStateChange bool
	for _, line := range lib.Lines() {
		switch line.Messages[0] {
		case "circuit breaker triggered":
			foundTrigger = true
		case "circuit breaker state changed":
			foundStateChange = true
		}
	}
	assert.True(t, foundTrigger, "trigger must be logged")
	assert.True(t, foundStateChange, "state change must be logged")
}
