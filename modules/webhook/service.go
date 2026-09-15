package webhook

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"go.jetify.com/typeid"

	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/internal/logger"
	"github.com/riipandi/tango/internal/queue"
	"github.com/riipandi/tango/pkg/responder"
)

// secretBytes is the entropy of a signing secret (hex-encoded, so 64
// characters on the wire).
const secretBytes = 32

// cipher seals and opens the per-endpoint signing secret at rest.
type cipher interface {
	Encrypt(plaintext string) (string, error)
	Decrypt(encoded string) (string, error)
}

// Service holds the webhook business rules: endpoint CRUD, event fan
// out (the outbox write), and signed delivery.
type Service struct {
	store  Store
	queue  *queue.Client
	cipher cipher
	db     datastore.Store
	log    logger.Logger

	// now is injectable so signing tests pin the timestamp.
	now func() time.Time

	// send performs one HTTP delivery; the fetcher-backed
	// implementation lives in sender.go.
	send Sender
}

// Sender delivers one signed request and reports the outcome.
type Sender interface {
	Send(ctx context.Context, delivery Delivery) (status int, body []byte, err error)
}

// ServiceOption configures the service.
type ServiceOption func(*Service)

// WithClock overrides the signing clock (tests).
func WithClock(now func() time.Time) ServiceOption {
	return func(s *Service) { s.now = now }
}

// WithSender overrides the HTTP sender (tests).
func WithSender(sender Sender) ServiceOption {
	return func(s *Service) { s.send = sender }
}

// NewService builds the feature over its dependencies. db is used for
// the outbox transaction; queue carries the delivery tasks.
func NewService(store Store, db datastore.Store, queue *queue.Client, sealer cipher, log logger.Logger, opts ...ServiceOption) *Service {
	s := &Service{
		store:  store,
		db:     db,
		queue:  queue,
		cipher: sealer,
		log:    log,
		now:    func() time.Time { return time.Now().UTC() },
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// RegisterQueue registers the webhook delivery queue on the shared
// client. Called at build time by the composition root, before the
// dispatcher starts; a second call panics inside the queue (duplicate
// queue name), which is the intended fail-fast for double wiring.
func (s *Service) RegisterQueue(client *queue.Client) {
	client.Register(queue.NewQueue(func(ctx context.Context, task WebhookDeliveryTask) error {
		return s.Deliver(ctx, task)
	}))
}

// Name implements the module contract.
func (*Service) Name() string { return ModuleName }

// List pages endpoints (secrets never leave the store).
func (s *Service) List(ctx context.Context, params ListParams) ([]Webhook, int, error) {
	return s.store.List(ctx, params)
}

// Get loads one endpoint without its secret.
func (s *Service) Get(ctx context.Context, id WebhookID) (Webhook, error) {
	return s.store.Get(ctx, id)
}

// Create registers an endpoint and returns it with the freshly minted
// signing secret — the only time the plaintext is exposed.
func (s *Service) Create(ctx context.Context, params CreateParams) (Webhook, error) {
	if err := params.Validate(); err != nil {
		return Webhook{}, err
	}

	secret, err := newSecret()
	if err != nil {
		return Webhook{}, err
	}
	encrypted, err := s.cipher.Encrypt(secret)
	if err != nil {
		return Webhook{}, fmt.Errorf("webhook: encrypt secret: %w", err)
	}

	hook, err := s.store.Create(ctx, params, encrypted)
	if err != nil {
		return Webhook{}, err
	}
	hook.Secret = &secret
	return hook, nil
}

// Update patches an endpoint.
func (s *Service) Update(ctx context.Context, id WebhookID, params UpdateParams) (Webhook, error) {
	if err := params.Validate(); err != nil {
		return Webhook{}, err
	}
	return s.store.Update(ctx, id, params)
}

// Delete removes an endpoint.
func (s *Service) Delete(ctx context.Context, id WebhookID) error {
	return s.store.Delete(ctx, id)
}

// RotateSecret issues a new signing secret and returns it once.
func (s *Service) RotateSecret(ctx context.Context, id WebhookID) (string, error) {
	secret, err := newSecret()
	if err != nil {
		return "", err
	}
	encrypted, err := s.cipher.Encrypt(secret)
	if err != nil {
		return "", fmt.Errorf("webhook: encrypt secret: %w", err)
	}
	if err := s.store.SetSecret(ctx, id, encrypted); err != nil {
		return "", err
	}
	return secret, nil
}

// ListLogs pages delivery logs; a nil id lists across endpoints.
func (s *Service) ListLogs(ctx context.Context, id *WebhookID, params ListParams) ([]DeliveryLog, int, error) {
	return s.store.ListLogs(ctx, params, id)
}

// Emit fans an event out to its subscribers. The pending log row and
// the queued delivery task are written in one transaction, so an
// event that never commits never delivers. A nil queue (tests) or a
// module without subscribers degrades to a no-op.
func (s *Service) Emit(ctx context.Context, event string, payload map[string]any) error {
	if s.queue == nil {
		return nil
	}

	subscribers, err := s.store.Subscribers(ctx, event)
	if err != nil {
		return err
	}
	if len(subscribers) == 0 {
		return nil
	}
	return s.enqueue(ctx, subscribers, event, payload)
}

// DeliverTo queues one delivery to a named endpoint, bypassing the
// subscription filter. It backs the manual test route: a receiver
// must be reachable even when its subscription list does not name the
// synthetic event.
func (s *Service) DeliverTo(ctx context.Context, id WebhookID, event string, payload map[string]any) (DeliveryLogID, error) {
	hook, err := s.store.Get(ctx, id)
	if err != nil {
		return DeliveryLogID{}, err
	}

	logID, err := s.enqueueForLog(ctx, hook, event, payload)
	if err != nil {
		return DeliveryLogID{}, err
	}
	return logID, nil
}

// enqueue writes one pending log row per endpoint and queues the
// matching delivery task, all inside a single transaction.
func (s *Service) enqueue(ctx context.Context, subscribers []Webhook, event string, payload map[string]any) error {
	_, err := s.enqueueAll(ctx, subscribers, event, payload)
	return err
}

// enqueueForLog queues a delivery for one endpoint and returns the
// log row it will update.
func (s *Service) enqueueForLog(ctx context.Context, hook Webhook, event string, payload map[string]any) (DeliveryLogID, error) {
	if s.queue == nil {
		return DeliveryLogID{}, errors.New("webhook: queue is not configured")
	}
	ids, err := s.enqueueAll(ctx, []Webhook{hook}, event, payload)
	if err != nil {
		return DeliveryLogID{}, err
	}
	return ids[0], nil
}

// enqueueAll performs the outbox write and returns the created log IDs
// in subscriber order.
func (s *Service) enqueueAll(ctx context.Context, subscribers []Webhook, event string, payload map[string]any) ([]DeliveryLogID, error) {
	if s.queue == nil {
		return nil, nil
	}

	body, err := CanonicalPayload(payload)
	if err != nil {
		return nil, err
	}

	// One task per subscriber: each has its own log row and retry
	// budget, so a failing receiver cannot hold up the others.
	tasks := make([]queue.Task, 0, len(subscribers))
	logs := make([]*DeliveryLog, 0, len(subscribers))
	logIDs := make([]DeliveryLogID, 0, len(subscribers))

	err = s.db.WithTx(ctx, func(exec datastore.Executor) error {
		for i := range subscribers {
			hookID := subscribers[i].ID
			entry := &DeliveryLog{
				WebhookID: &hookID,
				Event:     &event,
				Request: map[string]any{
					"endpoint": subscribers[i].Endpoint,
					"method":   subscribers[i].Method,
					"body":     string(body),
				},
			}
			if insertErr := s.store.InsertLog(ctx, exec, entry); insertErr != nil {
				return insertErr
			}
			logs = append(logs, entry)
			logIDs = append(logIDs, entry.ID)
			tasks = append(tasks, WebhookDeliveryTask{
				LogID:     entry.ID.String(),
				WebhookID: hookID.String(),
				Event:     event,
				Payload:   payload,
			})
		}
		_, addErr := s.queue.Add(tasks...).Ctx(ctx).Executor(exec).Save()
		return addErr
	})
	if err != nil {
		return nil, err
	}

	// Notify once the transaction committed: the dispatcher poll
	// cannot see tasks added inside a caller transaction.
	s.queue.Notify()
	return logIDs, nil
}

// Deliver executes one queued delivery and records the attempt. The
// error is returned so the queue applies its retry schedule.
func (s *Service) Deliver(ctx context.Context, task WebhookDeliveryTask) error {
	logID, err := parseDeliveryLogID(task.LogID)
	if err != nil {
		return err
	}
	webhookID, err := parseWebhookID(task.WebhookID)
	if err != nil {
		return err
	}

	hook, err := s.store.Get(ctx, webhookID)
	if err != nil {
		return err
	}
	if !hook.Enabled {
		return s.finish(ctx, logID, AttemptResult{Err: ErrDisabled})
	}

	encrypted, err := s.store.Secret(ctx, webhookID)
	if err != nil {
		return s.finish(ctx, logID, AttemptResult{Err: err})
	}
	secret, err := s.cipher.Decrypt(encrypted)
	if err != nil {
		return s.finish(ctx, logID, AttemptResult{Err: fmt.Errorf("webhook: decrypt secret: %w", err)})
	}

	delivery, err := Sign(hook.Endpoint, hook.Method, hook.Headers, task.Event, task.Payload, secret, s.now())
	if err != nil {
		// Payload-size violations are permanent: stop retrying.
		return s.finish(ctx, logID, AttemptResult{Err: err})
	}

	status, body, sendErr := s.send.Send(ctx, delivery)

	result := AttemptResult{
		HTTPStatus: status,
		Request:    deliverySummary(delivery),
		Response:   responseSummary(body),
	}
	switch {
	case sendErr != nil:
		result.Err = sendErr
	case status >= 200 && status < 300:
		result.Succeeded = true
	default:
		result.Err = fmt.Errorf("webhook: endpoint answered %d", status)
	}

	if err := s.finish(ctx, logID, result); err != nil {
		return err
	}
	return resultErr(result)
}

// finish records one attempt outcome.
func (s *Service) finish(ctx context.Context, logID DeliveryLogID, result AttemptResult) error {
	// The attempt write must survive cancellation: the request context
	// is gone by the time a timeout error is recorded.
	if err := s.store.RecordAttempt(context.WithoutCancel(ctx), logID, result); err != nil {
		s.log.WithError(err).WithMetadata(map[string]any{"log_id": logID.String()}).
			Error("failed to record webhook delivery attempt")
		return errors.Join(resultErr(result), err)
	}
	return resultErr(result)
}

func resultErr(result AttemptResult) error {
	if result.Succeeded {
		return nil
	}
	if result.Err == nil {
		return errors.New("webhook: delivery failed")
	}
	return result.Err
}

// responseSummary keeps a bounded, loggable view of the response body:
// the full body belongs in the delivery record, not in a JSONB column
// that a hostile receiver could blow up.
func responseSummary(body []byte) map[string]any {
	if len(body) == 0 {
		return nil
	}
	const maxBody = 4 << 10
	truncated := len(body) > maxBody
	kept := body
	if truncated {
		kept = body[:maxBody]
	}
	out := map[string]any{"body": string(kept), "bytes": len(body)}
	if truncated {
		out["truncated"] = true
	}
	return out
}

// deliverySummary captures the rendered request for the delivery
// record: method, target, headers (signature included) and body. A
// receiver can re-verify the signature from this record alone.
func deliverySummary(delivery Delivery) map[string]any {
	return map[string]any{
		"method":   delivery.Method,
		"endpoint": delivery.URL,
		"headers":  delivery.Headers,
		"body":     string(delivery.Body),
	}
}

// newSecret mints a 256-bit signing secret, URL-safe encoded.
func newSecret() (string, error) {
	buf := make([]byte, secretBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("webhook: generate secret: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// parseWebhookID decodes the wire-form typed ID.
func parseWebhookID(raw string) (WebhookID, error) {
	id, err := typeid.Parse[WebhookID](raw)
	if err != nil {
		return WebhookID{}, fmt.Errorf("webhook: invalid endpoint id: %w", err)
	}
	return id, nil
}

func parseDeliveryLogID(raw string) (DeliveryLogID, error) {
	id, err := typeid.Parse[DeliveryLogID](raw)
	if err != nil {
		return DeliveryLogID{}, fmt.Errorf("webhook: invalid log id: %w", err)
	}
	return id, nil
}

// PageParams narrows endpoint listings by the request query.
func PageParams(params responder.PaginationParams) ListParams {
	return ListParams{PaginationParams: params}
}
