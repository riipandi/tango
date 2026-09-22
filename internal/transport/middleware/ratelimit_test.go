package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"

	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/pkg/testutils"
)

// valkeyClient opens the backend client a KV limiter test runs against.
func valkeyClient(url string) (valkey.Client, error) {
	opt, err := valkey.ParseURL(url)
	if err != nil {
		return nil, err
	}
	return valkey.NewClient(opt)
}

// stubLimiter is a Limiter a test answers with, recording the keys it was
// asked about.
type stubLimiter struct {
	keys   []string
	result Result
	err    error
}

func (s *stubLimiter) Allow(_ context.Context, key string) (Result, error) {
	s.keys = append(s.keys, key)
	return s.result, s.err
}

func TestRateLimitWritesTheHeadersAClientPacesBy(t *testing.T) {
	reset := time.Now().Add(time.Minute).Truncate(time.Second)
	limiter := &stubLimiter{result: Result{Limit: 60, Remaining: 59, ResetAt: reset}}
	handler := RateLimit(limiter)(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusAccepted)
		}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api", nil)
	req.RemoteAddr = "192.0.2.1:4711"
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusAccepted, rec.Code)
	assert.Equal(t, "60", rec.Header().Get(RateLimitLimitHeader))
	assert.Equal(t, "59", rec.Header().Get(RateLimitRemainingHeader))
	assert.Equal(t, strconv.FormatInt(reset.Unix(), 10), rec.Header().Get(RateLimitResetHeader))

	// The key is the address without its port, reduced to the alphabet the
	// rate_limits check allows.
	assert.Equal(t, []string{"ip_192_0_2_1"}, limiter.keys)
}

func TestRateLimitRefusesAStudentWhoSpentTheWindow(t *testing.T) {
	limiter := &stubLimiter{result: Result{
		Limited: true, Limit: 60, Remaining: 0,
		RetryAfter: 30 * time.Second,
	}}
	handler := RateLimit(limiter)(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			require.Fail(t, "a limited request must not reach the route")
		}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api", nil))

	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
	assert.Equal(t, "30", rec.Header().Get(RateLimitRetryHeader))
	assert.Equal(t, "0", rec.Header().Get(RateLimitRemainingHeader))

	var body struct {
		Status string `json:"status"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "error", body.Status)
}

func TestRateLimitLetsTheRequestThroughWhenTheBackendCannotAnswer(t *testing.T) {
	limiter := &stubLimiter{err: context.DeadlineExceeded}
	handler := RateLimit(limiter)(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api", nil))

	// A limiter that cannot answer is a degraded protection, not a downed
	// service: the request passes, the headers stay unset.
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, rec.Header().Get(RateLimitLimitHeader))
}

func TestRateLimitWithoutALimiterIsAPassThrough(t *testing.T) {
	handler := RateLimit(nil)(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestRateLimitSparesTheExcludedPrefixes(t *testing.T) {
	limiter := &stubLimiter{}
	handler := RateLimit(limiter, "/api/healthz")(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/healthz", nil))
	// A prefix matches the paths under it.
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/healthz/deep", nil))

	assert.Empty(t, limiter.keys, "an excluded path reaches no check at all, not merely one it would pass")

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/healthcheck", nil))
	assert.Equal(t, []string{"ip_192_0_2_1"}, limiter.keys,
		"a path that merely shares a prefix character is still counted")
}

func TestRetryAfterFromDetailPrefersTheFunctionHint(t *testing.T) {
	detail := "Key: ip_1, Count: 61, Limit: 60, Retry after: 42 seconds"
	assert.Equal(t, 42*time.Second, retryAfterFromDetail(detail, time.Minute))
}

func TestRetryAfterFromDetailFallsBackToTheWindow(t *testing.T) {
	assert.Equal(t, time.Minute, retryAfterFromDetail("no hint", time.Minute))
}

// migratedPool applies the migrations to a fresh test database and returns
// the pool the limiter runs on. The same idiom the queue tests use.
func migratedPool(t *testing.T) *datastore.Postgres {
	t.Helper()

	container := testutils.StartPostgres(t.Context(), t)
	dsn := container.NewDatabase(t)

	migrationDB, err := datastore.OpenMigrationDB(t.Context(), datastore.PostgresOptions{DSN: dsn})
	require.NoError(t, err)
	migrator, err := database.NewMigrator(t.Context(), migrationDB, database.MigratorOptions{})
	require.NoError(t, err)
	_, err = migrator.Up(t.Context())
	require.NoError(t, err)
	require.NoError(t, migrationDB.Close())

	pool, err := datastore.NewPostgres(t.Context(), datastore.PostgresOptions{
		DSN:             dsn,
		ApplicationName: "ratelimit_test",
	})
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

func TestDatabaseLimiterCountsTheWindow(t *testing.T) {
	testutils.SkipWithoutDocker(t)

	cfg := config.RateLimit{Driver: config.RateLimitDB, Limit: 2, Window: time.Minute}
	limiter := NewDatabaseLimiter(migratedPool(t), cfg)

	first, err := limiter.Allow(t.Context(), "ip_192_0_2_7")
	require.NoError(t, err)
	assert.False(t, first.Limited)
	assert.Equal(t, 2, first.Limit)
	assert.Equal(t, 1, first.Remaining)

	second, err := limiter.Allow(t.Context(), "ip_192_0_2_7")
	require.NoError(t, err)
	assert.Equal(t, 0, second.Remaining)

	third, err := limiter.Allow(t.Context(), "ip_192_0_2_7")
	require.NoError(t, err)
	assert.True(t, third.Limited)
	assert.Equal(t, 0, third.Remaining)
	assert.Greater(t, third.RetryAfter, time.Duration(0), "the function's own retry hint")
}

func TestDatabaseLimiterKeysAreIndependent(t *testing.T) {
	testutils.SkipWithoutDocker(t)

	cfg := config.RateLimit{Driver: config.RateLimitDB, Limit: 1, Window: time.Minute}
	limiter := NewDatabaseLimiter(migratedPool(t), cfg)

	_, err := limiter.Allow(t.Context(), "ip_192_0_2_7")
	require.NoError(t, err)

	other, err := limiter.Allow(t.Context(), "ip_192_0_2_8")
	require.NoError(t, err)
	assert.False(t, other.Limited, "a second client starts its own window")
}

func TestKVStoreLimiterCountsTheWindow(t *testing.T) {
	testutils.SkipWithoutDocker(t)

	container := testutils.StartValkey(t.Context(), t)
	client, err := valkeyClient(container.URL)
	require.NoError(t, err)
	t.Cleanup(client.Close)

	cfg := config.RateLimit{Driver: config.RateLimitKV, Limit: 2, Window: time.Minute}
	limiter := NewKVStoreLimiter(client, cfg)
	key := "ip_" + sanitizeKey(strings.ToLower(t.Name()))

	first, err := limiter.Allow(t.Context(), key)
	require.NoError(t, err)
	assert.False(t, first.Limited)
	assert.Equal(t, 1, first.Remaining)
	assert.False(t, first.ResetAt.IsZero())

	second, err := limiter.Allow(t.Context(), key)
	require.NoError(t, err)
	assert.Equal(t, 0, second.Remaining)

	third, err := limiter.Allow(t.Context(), key)
	require.NoError(t, err)
	assert.True(t, third.Limited)
	assert.Greater(t, third.RetryAfter, time.Duration(0), "the window has time left")
}

func TestKVStoreLimiterExpiresTheWindow(t *testing.T) {
	testutils.SkipWithoutDocker(t)

	container := testutils.StartValkey(t.Context(), t)
	client, err := valkeyClient(container.URL)
	require.NoError(t, err)
	t.Cleanup(client.Close)

	cfg := config.RateLimit{Driver: config.RateLimitKV, Limit: 1, Window: 2 * time.Second}
	limiter := NewKVStoreLimiter(client, cfg)
	key := "ip_" + sanitizeKey(strings.ToLower(t.Name()))

	exhausted, err := limiter.Allow(t.Context(), key)
	require.NoError(t, err)
	require.False(t, exhausted.Limited)

	limited, err := limiter.Allow(t.Context(), key)
	require.NoError(t, err)
	require.True(t, limited.Limited)

	time.Sleep(3 * time.Second)

	renewed, err := limiter.Allow(t.Context(), key)
	require.NoError(t, err)
	assert.False(t, renewed.Limited, "a spent window starts again once it expires")
}
