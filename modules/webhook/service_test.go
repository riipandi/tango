package webhook

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/internal/fetcher"
	"github.com/riipandi/tango/internal/jobs"
	"github.com/riipandi/tango/internal/logger"
	"github.com/riipandi/tango/internal/queue"
	"github.com/riipandi/tango/pkg/crypto"
	"github.com/riipandi/tango/pkg/testutils"
)

// testStack is the real delivery stack over the shared test container:
// Postgres store, queue client, AES cipher, and a controllable sender.
type testStack struct {
	Service *Service
	Store   *PostgresStore
	DB      datastore.Store
	Queue   *queue.Client
	Sender  *captureSender
}

func newTestStack(t *testing.T, sender *captureSender) *testStack {
	t.Helper()
	ctx := t.Context()

	pg := testutils.StartPostgres(ctx, t)
	if _, err := database.MigrateUp(ctx, pg.DSN); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	db, err := datastore.New(ctx, datastore.Options{DSN: pg.DSN})
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	client, err := queue.NewClient(queue.ClientConfig{
		Store:        db,
		NumWorkers:   1,
		ReleaseAfter: time.Hour,
	})
	require.NoError(t, err)

	// The container is shared: never let another test's rows leak in.
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = db.Exec(bg, "DELETE FROM queue_tasks")
		_, _ = db.Exec(bg, "DELETE FROM queue_tasks_completed")
		_, _ = db.Exec(bg, "DELETE FROM webhook_delivery_attempts")
		_, _ = db.Exec(bg, "DELETE FROM webhook_deliveries")
		_, _ = db.Exec(bg, "DELETE FROM webhook_endpoints")
	})

	sealer, err := crypto.NewCipher(make([]byte, 32))
	require.NoError(t, err)

	store := NewPostgresStore(db)
	service := NewService(store, db, client, sealer, logger.NewMock(),
		WithClock(func() time.Time { return time.Now().UTC() }),
		WithSender(sender),
	)
	service.RegisterQueue(client)

	stack := &testStack{Service: service, Store: store, DB: db, Queue: client, Sender: sender}
	t.Cleanup(func() {
		// Draining the queue keeps a late delivery out of the next test.

	})
	return stack
}

// captureSender records every delivery and replays a scripted result
// per call; the last scripted result repeats.
type captureSender struct {
	mu      sync.Mutex
	script  []sendResult
	calls   int
	gotBody []byte
	gotReq  OutboundDelivery
}

type sendResult struct {
	status int
	body   string
	err    error
}

func (s *captureSender) Send(_ context.Context, delivery OutboundDelivery) (int, []byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.calls++
	s.gotReq = delivery
	s.gotBody = delivery.Body

	index := min(s.calls-1, len(s.script)-1)
	result := s.script[index]
	return result.status, []byte(result.body), result.err
}

func (s *captureSender) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func (s *captureSender) last() OutboundDelivery {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.gotReq
}

// waitFor polls until the predicate holds or the timeout passes.
func waitFor(t *testing.T, timeout time.Duration, predicate func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if predicate() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return predicate()
}

func okSender() *captureSender {
	return &captureSender{script: []sendResult{{status: http.StatusOK, body: `{"ok":true}`}}}
}

func (s *testStack) create(t *testing.T, name, endpoint string, events ...string) Webhook {
	t.Helper()
	hook, err := s.Service.Create(t.Context(), CreateParams{
		Name:       name,
		Endpoint:   endpoint,
		EventTypes: events,
	})
	require.NoError(t, err)
	return hook
}

// startQueue runs the dispatcher; the stack cleans it up.
func (s *testStack) startQueue(t *testing.T) {
	t.Helper()
	s.Queue.Start(t.Context())
}

func (s *testStack) onlyDelivery(t *testing.T, id WebhookID) Delivery {
	t.Helper()
	logs, _, err := s.Store.ListDeliveries(t.Context(), ListParams{}, &id)
	require.NoError(t, err)
	require.Len(t, logs, 1)
	return logs[0]
}

func TestCreateStoresSecretEncryptedAndNeverReturnsItOnRead(t *testing.T) {
	stack := newTestStack(t, okSender())
	ctx := t.Context()

	hook := stack.create(t, "ci-endpoint", "https://example.test/hook", "user.created")
	require.NotNil(t, hook.Secret, "create returns the plaintext secret once")
	assert.Equal(t, "webhook", hook.ID.Prefix())

	fetched, err := stack.Service.Get(ctx, hook.ID)
	require.NoError(t, err)
	assert.Nil(t, fetched.Secret, "reads must not return the secret")

	var stored *string
	require.NoError(t, stack.DB.QueryRow(ctx,
		"SELECT secret_enc FROM webhook_endpoints WHERE id = $1", hook.ID.UUID()).Scan(&stored))
	require.NotNil(t, stored)
	assert.NotEqual(t, *hook.Secret, *stored, "the secret must be ciphertext at rest")
}

func TestEmitWritesOutboxRowThenDeliversSignedBody(t *testing.T) {
	stack := newTestStack(t, okSender())
	ctx := t.Context()

	hook := stack.create(t, "outbox-endpoint", "https://example.test/hook", "user.created")

	stack.startQueue(t)

	require.NoError(t, stack.Service.Emit(ctx, "user.created", map[string]any{
		"event":   "user.created",
		"user_id": "user_01m2",
	}))

	// The outbox row is written before any delivery attempt.
	pending := stack.onlyDelivery(t, hook.ID)
	require.NotNil(t, pending.Event)
	assert.Equal(t, "user.created", *pending.Event)

	require.True(t, waitFor(t, 15*time.Second, func() bool { return stack.Sender.count() >= 1 }),
		"the delivery must reach the sender")

	delivery := stack.Sender.last()
	assert.Equal(t, hook.Endpoint, delivery.URL)
	assert.Equal(t, "POST", delivery.Method)
	require.NoError(t, VerifySignature(delivery.Headers[SignatureHeader], delivery.Body, *hook.Secret, time.Now().UTC()))

	require.True(t, waitFor(t, 15*time.Second, func() bool { return stack.onlyDelivery(t, hook.ID).Succeeded }),
		"the attempt outcome must be recorded")

	recorded := stack.onlyDelivery(t, hook.ID)
	assert.Equal(t, 1, recorded.Attempts)
	require.NotNil(t, recorded.HTTPStatus)
	assert.Equal(t, http.StatusOK, *recorded.HTTPStatus)
	assert.Nil(t, recorded.Error)
}

func TestEmitSkipsUnsubscribedEvent(t *testing.T) {
	stack := newTestStack(t, okSender())
	ctx := t.Context()

	stack.create(t, "user-only", "https://example.test/hook", "user.created")

	require.NoError(t, stack.Service.Emit(ctx, "api_key.created", map[string]any{"event": "api_key.created"}))

	_, total, err := stack.Store.ListDeliveries(ctx, ListParams{}, nil)
	require.NoError(t, err)
	assert.Zero(t, total, "a non-subscriber gets no outbox row")
	assert.Zero(t, stack.Sender.count())
}

func TestEmitReachesWildcardAndEmptySubscribers(t *testing.T) {
	stack := newTestStack(t, okSender())
	ctx := t.Context()

	stack.create(t, "wildcard", "https://one.test/hook", AllEvents)
	stack.create(t, "implicit", "https://two.test/hook")

	require.NoError(t, stack.Service.Emit(ctx, "anything.happened", map[string]any{"event": "anything.happened"}))

	_, total, err := stack.Store.ListDeliveries(ctx, ListParams{}, nil)
	require.NoError(t, err)
	assert.Equal(t, 2, total)
}

func TestSubscribersFiltering(t *testing.T) {
	stack := newTestStack(t, okSender())
	ctx := t.Context()

	stack.create(t, "user-events", "https://one.test/hook", "user.created", "user.deleted")
	stack.create(t, "wildcard-events", "https://two.test/hook")
	stack.create(t, "other-events", "https://three.test/hook", "api_key.created")

	subscribers, err := stack.Store.Subscribers(ctx, "user.created")
	require.NoError(t, err)

	names := make([]string, 0, len(subscribers))
	for _, hook := range subscribers {
		names = append(names, hook.Name)
	}
	assert.ElementsMatch(t, []string{"user-events", "wildcard-events"}, names)
}

// TestRetryScheduleRecordsEveryAttempt drives the deliveries directly
// instead of waiting out the queue's 30s backoff; the attempt budget
// and the recorded outcome are the same either way.
func TestRetryScheduleRecordsEveryAttempt(t *testing.T) {
	sender := &captureSender{script: []sendResult{{status: http.StatusInternalServerError, body: `{"error":"boom"}`}}}
	stack := newTestStack(t, sender)
	ctx := t.Context()

	hook := stack.create(t, "failing-endpoint", "https://example.test/hook", AllEvents)
	deliveryID, err := stack.Service.DeliverTo(ctx, hook.ID, "user.created", map[string]any{"event": "user.created"})
	require.NoError(t, err)

	for attempt := range WebhookMaxAttempts {
		deliverErr := stack.Service.Deliver(ctx, WebhookDeliveryTask{
			DeliveryID: deliveryID.String(),
			WebhookID:  hook.ID.String(),
		})
		assert.Error(t, deliverErr, "attempt %d must surface the failure", attempt+1)
	}

	assert.Equal(t, WebhookMaxAttempts, sender.count(), "each attempt reaches the receiver")

	recorded := stack.onlyDelivery(t, hook.ID)
	assert.Equal(t, WebhookMaxAttempts, recorded.Attempts)
	assert.False(t, recorded.Succeeded)
	require.NotNil(t, recorded.Error)
	assert.Contains(t, *recorded.Error, "500")
	assert.Contains(t, recorded.Response["body"], "boom")
}

func TestDeliveryToDisabledEndpointNeverCallsOut(t *testing.T) {
	sender := okSender()
	stack := newTestStack(t, sender)
	ctx := t.Context()

	hook := stack.create(t, "disabled-endpoint", "https://example.test/hook", AllEvents)
	disabled := false
	_, err := stack.Service.Update(ctx, hook.ID, UpdateParams{Enabled: &disabled})
	require.NoError(t, err)

	deliveryID, err := stack.Service.DeliverTo(ctx, hook.ID, "user.created", map[string]any{"event": "user.created"})
	require.NoError(t, err)

	err = stack.Service.Deliver(ctx, WebhookDeliveryTask{
		DeliveryID: deliveryID.String(),
		WebhookID:  hook.ID.String(),
	})
	assert.ErrorIs(t, err, ErrDisabled)
	assert.Zero(t, sender.count(), "a disabled endpoint must not be called")
}

func TestRotateSecretInvalidatesTheOldSignature(t *testing.T) {
	sender := okSender()
	stack := newTestStack(t, sender)
	ctx := t.Context()

	hook := stack.create(t, "rotating", "https://example.test/hook", AllEvents)
	rotated, err := stack.Service.RotateSecret(ctx, hook.ID)
	require.NoError(t, err)
	assert.NotEqual(t, *hook.Secret, rotated)

	deliveryID, err := stack.Service.DeliverTo(ctx, hook.ID, "user.created", map[string]any{"event": "user.created"})
	require.NoError(t, err)
	require.NoError(t, stack.Service.Deliver(ctx, WebhookDeliveryTask{
		DeliveryID: deliveryID.String(),
		WebhookID:  hook.ID.String(),
	}))

	delivery := sender.last()
	require.NoError(t, VerifySignature(delivery.Headers[SignatureHeader], delivery.Body, rotated, time.Now().UTC()))
	assert.Error(t, VerifySignature(delivery.Headers[SignatureHeader], delivery.Body, *hook.Secret, time.Now().UTC()),
		"the rotated-out secret must no longer verify")
}

func TestDeliverToQueuesWithAPendingDeliveryRow(t *testing.T) {
	stack := newTestStack(t, okSender())
	ctx := t.Context()

	hook := stack.create(t, "testable", "https://example.test/hook")
	deliveryID, err := stack.Service.DeliverTo(ctx, hook.ID, defaultTestEvent, map[string]any{"event": defaultTestEvent})
	require.NoError(t, err)
	assert.Equal(t, "webhook_delivery", deliveryID.Prefix())

	recorded := stack.onlyDelivery(t, hook.ID)
	assert.Equal(t, deliveryID.String(), recorded.ID.String())
	assert.Zero(t, recorded.Attempts, "the row is pending until a worker runs")
}

func TestDuplicateNameConflicts(t *testing.T) {
	stack := newTestStack(t, okSender())
	ctx := t.Context()

	params := CreateParams{Name: "unique-name", Endpoint: "https://example.test/hook"}
	_, err := stack.Service.Create(ctx, params)
	require.NoError(t, err)

	_, err = stack.Service.Create(ctx, params)
	assert.ErrorIs(t, err, ErrDuplicateName)
}

func TestPruneLogsRemovesEntriesPastRetention(t *testing.T) {
	stack := newTestStack(t, okSender())
	ctx := t.Context()

	hook := stack.create(t, "pruned", "https://example.test/hook")
	_, err := stack.Service.DeliverTo(ctx, hook.ID, "user.created", map[string]any{"event": "user.created"})
	require.NoError(t, err)

	tag, err := stack.DB.Exec(ctx,
		"UPDATE webhook_deliveries SET created_at = CURRENT_TIMESTAMP - INTERVAL '60 days' WHERE webhook_id = $1",
		hook.ID.UUID())
	require.NoError(t, err)
	require.Equal(t, int64(1), tag.RowsAffected())

	removed, err := stack.Store.PruneDeliveries(ctx, time.Now().UTC().Add(-jobs.WebhookLogRetention))
	require.NoError(t, err)
	assert.Equal(t, int64(1), removed)
}

func TestListFiltersByEnabledAndEvent(t *testing.T) {
	stack := newTestStack(t, okSender())
	ctx := t.Context()

	stack.create(t, "enabled-user", "https://one.test/hook", "user.created")
	stack.create(t, "enabled-api", "https://two.test/hook", "api_key.created")

	enabled := true
	userOnly, total, err := stack.Store.List(ctx, ListParams{Enabled: &enabled, Event: "user.created"})
	require.NoError(t, err)
	assert.Equal(t, 1, total)
	require.Len(t, userOnly, 1)
	assert.Equal(t, "enabled-user", userOnly[0].Name)
}

// ptr returns a pointer to its argument (test helper).
func ptr[T any](value T) *T { return &value }

// TestHTTPDeliveryAgainstLiveReceiver exercises the fetcher-backed
// sender against a real HTTP server: method, headers, signature, and
// the response status must all survive the round trip.
func TestHTTPDeliveryAgainstLiveReceiver(t *testing.T) {
	var (
		mu      sync.Mutex
		gotReq  *http.Request
		gotBody []byte
	)
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		gotReq, gotBody = r, body
		mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"received":true}`))
	}))
	t.Cleanup(receiver.Close)

	ctx := t.Context()
	pg := testutils.StartPostgres(ctx, t)
	if _, err := database.MigrateUp(ctx, pg.DSN); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	db, err := datastore.New(ctx, datastore.Options{DSN: pg.DSN})
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = db.Exec(bg, "DELETE FROM webhook_delivery_attempts")
		_, _ = db.Exec(bg, "DELETE FROM webhook_deliveries")
		_, _ = db.Exec(bg, "DELETE FROM webhook_endpoints")
	})

	outbound := fetcher.New(fetcher.Options{Logger: logger.NewMock(), Timeout: 5 * time.Second})
	t.Cleanup(func() { _ = outbound.Close() })

	sealer, err := crypto.NewCipher(make([]byte, 32))
	require.NoError(t, err)

	store := NewPostgresStore(db)
	service := NewService(store, db, nil, sealer, logger.NewMock(),
		WithSender(NewFetcherSender(outbound)),
	)

	hook, err := service.Create(ctx, CreateParams{Name: "real-receiver", Endpoint: receiver.URL})
	require.NoError(t, err)

	body, err := CanonicalPayload(map[string]any{"event": "user.created"})
	require.NoError(t, err)

	delivery := &Delivery{WebhookID: &hook.ID, Event: ptr("user.created")}
	require.NoError(t, store.InsertDelivery(ctx, db, delivery, body))
	require.NotZero(t, delivery.ID)
	require.NoError(t, service.Deliver(ctx, WebhookDeliveryTask{
		DeliveryID: delivery.ID.String(),
		WebhookID:  hook.ID.String(),
	}))

	mu.Lock()
	req, body := gotReq, gotBody
	mu.Unlock()
	require.NotNil(t, req, "the receiver must see the delivery")
	assert.Equal(t, http.MethodPost, req.Method)
	assert.Equal(t, "application/json", req.Header.Get("Content-Type"))
	assert.NotEmpty(t, req.Header.Get(SignatureHeader))
	require.NoError(t, VerifySignature(req.Header.Get(SignatureHeader), body, *hook.Secret, time.Now().UTC()))

	recorded := func() Delivery {
		logs, _, listErr := store.ListDeliveries(ctx, ListParams{}, &hook.ID)
		require.NoError(t, listErr)
		require.Len(t, logs, 1)
		return logs[0]
	}()
	assert.True(t, recorded.Succeeded)
	require.NotNil(t, recorded.HTTPStatus)
	assert.Equal(t, http.StatusAccepted, *recorded.HTTPStatus)
	assert.Contains(t, recorded.Response["body"], "received")

	// The delivery record must let a receiver re-verify offline: the
	// committed body bytes and the signature the receiver saw. The
	// signature travels with the sender capture; the stored bytes are
	// re-verified against a fresh signature computed from the same
	// secret, pinning byte-for-byte stability.
	stored, err := store.DeliveryForSend(ctx, recorded.ID)
	require.NoError(t, err)
	assert.Equal(t, string(body), string(stored.Body), "the stored bytes are the delivered bytes")
	fresh, err := Sign(stored.Event, hook.Endpoint, hook.Method, nil, stored.Body, *hook.Secret, time.Now().UTC())
	require.NoError(t, err)
	require.NoError(t, VerifySignature(fresh.Headers[SignatureHeader], stored.Body, *hook.Secret, time.Now().UTC()))
}

func TestServiceNameAndDoubleRegistrationPanics(t *testing.T) {
	stack := newTestStack(t, okSender())
	assert.Equal(t, ModuleName, stack.Service.Name())

	// Double wiring is a build-time bug; the queue rejects the duplicate.
	assert.Panics(t, func() { stack.Service.RegisterQueue(stack.Queue) })
}

func TestRejectOversizedPayloadAtEmit(t *testing.T) {
	stack := newTestStack(t, okSender())
	ctx := t.Context()

	stack.create(t, "size-guard", "https://example.test/hook", AllEvents)

	err := stack.Service.Emit(ctx, "user.created", map[string]any{"blob": strings.Repeat("x", maxPayloadBytes+1)})
	assert.ErrorIs(t, err, ErrTooLarge)

	_, total, err := stack.Store.ListDeliveries(ctx, ListParams{}, nil)
	require.NoError(t, err)
	assert.Zero(t, total, "an oversized event must not leave an outbox row")
}
