package jobs

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	jsonv2 "encoding/json/v2"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/internal/fetcher"
	"github.com/riipandi/tango/internal/logger"
	"github.com/riipandi/tango/internal/mailer"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/pkg/antree"
	"github.com/riipandi/tango/pkg/testutils"
)

// testDB opens the shared container with migrations applied.
func testDB(t *testing.T) datastore.Store {
	t.Helper()
	ctx := t.Context()

	pg := testutils.StartPostgres(ctx, t)
	if _, err := database.MigrateUp(ctx, pg.DSN); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	db, err := datastore.New(ctx, datastore.Options{DSN: pg.DSN})
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	// The container is shared: never let this test's rows reach another.
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = db.Exec(bg, "DELETE FROM queue_tasks")
		_, _ = db.Exec(bg, "DELETE FROM queue_tasks_completed")
	})
	return db
}

func testQueue(t *testing.T, db datastore.Store) *antree.Client {
	t.Helper()
	client, err := antree.NewClient(antree.ClientConfig{
		DB:           db.Pool(),
		NumWorkers:   1,
		ReleaseAfter: time.Hour,
	})
	require.NoError(t, err)
	return client
}

// queuedTasks returns the payloads sitting in one queue.
func queuedTasks[T any](t *testing.T, db datastore.Store, queue string) []T {
	t.Helper()
	rows, err := db.Query(t.Context(),
		"SELECT task FROM queue_tasks WHERE queue = $1 ORDER BY id", queue)
	require.NoError(t, err)
	defer rows.Close()

	out := []T{}
	for rows.Next() {
		var payload []byte
		require.NoError(t, rows.Scan(&payload))
		var decoded T
		require.NoError(t, jsonv2.Unmarshal(payload, &decoded))
		out = append(out, decoded)
	}
	require.NoError(t, rows.Err())
	return out
}

// newUserRow inserts a user for FK-bound fixtures.
func newUserRow(t *testing.T, db datastore.Store) user.UserID {
	t.Helper()
	stamp := strconv.FormatInt(time.Now().UnixNano(), 10)
	created, err := user.NewPostgresStore(db).Create(t.Context(), user.CreateParams{
		Username: "jobs_" + stamp[len(stamp)-8:],
		Email:    "jobs-" + stamp + "@test.local",
	})
	require.NoError(t, err)
	return created.ID
}

func TestNewRegistryRegistersQueues(t *testing.T) {
	db := testDB(t)
	queue := testQueue(t, db)

	registry := NewRegistry(queue, &fakeMailer{}, logger.NewMock())
	require.NotNil(t, registry)

	// The email and maintenance queues must accept their task types.
	require.NoError(t, registry.EnqueueEmail(t.Context(), mailer.Message{
		To: "someone@example.com", Subject: "Hi", Template: "test-email",
	}))

	tasks := queuedTasks[EmailTask](t, db, EmailQueue)
	require.Len(t, tasks, 1)
	assert.Equal(t, "someone@example.com", tasks[0].To)
	assert.Equal(t, "Hi", tasks[0].Subject)
	assert.Equal(t, "test-email", tasks[0].Template)
}

func TestEnqueueEmailRequiresQueue(t *testing.T) {
	registry := &Registry{log: logger.NewMock()}
	err := registry.EnqueueEmail(context.Background(), mailer.Message{To: "a@b.c"})
	assert.ErrorContains(t, err, "queue is not configured")
}

func TestRegistryModuleContract(t *testing.T) {
	registry := &Registry{jobs: map[string]Job{}, log: logger.NewMock()}
	assert.Equal(t, "jobs", registry.Name())
	assert.NoError(t, registry.Stop(context.Background()))
}

func TestSetVersionFeedAndLatest(t *testing.T) {
	registry := &Registry{jobs: map[string]Job{}, log: logger.NewMock()}

	// Without a feed the registry reports the running build.
	assert.NotEmpty(t, registry.Latest())

	feed := &VersionFeed{Fetch: func(context.Context) (string, error) { return "v9.9.9", nil }}
	registry.SetVersionFeed(feed)
	require.NoError(t, feed.Refresh(context.Background()))
	assert.Equal(t, "9.9.9", registry.Latest())
}

func TestStartSchedulesEveryRegisteredJob(t *testing.T) {
	db := testDB(t)
	queue := testQueue(t, db)
	registry := NewRegistry(queue, &fakeMailer{}, logger.NewMock())

	registry.AddJob(Job{Name: "scheduled", Interval: time.Hour, Run: func(context.Context) error { return nil }})
	require.NoError(t, registry.Start(t.Context()))

	scheduled := queuedTasks[RecurringTask](t, db, MaintenanceQueue)
	require.Len(t, scheduled, 1)
	assert.Equal(t, "scheduled", scheduled[0].Job)
	assert.EqualValues(t, 3600, scheduled[0].IntervalSeconds)

	// The first run is delayed, never immediate.
	var future int
	require.NoError(t, db.QueryRow(t.Context(),
		"SELECT count(*) FROM queue_tasks WHERE queue = $1 AND wait_until > CURRENT_TIMESTAMP", MaintenanceQueue).Scan(&future))
	assert.Equal(t, 1, future, "the first run must be delayed by the jittered interval")

	// Start is one-shot: a second call must not double-schedule.
	require.NoError(t, registry.Start(t.Context()))
	assert.Len(t, queuedTasks[RecurringTask](t, db, MaintenanceQueue), 1)
}

func TestRunJobReschedulesAfterSuccess(t *testing.T) {
	db := testDB(t)
	queue := testQueue(t, db)
	registry := NewRegistry(queue, &fakeMailer{}, logger.NewMock())

	ran := 0
	registry.AddJob(Job{Name: "recurring", Interval: time.Minute, Run: func(context.Context) error {
		ran++
		return nil
	}})

	require.NoError(t, registry.runJob(t.Context(), RecurringTask{Job: "recurring", IntervalSeconds: 60}))
	assert.Equal(t, 1, ran)

	// The next run is enqueued even though this one returned no error.
	next := queuedTasks[RecurringTask](t, db, MaintenanceQueue)
	require.Len(t, next, 1)
	assert.Equal(t, "recurring", next[0].Job)
	assert.EqualValues(t, 60, next[0].IntervalSeconds)
}

// freezeAt pins the job clock so expired rows can be produced from
// rows that were valid when written. The DDL carries
// CHECK (expires_at > CURRENT_TIMESTAMP) — inherited from upstream —
// which is only evaluated at INSERT/UPDATE: a row becomes expired by
// the clock moving on. Holding the job clock in the past reproduces
// that state without waiting for wall time.
func freezeAt(t *testing.T, at time.Time) {
	t.Helper()
	original := now
	now = func() time.Time { return at }
	t.Cleanup(func() { now = original })
}

func TestCleanupTokensRemovesExpiredRows(t *testing.T) {
	db := testDB(t)
	ctx := t.Context()
	userID := newUserRow(t, db)

	// Token rows are written as valid, then the job clock moves two
	// hours ahead: auth_tokens, signup_tokens, and sessions are all
	// past their expiry at that point.
	inserted := time.Now().UTC()
	expired := inserted.Add(2 * time.Hour)
	live := inserted.Add(24 * time.Hour)

	_, err := db.Exec(ctx,
		`INSERT INTO public.auth_tokens (user_id, token_hash, purpose, expires_at) VALUES ($1, 'jobs-expired-auth', 'one_time_access', $2)`,
		userID.UUID(), expired)
	require.NoError(t, err)
	_, err = db.Exec(ctx,
		`INSERT INTO public.auth_tokens (user_id, token_hash, purpose, expires_at) VALUES ($1, 'jobs-live-auth', 'email_verification', $2)`,
		userID.UUID(), live)
	require.NoError(t, err)
	_, err = db.Exec(ctx,
		`INSERT INTO public.signup_tokens (token_hash, expires_at) VALUES ('jobs-expired-signup', $1)`, expired)
	require.NoError(t, err)
	_, err = db.Exec(ctx,
		`INSERT INTO public.sessions (id, user_id, provider, token_hash, expires_at) VALUES ('jobs-expired-session', $1, 'test', 'jobs-session-hash', $2)`,
		userID.UUID(), expired)
	require.NoError(t, err)
	_, err = db.Exec(ctx,
		`INSERT INTO public.device_login_requests (code, device_token_hash, expires_at) VALUES ('JOBS1234', 'jobs-device-hash', $1)`, expired)
	require.NoError(t, err)

	freezeAt(t, expired.Add(3*time.Hour))

	job := CleanupTokens(db, logger.NewMock())
	require.NoError(t, job.Run(ctx))

	// Every expired row is gone; the live one survives.
	for _, probe := range []struct {
		query string
		args  []any
		want  int
	}{
		{"SELECT count(*) FROM public.auth_tokens WHERE token_hash = 'jobs-expired-auth'", nil, 0},
		{"SELECT count(*) FROM public.auth_tokens WHERE token_hash = 'jobs-live-auth'", nil, 1},
		{"SELECT count(*) FROM public.signup_tokens WHERE token_hash = 'jobs-expired-signup'", nil, 0},
		{"SELECT count(*) FROM public.sessions WHERE id = 'jobs-expired-session'", nil, 0},
		{"SELECT count(*) FROM public.device_login_requests WHERE code = 'JOBS1234'", nil, 0},
	} {
		var got int
		require.NoError(t, db.QueryRow(ctx, probe.query, probe.args...).Scan(&got))
		assert.Equal(t, probe.want, got, probe.query)
	}
}

func TestCleanupTokensAlsoDropsRevokedSessions(t *testing.T) {
	db := testDB(t)
	ctx := t.Context()
	userID := newUserRow(t, db)

	_, err := db.Exec(ctx,
		`INSERT INTO public.sessions (id, user_id, provider, token_hash, expires_at, revoked_at)
		 VALUES ('jobs-revoked-session', $1, 'test', 'jobs-revoked-hash', $2, CURRENT_TIMESTAMP)`,
		userID.UUID(), time.Now().UTC().Add(time.Hour))
	require.NoError(t, err)

	require.NoError(t, CleanupTokens(db, logger.NewMock()).Run(ctx))

	var remaining int
	require.NoError(t, db.QueryRow(ctx,
		"SELECT count(*) FROM public.sessions WHERE id = 'jobs-revoked-session'").Scan(&remaining))
	assert.Zero(t, remaining, "a revoked session goes with the expired ones")
}

func TestCleanupTokensReportsNothingToDo(t *testing.T) {
	db := testDB(t)
	// No expired rows: the job still succeeds and stays quiet.
	require.NoError(t, CleanupTokens(db, logger.NewMock()).Run(t.Context()))
}

// stubPruner records the cutoff and returns a scripted count.
type stubPruner struct {
	cutoff  time.Time
	removed int64
	err     error
}

func (p *stubPruner) PruneLogs(_ context.Context, before time.Time) (int64, error) {
	p.cutoff = before
	return p.removed, p.err
}

func TestCleanupWebhookLogsUsesRetentionWindow(t *testing.T) {
	pruner := &stubPruner{removed: 3}
	before := time.Now().UTC()

	require.NoError(t, CleanupWebhookLogs(pruner, logger.NewMock()).Run(t.Context()))

	expected := before.Add(-WebhookLogRetention)
	assert.WithinDuration(t, expected, pruner.cutoff, time.Minute)
}

func TestCleanupWebhookLogsPropagatesFailure(t *testing.T) {
	pruner := &stubPruner{err: assert.AnError}
	err := CleanupWebhookLogs(pruner, logger.NewMock()).Run(t.Context())
	assert.ErrorIs(t, err, assert.AnError)
}

func TestRemindExpiringAPIKeysQueuesOnceAndMarksTheKey(t *testing.T) {
	db := testDB(t)
	ctx := t.Context()
	userID := newUserRow(t, db)

	// Inside the reminder window, unrevoked, unannounced.
	expiresAt := time.Now().UTC().Add(24 * time.Hour)
	_, err := db.Exec(ctx,
		`INSERT INTO public.api_keys (user_id, name, prefix, key_hash, expires_at)
		 VALUES ($1, 'Expiring Key', 'pik', $2, $3)`,
		userID.UUID(), []byte("jobs-key-hash"), expiresAt)
	require.NoError(t, err)

	freezeAt(t, time.Now().UTC())

	mail := &fakeEnqueuer{}
	job := RemindExpiringAPIKeys(db, mail, logger.NewMock())
	require.NoError(t, job.Run(ctx))

	sent := mail.sent()
	require.Len(t, sent, 1)
	assert.Equal(t, "api-key-expiring-soon", sent[0].Template)
	assert.Equal(t, "Expiring Key", sent[0].Data["APIKeyName"])

	// The marker makes the job idempotent inside the window.
	require.NoError(t, job.Run(ctx))
	assert.Len(t, mail.sent(), 1, "a reminded key must not be reminded again")
}

func TestRemindExpiringAPIKeysSkipsOutOfWindowKeys(t *testing.T) {
	db := testDB(t)
	ctx := t.Context()
	userID := newUserRow(t, db)

	// Both keys expire after the 7-day reminder window.
	for name, expires := range map[string]time.Time{
		"far-key":  time.Now().UTC().Add(60 * 24 * time.Hour),
		"near-key": time.Now().UTC().Add(40 * 24 * time.Hour),
	} {
		_, err := db.Exec(ctx,
			`INSERT INTO public.api_keys (user_id, name, prefix, key_hash, expires_at)
			 VALUES ($1, $2, 'pik', $3, $4)`,
			userID.UUID(), name, []byte("hash-"+name), expires)
		require.NoError(t, err)
	}

	// Move the clock 36 days ahead: only the near key falls inside
	// the window then.
	freezeAt(t, time.Now().UTC().Add(36*24*time.Hour))

	mail := &fakeEnqueuer{}
	require.NoError(t, RemindExpiringAPIKeys(db, mail, logger.NewMock()).Run(ctx))

	sent := mail.sent()
	require.Len(t, sent, 1, "only the key inside the window is reminded")
	assert.Equal(t, "near-key", sent[0].Data["APIKeyName"])
}

func TestRemindExpiringAPIKeysSkipsRevokedAndAnnounced(t *testing.T) {
	db := testDB(t)
	ctx := t.Context()
	userID := newUserRow(t, db)

	for _, fixture := range []struct {
		name      string
		revoked   bool
		announced bool
	}{
		{"revoked-key", true, false},
		{"announced-key", false, true},
	} {
		var revokedAt, announcedAt any
		if fixture.revoked {
			revokedAt = time.Now().UTC()
		}
		if fixture.announced {
			announcedAt = time.Now().UTC()
		}
		_, err := db.Exec(ctx,
			`INSERT INTO public.api_keys (user_id, name, prefix, key_hash, expires_at, revoked_at, expiration_email_sent_at)
			 VALUES ($1, $2, 'pik', $3, $4, $5, $6)`,
			userID.UUID(), fixture.name, []byte("hash-"+fixture.name),
			time.Now().UTC().Add(24*time.Hour), revokedAt, announcedAt)
		require.NoError(t, err)
	}

	mail := &fakeEnqueuer{}
	require.NoError(t, RemindExpiringAPIKeys(db, mail, logger.NewMock()).Run(ctx))
	assert.Empty(t, mail.sent(), "revoked and announced keys are not reminded")
}

func TestRemindExpiringAPIKeysWithoutMailerIsANoop(t *testing.T) {
	db := testDB(t)
	require.NoError(t, RemindExpiringAPIKeys(db, nil, logger.NewMock()).Run(t.Context()))
}

func TestVersionJobRefreshesTheFeed(t *testing.T) {
	feed := &VersionFeed{Fetch: func(context.Context) (string, error) { return "v3.0.0", nil }}

	job := VersionJob(feed, logger.NewMock())
	assert.Equal(t, "check_latest_version", job.Name)
	require.NoError(t, job.Run(t.Context()))
	assert.Equal(t, "3.0.0", feed.Latest())
}

func TestVersionJobSurfacesLookupFailure(t *testing.T) {
	feed := &VersionFeed{Fetch: func(context.Context) (string, error) { return "", assert.AnError }}
	err := VersionJob(feed, logger.NewMock()).Run(t.Context())
	assert.ErrorIs(t, err, assert.AnError)
}

func TestNewVersionFeedReadsTheTagName(t *testing.T) {
	// The feed is built over the shared fetcher; a nil client is not
	// exercised here, only the wiring shape.
	feed := NewVersionFeed(nil, "https://example.test/releases/latest")
	require.NotNil(t, feed)
	assert.Equal(t, VersionCheckInterval, feed.CheckInterval)
	assert.NotNil(t, feed.Fetch)
}

func TestNewVersionFeedParsesAReleaseResponse(t *testing.T) {
	// End to end over the shared fetcher: the GitHub payload shape and
	// the tag prefix strip must survive the real HTTP path.
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/repos/owner/repo/releases/latest", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"v7.2.1","name":"7.2.1"}`))
	}))
	t.Cleanup(receiver.Close)

	outbound := fetcher.New(fetcher.Options{Logger: logger.NewMock(), Timeout: 5 * time.Second})
	t.Cleanup(func() { _ = outbound.Close() })

	feed := NewVersionFeed(outbound, receiver.URL+"/repos/owner/repo/releases/latest")
	require.NoError(t, feed.Refresh(context.Background()))
	assert.Equal(t, "7.2.1", feed.Latest())
}

func TestNewVersionFeedSurfacesNon2xx(t *testing.T) {
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(receiver.Close)

	outbound := fetcher.New(fetcher.Options{Logger: logger.NewMock(), Timeout: 5 * time.Second})
	t.Cleanup(func() { _ = outbound.Close() })

	feed := NewVersionFeed(outbound, receiver.URL)
	err := feed.Refresh(context.Background())
	require.Error(t, err)
	assert.Equal(t, feedLatestFallback(), feed.Latest(), "a failed lookup keeps the previous value")
}
