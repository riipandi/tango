package fetcher

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/riipandi/tango/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

	f := New(Options{BaseURL: "http://localhost:3000"})
	defer f.Close()

	_, err := f.GetJSON(t.Context(), server.URL, nil)
	require.NoError(t, err)

	want := fmt.Sprintf("%s/%s (+%s)", config.AppName, config.AppVersion, "http://localhost:3000")
	assert.Equal(t, want, received)
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

func TestSanitizeUserAgent(t *testing.T) {
	ua := config.AppName + "/1.0\r\nX-Evil: injected\x7f"
	assert.Equal(t, config.AppName+"/1.0X-Evil: injected", sanitizeUserAgent(ua))
	assert.NotContains(t, userAgent("http://localhost:3000"), "\n")
}
