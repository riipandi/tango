package health_test

import (
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/riipandi/tango/internal/health"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// nonEmptyLines splits output into lines, dropping blank ones.
func nonEmptyLines(s string) []string {
	var lines []string
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// getHealth calls the handler and returns the response and its decoded body.
func getHealth(t *testing.T, checker *health.Checker) (*http.Response, map[string]any) {
	t.Helper()

	recorder := httptest.NewRecorder()
	health.Handler(checker).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/health", nil))

	response := recorder.Result()
	t.Cleanup(func() { require.NoError(t, response.Body.Close()) })

	var body map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	return response, body
}

func TestHandlerAnswers200WhenHealthy(t *testing.T) {
	checker := health.NewChecker(health.WithCheck(health.Check{Name: "postgres", Check: passing}))

	response, body := getHealth(t, checker)

	assert.Equal(t, http.StatusOK, response.StatusCode)
	assert.Equal(t, "success", body["status"])
	assert.Contains(t, body, "data")
	assert.Contains(t, body, "metadata")

	// The payload reports the global status, which is a different vocabulary
	// from a component: the system is healthy, a component is up.
	data, ok := body["data"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "healthy", data["status"])

	// The body must be the standard envelope, so a client parses it the same
	// way it parses every other response.
	metadata, ok := body["metadata"].(map[string]any)
	require.True(t, ok, "metadata must be an object")
	assert.Equal(t, float64(http.StatusOK), metadata["status_code"])
	assert.NotEmpty(t, metadata["request_id"])
}

// A failing check must answer 503, so a load balancer can act on the status
// code alone without parsing the body.
func TestHandlerAnswers503WhenDown(t *testing.T) {
	checker := health.NewChecker(health.WithCheck(health.Check{Name: "postgres", Check: failing}))

	response, body := getHealth(t, checker)

	assert.Equal(t, http.StatusServiceUnavailable, response.StatusCode)
	assert.Equal(t, "error", body["status"])

	// The message names the failing check, so a reader knows what to look at
	// without opening the details. It is the same one-line message the CLI
	// prints, so the two surfaces describe a failure the same way.
	message, ok := body["message"].(string)
	require.True(t, ok, "message must be a string")
	assert.Contains(t, message, "unhealthy")
	assert.Contains(t, message, "postgres")

	metadata, ok := body["metadata"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, float64(http.StatusServiceUnavailable), metadata["status_code"])
}

// The failing detail must reach the response, so a client can show why.
func TestHandlerIncludesFailureDetail(t *testing.T) {
	checker := health.NewChecker(health.WithCheck(health.Check{Name: "postgres", Check: failing}))

	_, body := getHealth(t, checker)

	detail, ok := body["error"].(map[string]any)
	require.True(t, ok, "error must carry the result object")
	assert.Equal(t, "unhealthy", detail["status"])

	postgres := detailFor(t, detail, "postgres")
	assert.Equal(t, "down", postgres["status"])
	assert.Equal(t, "boom", postgres["error"])
	assert.NotEmpty(t, postgres["timestamp"])

	// Durations are published in milliseconds, not nanoseconds: a probe result
	// is meant to be read.
	assert.Contains(t, postgres, "duration_ms")
	assert.NotContains(t, postgres, "duration")
}

// detailFor finds one check in a decoded result. Details are an ordered array,
// not a map, so a diff of two responses does not churn.
func detailFor(t *testing.T, result map[string]any, name string) map[string]any {
	t.Helper()

	details, ok := result["details"].([]any)
	require.True(t, ok, "details must be an array")
	for _, entry := range details {
		detail, ok := entry.(map[string]any)
		require.True(t, ok, "each detail must be an object")
		if detail["name"] == name {
			return detail
		}
	}
	t.Fatalf("check %q missing from details %v", name, details)
	return nil
}

// The details array must keep a stable order, so the same state always produces
// the same body.
func TestHandlerOrdersDetailsByName(t *testing.T) {
	checker := health.NewChecker(health.WithChecks(
		health.Check{Name: "valkey", Check: passing},
		health.Check{Name: "postgres", Check: passing},
	))

	_, body := getHealth(t, checker)

	details, ok := body["data"].(map[string]any)["details"].([]any)
	require.True(t, ok, "details must be an array")

	var names []string
	for _, entry := range details {
		detail, ok := entry.(map[string]any)
		require.True(t, ok)
		names = append(names, detail["name"].(string))
	}
	assert.Equal(t, []string{"postgres", "valkey"}, names)
}

// The info block identifies the build that answered the probe.
func TestHandlerPublishesInfo(t *testing.T) {
	checker := health.NewChecker(
		health.WithCheck(health.Check{Name: "postgres", Check: passing}),
		health.WithInfo(map[string]string{"name": "tango", "version": "1.2.3"}),
	)

	_, body := getHealth(t, checker)

	data, ok := body["data"].(map[string]any)
	require.True(t, ok)
	info, ok := data["info"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "tango", info["name"])
	assert.Equal(t, "1.2.3", info["version"])
}

// An optional dependency that is down must not fail the endpoint, or a missing
// optional backend would take the service out of rotation.
func TestHandlerIgnoresOptionalFailure(t *testing.T) {
	checker := health.NewChecker(health.WithChecks(
		health.Check{Name: "postgres", Check: passing},
		health.Check{Name: "valkey", Check: failing, Optional: true},
	))

	response, body := getHealth(t, checker)

	assert.Equal(t, http.StatusOK, response.StatusCode)
	assert.Equal(t, "success", body["status"])
}

// A cached health response would keep reporting a stale status, so the handler
// must mark it uncacheable.
func TestHandlerDisablesCaching(t *testing.T) {
	checker := health.NewChecker(health.WithCheck(health.Check{Name: "postgres", Check: passing}))

	response, _ := getHealth(t, checker)

	assert.Equal(t, "no-store", response.Header.Get("Cache-Control"))
	assert.Equal(t, "no-cache", response.Header.Get("Pragma"))
	assert.Equal(t, "0", response.Header.Get("Expires"))
}

// The handler must serve concurrent probes without racing on the cached state.
func TestHandlerServesConcurrentRequests(t *testing.T) {
	checker := health.NewChecker(
		health.WithCacheTTL(0),
		health.WithCheck(health.Check{Name: "postgres", Check: passing}),
	)
	handler := health.Handler(checker)

	done := make(chan struct{})
	for range 8 {
		go func() {
			defer func() { done <- struct{}{} }()
			for range 10 {
				recorder := httptest.NewRecorder()
				handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/health", nil))
				assert.Equal(t, http.StatusOK, recorder.Code)
			}
		}()
	}
	for range 8 {
		<-done
	}
}

func TestMessageNamesFailingChecks(t *testing.T) {
	result := health.Result{
		Status: health.GlobalUnhealthy,
		Details: map[string]health.CheckResult{
			"postgres": {Name: "postgres", Status: health.StatusDown, Error: "boom"},
			"valkey":   {Name: "valkey", Status: health.StatusDown, Error: "boom"},
		},
	}

	assert.Equal(t, "unhealthy: postgres, valkey is down", health.Message(result))
}

func TestMessageReportsHealthyStatus(t *testing.T) {
	result := health.Result{Status: health.GlobalHealthy, Details: map[string]health.CheckResult{}}

	assert.Equal(t, "healthy (0 s)", health.Message(result))
}

// WriteText is the CLI's default output, so its shape is asserted exactly: a
// stable shape is what makes the output parseable by eye and by a log reader.
func TestWriteTextHealthyReport(t *testing.T) {
	result := health.Result{
		Status:   health.GlobalHealthy,
		Duration: 1500 * time.Millisecond,
		Details: map[string]health.CheckResult{
			"postgres": {
				Name:      "postgres",
				Status:    health.StatusUp,
				Timestamp: time.Now().Add(-2 * time.Second),
				Duration:  235 * time.Microsecond,
			},
		},
		Info: map[string]string{"name": "tango", "version": "1.2.3"},
	}

	var out strings.Builder
	require.NoError(t, health.WriteText(&out, result))

	assert.Equal(t, `name: tango
version: 1.2.3
status: healthy
duration: 1.5 s
checks: 1 up, 0 down

CHECK     STATUS  DURATION  CHECKED        ERROR
postgres  up      235 µs    2 seconds ago
`, out.String())
}

// A failing check must appear in the table with its error, and the summary must
// name it, so a reader knows what is wrong without extra tooling.
func TestWriteTextFailingReport(t *testing.T) {
	result := health.Result{
		Status: health.GlobalUnhealthy,
		Details: map[string]health.CheckResult{
			"postgres": {
				Name:     "postgres",
				Status:   health.StatusDown,
				Error:    "connection refused",
				Duration: 2 * time.Millisecond,
			},
			"valkey": {Name: "valkey", Status: health.StatusUp, Optional: true, Duration: time.Millisecond},
		},
	}

	var out strings.Builder
	require.NoError(t, health.WriteText(&out, result))

	assert.Contains(t, out.String(), "status: unhealthy")
	assert.Contains(t, out.String(), "checks: 1 up, 1 down, 1 optional")
	assert.Contains(t, out.String(), "postgres  down           2 ms      never    connection refused")
	assert.Contains(t, out.String(), "valkey    up (optional)  1 ms      never")
}

// Output must not carry trailing spaces: they are noise in a diff and in a
// copied line. tabwriter pads columns, so this is a real risk.
func TestWriteTextHasNoTrailingWhitespace(t *testing.T) {
	result := health.Result{
		Status: health.GlobalHealthy,
		Details: map[string]health.CheckResult{
			"a-short":           {Name: "a-short", Status: health.StatusUp},
			"a-much-longer-one": {Name: "a-much-longer-one", Status: health.StatusUp},
		},
	}

	var out strings.Builder
	require.NoError(t, health.WriteText(&out, result))

	for _, line := range strings.Split(out.String(), "\n") {
		assert.Equal(t, strings.TrimRight(line, " \t"), line, "line has trailing whitespace: %q", line)
	}
}

// The table must be aligned, so a reader can scan the columns. tabwriter pads
// each column to the widest cell.
func TestWriteTextAlignsColumns(t *testing.T) {
	result := health.Result{
		Status: health.GlobalHealthy,
		Details: map[string]health.CheckResult{
			"a":              {Name: "a", Status: health.StatusUp},
			"a-longer-check": {Name: "a-longer-check", Status: health.StatusUp},
		},
	}

	var out strings.Builder
	require.NoError(t, health.WriteText(&out, result))

	lines := nonEmptyLines(out.String())
	header := tableHeaderIndex(t, lines)
	require.Greater(t, header, 0, "header must hold a STATUS column")

	rows := lines[headerIndex(lines)+1:]
	require.Len(t, rows, 2)
	for _, line := range rows {
		assert.Equal(t, header, strings.Index(line, "up"), "STATUS column is not aligned: %q", line)
	}
}

// headerIndex returns the position of the table header, which is the only line
// that starts with the CHECK column name.
func headerIndex(lines []string) int {
	for i, line := range lines {
		if strings.HasPrefix(line, "CHECK") {
			return i
		}
	}
	return -1
}

// tableHeaderIndex returns the column position of STATUS in the table header.
func tableHeaderIndex(t *testing.T, lines []string) int {
	t.Helper()

	index := headerIndex(lines)
	require.NotEqual(t, -1, index, "table header missing from:\n%s", strings.Join(lines, "\n"))
	return strings.Index(lines[index], "STATUS")
}

// A result with no checks must still print a summary, so an empty checker does
// not produce an empty report.
func TestWriteTextWithoutChecks(t *testing.T) {
	var out strings.Builder
	require.NoError(t, health.WriteText(&out, health.Result{Status: health.GlobalHealthy}))

	assert.Contains(t, out.String(), "status: healthy")
	assert.NotContains(t, out.String(), "CHECK")
}

// Duration must be humanized, not raw nanoseconds: "1.5 s" is readable where
// "1500000000" is not.
func TestWriteTextHumanizesDurations(t *testing.T) {
	result := health.Result{
		Status:   health.GlobalHealthy,
		Duration: 1500 * time.Millisecond,
		Details: map[string]health.CheckResult{
			"postgres": {Name: "postgres", Status: health.StatusUp, Duration: 235 * time.Microsecond},
		},
	}

	var out strings.Builder
	require.NoError(t, health.WriteText(&out, result))

	assert.Contains(t, out.String(), "duration: 1.5 s")
	assert.Contains(t, out.String(), "235 µs")
}

// Timestamps must be humanized, so a reader sees how long ago a check ran
// instead of comparing raw timestamps by eye.
func TestWriteTextHumanizesTimestamps(t *testing.T) {
	result := health.Result{
		Status: health.GlobalHealthy,
		Details: map[string]health.CheckResult{
			"postgres": {Name: "postgres", Status: health.StatusUp, Timestamp: time.Now().Add(-2 * time.Hour)},
		},
	}

	var out strings.Builder
	require.NoError(t, health.WriteText(&out, result))

	assert.Contains(t, out.String(), "2 hours ago")
}

// WriteShort is the --short output: one word, so a shell script reads it without
// parsing anything.
func TestWriteShortPrintsOnlyTheStatus(t *testing.T) {
	tests := []struct {
		status health.GlobalStatus
		want   string
	}{
		{status: health.GlobalHealthy, want: "healthy\n"},
		{status: health.GlobalUnhealthy, want: "unhealthy\n"},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			var out strings.Builder
			require.NoError(t, health.WriteShort(&out, health.Result{
				Status: tt.status,
				Details: map[string]health.CheckResult{
					"postgres": {Name: "postgres", Status: health.StatusDown, Error: "boom"},
				},
			}))

			assert.Equal(t, tt.want, out.String())
		})
	}
}
