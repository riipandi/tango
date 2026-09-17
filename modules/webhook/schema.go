// Package webhook registers outbound endpoints, fans application
// events out to the subscribers of each event name, and records every
// delivery attempt. Deliveries are signed (HMAC-SHA256 over a
// timestamped canonical body) and retried by the queue, so the
// HTTP path never blocks on a slow receiver.
//
// The module owns the webhook surface and its delivery records.
package webhook

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/go-ozzo/ozzo-validation/v4"
	"go.jetify.com/typeid"

	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/internal/queue"
)

// Delivery queue tuning. Webhook attempts are bounded by the queue's own
// backoff, so the values here stay deliberately small: a failing
// receiver must not stall the worker pool for minutes.
const (
	// WebhookQueue is the registry key of the delivery queue; it must
	// stay stable across releases.
	WebhookQueue = "webhook"
	// WebhookMaxAttempts is the retry budget for one signed delivery.
	WebhookMaxAttempts = 5
	// WebhookTimeout bounds the outbound request; the spec fixes 30s,
	// the extra 10s absorbs connection setup and TLS.
	WebhookTimeout = 40 * time.Second
	// WebhookRequestTimeout is the receiver-facing deadline.
	WebhookRequestTimeout = 30 * time.Second
	// WebhookBackoff is the wait between delivery attempts.
	WebhookBackoff = 30 * time.Second
	// WebhookRetention keeps completed delivery tasks for a week so
	// failed payloads stay inspectable; the delivery row is the
	// primary record and is pruned separately.
	WebhookRetention = 7 * 24 * time.Hour
)

// WebhookDeliveryTask delivers one recorded delivery to one endpoint.
// The delivery row is the outbox record: it is written in the same
// transaction that enqueues the task, and it holds the immutable body
// bytes every attempt (including retries) signs and sends.
type WebhookDeliveryTask struct {
	// DeliveryID identifies the webhook_deliveries row to update.
	DeliveryID string `json:"delivery_id"`

	// WebhookID identifies the endpoint row.
	WebhookID string `json:"webhook_id"`
}

// Config defines the queue settings.
func (WebhookDeliveryTask) Config() queue.QueueConfig {
	return queue.QueueConfig{
		Name:        WebhookQueue,
		MaxAttempts: WebhookMaxAttempts,
		Timeout:     WebhookTimeout,
		Backoff:     WebhookBackoff,
		Retention: &queue.Retention{
			Duration:   WebhookRetention,
			OnlyFailed: true,
			Data:       &queue.RetainData{OnlyFailed: true},
		},
	}
}

// Typed IDs: UUIDv7 suffix plus a snake_case prefix matching the
// singular table name.
type (
	webhookPrefix struct{}

	// WebhookID identifies a webhook_endpoints row (the endpoint).
	WebhookID = typeid.TypeID[webhookPrefix]

	deliveryPrefix struct{}

	// DeliveryID identifies a webhook_deliveries row (one delivery).
	DeliveryID = typeid.TypeID[deliveryPrefix]
)

func (webhookPrefix) Prefix() string  { return "webhook" }
func (deliveryPrefix) Prefix() string { return "webhook_delivery" }

// Subscription wildcard: accepting it means "every event".
const AllEvents = "*"

// namePattern mirrors the database CHECK constraint on
// webhook_endpoints.name.
var namePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{3,100}$`)

// HTTP methods allowed for webhook endpoints.
var allowedMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE"}

const (
	// maxHeaderCount bounds the custom header map.
	maxHeaderCount = 20
	// maxHeadersBytes bounds the serialized custom header map.
	maxHeadersBytes = 8 << 10
	// maxEventTypes bounds the subscription list.
	maxEventTypes = 50
	// maxPayloadBytes bounds one event body; larger events are rejected
	// instead of silently truncated (the signature covers the whole body).
	maxPayloadBytes = 1 << 20
)

// Errors surfaced to handlers; statuses live on the sentinels.
var (
	// ErrNotFound covers unknown endpoints and delivery logs.
	ErrNotFound = errors.New("webhook: not found")
	// ErrDuplicateName is a unique-constraint violation on name.
	ErrDuplicateName = errors.New("webhook: name already exists")
	// ErrDisabled reports a delivery against a disabled endpoint.
	ErrDisabled = errors.New("webhook: endpoint is disabled")
	// ErrTooLarge rejects an oversized event payload.
	ErrTooLarge = errors.New("webhook: payload exceeds the size limit")
)

// Webhook is one registered endpoint.
type Webhook struct {
	ID          WebhookID         `json:"id"`
	Name        string            `json:"name"`
	Description *string           `json:"description,omitzero"`
	Endpoint    string            `json:"endpoint"`
	Method      string            `json:"method"`
	Headers     map[string]string `json:"headers,omitzero"`
	Enabled     bool              `json:"enabled"`
	EventTypes  []string          `json:"event_types"`

	// Secret is the plaintext signing secret. It is populated on
	// create/rotate only; listings and reads never carry it.
	Secret *string `json:"secret,omitzero"`

	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt *time.Time `json:"updated_at,omitzero"`
}

// SubscribedTo reports whether the endpoint receives the event.
func (w Webhook) SubscribedTo(event string) bool {
	if len(w.EventTypes) == 0 || slices.Contains(w.EventTypes, AllEvents) {
		return true
	}
	return slices.Contains(w.EventTypes, event)
}

// CreateParams carries the POST body fields.
type CreateParams struct {
	Name        string            `json:"name"`
	Description *string           `json:"description,omitzero"`
	Endpoint    string            `json:"endpoint"`
	Method      string            `json:"method,omitzero"`
	Headers     map[string]string `json:"headers,omitzero"`
	Enabled     *bool             `json:"enabled,omitzero"`
	EventTypes  []string          `json:"event_types,omitzero"`
}

// Validate checks the endpoint name and shape.
func (p *CreateParams) Validate() error {
	p.Name = strings.TrimSpace(p.Name)
	p.Endpoint = strings.TrimSpace(p.Endpoint)
	p.Method = strings.ToUpper(strings.TrimSpace(p.Method))
	if p.Method == "" {
		p.Method = "POST"
	}
	p.EventTypes = normalizeEventTypes(p.EventTypes)

	return validation.ValidateStruct(p,
		validation.Field(&p.Name,
			validation.Required,
			validation.Match(namePattern).Error("must be 3-100 letters, digits, dashes or underscores")),
		validation.Field(&p.Endpoint, validation.Required, validation.By(validEndpoint)),
		validation.Field(&p.Method, validation.Required, validation.In(toAny(allowedMethods)...)),
		validation.Field(&p.Headers, validation.By(validHeaders)),
		validation.Field(&p.EventTypes, validation.Length(0, maxEventTypes)),
	)
}

// UpdateParams carries the PUT body fields. Nil pointers keep the
// current value; a rotated secret is issued by the rotate route.
type UpdateParams struct {
	Name        *string           `json:"name,omitzero"`
	Description *string           `json:"description,omitzero"`
	Endpoint    *string           `json:"endpoint,omitzero"`
	Method      *string           `json:"method,omitzero"`
	Headers     map[string]string `json:"headers,omitzero"`
	Enabled     *bool             `json:"enabled,omitzero"`
	EventTypes  []string          `json:"event_types,omitzero"`
}

// Validate enforces the same rules as create on the fields present.
func (p *UpdateParams) Validate() error {
	if p.Name != nil {
		*p.Name = strings.TrimSpace(*p.Name)
	}
	if p.Endpoint != nil {
		*p.Endpoint = strings.TrimSpace(*p.Endpoint)
	}
	if p.Method != nil {
		trimmed := strings.ToUpper(strings.TrimSpace(*p.Method))
		p.Method = &trimmed
	}
	if p.EventTypes != nil {
		p.EventTypes = normalizeEventTypes(p.EventTypes)
	}

	return validation.ValidateStruct(p,
		validation.Field(&p.Name, validation.NilOrNotEmpty,
			validation.Match(namePattern).Error("must be 3-100 letters, digits, dashes or underscores")),
		validation.Field(&p.Endpoint, validation.NilOrNotEmpty, validation.By(validEndpointPtr)),
		validation.Field(&p.Method, validation.By(validMethodPtr)),
		validation.Field(&p.Headers, validation.By(validHeaders)),
		validation.Field(&p.EventTypes, validation.Length(0, maxEventTypes)),
	)
}

// Page is the store-level paging window: plain ints with no HTTP
// dependency. The handler converts the request query into it.
type Page struct {
	Page  int
	Limit int
}

// All reports whether the listing skips paging (page or limit is the
// all marker -1).
func (p Page) All() bool { return p.Page == -1 || p.Limit == -1 }

// Offset returns the SQL offset for the current page.
func (p Page) Offset() int {
	if p.All() || p.Page < 1 || p.Limit < 1 {
		return 0
	}
	return (p.Page - 1) * p.Limit
}

// ListParams narrows and pages the endpoint listing.
type ListParams struct {
	Enabled *bool
	Event   string
	Page
}

// Delivery is one webhook_deliveries row: the immutable canonical
// body plus the delivery state. The latest attempt's outcome (status,
// error, redacted response) rides along for the listing view.
type Delivery struct {
	ID          DeliveryID     `json:"id"`
	WebhookID   *WebhookID     `json:"webhook_id,omitzero"`
	Event       *string        `json:"event,omitzero"`
	HTTPStatus  *int           `json:"http_status,omitzero"`
	Response    map[string]any `json:"response,omitzero"`
	Attempts    int            `json:"attempts"`
	Succeeded   bool           `json:"succeeded"`
	Error       *string        `json:"error,omitzero"`
	CreatedAt   time.Time      `json:"created_at"`
	DeliveredAt *time.Time     `json:"delivered_at,omitzero"`
}

// PendingDelivery is the send-ready view of one delivery row: the
// committed event name and the exact canonical body bytes.
type PendingDelivery struct {
	ID    DeliveryID
	Event string
	Body  []byte
}

// AttemptRecord is one webhook_delivery_attempts row as exposed to
// the listing: the outcome of a single delivery try.
type AttemptRecord struct {
	Number     int            `json:"attempt_number"`
	HTTPStatus *int           `json:"response_status,omitzero"`
	Error      *string        `json:"error,omitzero"`
	DurationMs *int           `json:"duration_ms,omitzero"`
	Response   map[string]any `json:"response,omitzero"`
	Succeeded  bool           `json:"succeeded"`
	CreatedAt  time.Time      `json:"created_at"`
}

// AttemptResult carries one delivery outcome back into the delivery
// row and its attempt record: what was sent (Request) and what came
// back (Response).
type AttemptResult struct {
	HTTPStatus int
	Request    map[string]any
	Response   map[string]any
	Duration   time.Duration
	Err        error
	Succeeded  bool
}

// Store persists endpoints, deliveries, and attempts in
// public.webhook_endpoints / public.webhook_deliveries /
// public.webhook_delivery_attempts.
type Store interface {
	List(ctx context.Context, params ListParams) ([]Webhook, int, error)
	Get(ctx context.Context, id WebhookID) (Webhook, error)
	Create(ctx context.Context, params CreateParams, encryptedSecret string) (Webhook, error)
	Update(ctx context.Context, id WebhookID, params UpdateParams) (Webhook, error)
	Delete(ctx context.Context, id WebhookID) error
	SetSecret(ctx context.Context, id WebhookID, encryptedSecret string) error

	// Subscribers returns the enabled endpoints for an event, with
	// their decrypted-at-the-service-layer secret still encrypted.
	Subscribers(ctx context.Context, event string) ([]Webhook, error)
	// Secret returns the stored ciphertext for one endpoint.
	Secret(ctx context.Context, id WebhookID) (string, error)

	// InsertDelivery writes the outbox record (pending delivery).
	InsertDelivery(ctx context.Context, exec Executor, delivery *Delivery, body []byte) error
	// DeliveryForSend loads the immutable body bytes and event name
	// for one attempt.
	DeliveryForSend(ctx context.Context, id DeliveryID) (PendingDelivery, error)
	// RecordAttempt appends one attempt row and folds its outcome
	// into the delivery.
	RecordAttempt(ctx context.Context, id DeliveryID, result AttemptResult) error
	// ListAttempts returns the attempt history of one delivery in
	// attempt order.
	ListAttempts(ctx context.Context, id DeliveryID) ([]AttemptRecord, error)
	ListDeliveries(ctx context.Context, params ListParams, webhookID *WebhookID) ([]Delivery, int, error)
	// PruneDeliveries deletes deliveries recorded before the cutoff.
	PruneDeliveries(ctx context.Context, before time.Time) (int64, error)
}

// Executor is the query surface accepted for outbox writes: the pool
// (autocommit) or an open transaction, so the log row and the enqueued
// delivery records commit together.
type Executor = datastore.Executor

func validEndpoint(value any) error {
	raw, _ := value.(string)
	return validateEndpoint(raw)
}

func validateEndpoint(raw string) error {
	// Trim here as well as in the DTO: the rule is also reachable
	// directly, and a padded URL must not slip through as "relative".
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return errors.New("cannot be blank")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return errors.New("must be a valid URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("must use http or https")
	}
	if parsed.Host == "" {
		return errors.New("must include a host")
	}
	return nil
}

func validEndpointPtr(value any) error {
	ptr, ok := value.(*string)
	if !ok || ptr == nil {
		return nil
	}
	return validateEndpoint(*ptr)
}

func validMethodPtr(value any) error {
	ptr, ok := value.(*string)
	if !ok || ptr == nil {
		return nil
	}
	if !slices.Contains(allowedMethods, *ptr) {
		return errors.New("must be one of " + strings.Join(allowedMethods, ", "))
	}
	return nil
}

// validHeaders enforces bounded, non-sensitive custom headers: the
// signature and content-type headers are set by the sender, never by
// the registration.
func validHeaders(value any) error {
	headers, _ := value.(map[string]string)
	if len(headers) > maxHeaderCount {
		return errors.New("too many headers")
	}
	total := 0
	for name, headerValue := range headers {
		total += len(name) + len(headerValue)
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "", "content-type", "content-length", "host", "x-signature", "x-webhook-id", "x-webhook-event":
			return errors.New("header " + name + " is reserved")
		}
	}
	if total > maxHeadersBytes {
		return errors.New("headers are too large")
	}
	return nil
}

// normalizeEventTypes trims, de-duplicates, and sorts the
// subscription list so storage stays comparable.
func normalizeEventTypes(events []string) []string {
	if len(events) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(events))
	for _, event := range events {
		trimmed := strings.TrimSpace(event)
		if trimmed == "" || slices.Contains(out, trimmed) {
			continue
		}
		out = append(out, trimmed)
	}
	slices.Sort(out)
	return out
}

func toAny(values []string) []any {
	out := make([]any, len(values))
	for i, value := range values {
		out[i] = value
	}
	return out
}
