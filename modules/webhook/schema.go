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
	// failed payloads stay inspectable; the webhook_logs row is the
	// primary record and is pruned separately.
	WebhookRetention = 7 * 24 * time.Hour
)

// WebhookDeliveryTask delivers one recorded event to one endpoint. The
// log row is the outbox record: it is written in the same transaction
// that enqueues the delivery, so a rolled back event never delivers.
type WebhookDeliveryTask struct {
	// LogID identifies the webhook_logs row updated per attempt.
	LogID string `json:"log_id"`

	// WebhookID identifies the endpoint row.
	WebhookID string `json:"webhook_id"`

	// Event is the event name the endpoint subscribed to.
	Event string `json:"event"`

	// Payload is the event body; the delivery signs its canonical JSON
	// encoding byte for byte.
	Payload map[string]any `json:"payload,omitzero"`
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

	// WebhookID identifies a webhook_events row (the endpoint).
	WebhookID = typeid.TypeID[webhookPrefix]

	webhookLogPrefix struct{}

	// DeliveryLogID identifies a webhook_logs row (one delivery).
	DeliveryLogID = typeid.TypeID[webhookLogPrefix]
)

func (webhookPrefix) Prefix() string    { return "webhook" }
func (webhookLogPrefix) Prefix() string { return "webhook_log" }

// Subscription wildcard: accepting it means "every event".
const AllEvents = "*"

// namePattern mirrors the database CHECK constraint on
// webhook_events.name.
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

// DeliveryLog is one webhook_logs row: the outbox record plus the
// outcome of the last attempt.
type DeliveryLog struct {
	ID         DeliveryLogID  `json:"id"`
	WebhookID  *WebhookID     `json:"webhook_id,omitzero"`
	Event      *string        `json:"event,omitzero"`
	HTTPStatus *int           `json:"http_status,omitzero"`
	Request    map[string]any `json:"request,omitzero"`
	Response   map[string]any `json:"response,omitzero"`
	Attempts   int            `json:"attempts"`
	Succeeded  bool           `json:"succeeded"`
	Error      *string        `json:"error,omitzero"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  *time.Time     `json:"updated_at,omitzero"`
}

// AttemptResult carries one delivery outcome back into the log row:
// what was sent (Request) and what came back (Response).
type AttemptResult struct {
	HTTPStatus int
	Request    map[string]any
	Response   map[string]any
	Err        error
	Succeeded  bool
}

// Store persists endpoints and delivery logs in
// public.webhook_events / public.webhook_logs.
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

	// InsertLog writes the outbox record (pending delivery).
	InsertLog(ctx context.Context, exec Executor, log *DeliveryLog) error
	// RecordAttempt updates one delivery with its latest outcome.
	RecordAttempt(ctx context.Context, id DeliveryLogID, result AttemptResult) error
	ListLogs(ctx context.Context, params ListParams, webhookID *WebhookID) ([]DeliveryLog, int, error)
	// PruneLogs deletes delivery logs older than the cutoff.
	PruneLogs(ctx context.Context, before time.Time) (int64, error)
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
