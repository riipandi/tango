package webhook

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/huandu/go-sqlbuilder"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"go.jetify.com/typeid"

	"github.com/riipandi/tango/internal/datastore"
)

// Tables owned by this module.
const (
	endpointsTable  = "public.webhook_endpoints"
	deliveriesTable = "public.webhook_deliveries"
	attemptsTable   = "public.webhook_delivery_attempts"
)

// PostgresStore persists endpoints, deliveries, and attempts.
type PostgresStore struct {
	exec datastore.Executor
}

var _ Store = (*PostgresStore)(nil)

// NewPostgresStore builds the production store over the shared pool.
func NewPostgresStore(exec datastore.Executor) *PostgresStore {
	return &PostgresStore{exec: exec}
}

// webhookColumns is the SELECT list for endpoints; keep in sync with
// scanWebhook.
var webhookColumns = []string{
	"id", "name", "description", "endpoint", "method", "headers",
	"enabled", "event_types", "created_at", "updated_at",
}

// deliveryColumns is the delivery SELECT list; the attempt columns
// ride along from the latest attempt row. Keep in sync with
// scanDelivery.
var deliveryColumns = []string{
	"d.id", "d.webhook_id", "d.event", "d.attempt_count", "d.status",
	"d.created_at", "d.delivered_at",
	"a.response_status", "a.error", "a.response",
}

// List pages the endpoints, newest first.
func (s *PostgresStore) List(ctx context.Context, params ListParams) ([]Webhook, int, error) {
	count := sqlbuilder.PostgreSQL.NewSelectBuilder()
	count.Select("count(*)")
	count.From(endpointsTable)
	applyEndpointFilters(count, params)

	countQuery, countArgs := count.Build()
	var total int
	if err := s.exec.QueryRow(ctx, countQuery, countArgs...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("webhook store: count: %w", err)
	}

	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select(webhookColumns...)
	sb.From(endpointsTable)
	applyEndpointFilters(sb, params)
	sb.OrderBy("created_at DESC", "id DESC")
	if !params.All() && params.Limit > 0 {
		sb.Limit(params.Limit).Offset(params.Offset())
	}

	query, args := sb.Build()
	rows, err := s.exec.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("webhook store: list: %w", err)
	}
	defer rows.Close()

	out := []Webhook{}
	for rows.Next() {
		hook, scanErr := scanWebhook(rows)
		if scanErr != nil {
			return nil, 0, scanErr
		}
		out = append(out, hook)
	}
	return out, total, rows.Err()
}

// Get loads one endpoint by ID.
func (s *PostgresStore) Get(ctx context.Context, id WebhookID) (Webhook, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select(webhookColumns...)
	sb.From(endpointsTable)
	sb.Where(sb.E("id", id.UUID()))

	query, args := sb.Build()
	return scanWebhook(s.exec.QueryRow(ctx, query, args...))
}

// Create inserts an endpoint with its already-encrypted secret.
func (s *PostgresStore) Create(ctx context.Context, params CreateParams, encryptedSecret string) (Webhook, error) {
	method := params.Method
	if method == "" {
		method = "POST"
	}
	enabled := true
	if params.Enabled != nil {
		enabled = *params.Enabled
	}
	events := normalizeEventTypes(params.EventTypes)

	ib := sqlbuilder.PostgreSQL.NewInsertBuilder()
	ib.InsertInto(endpointsTable)
	ib.Cols("name", "description", "endpoint", "method", "headers", "enabled", "event_types", "secret_enc")
	ib.Values(params.Name, params.Description, params.Endpoint, method, nullableHeaders(params.Headers), enabled, events, encryptedSecret)
	ib.Returning(webhookColumns...)

	query, args := ib.Build()
	hook, err := scanWebhook(s.exec.QueryRow(ctx, query, args...))
	if err != nil {
		return Webhook{}, mapErr(err)
	}
	return hook, nil
}

// Update patches the endpoint; nil params fields keep current values.
func (s *PostgresStore) Update(ctx context.Context, id WebhookID, params UpdateParams) (Webhook, error) {
	ub := sqlbuilder.PostgreSQL.NewUpdateBuilder()
	ub.Update(endpointsTable)

	assignments := []string{}
	if params.Name != nil {
		assignments = append(assignments, ub.Assign("name", *params.Name))
	}
	if params.Description != nil {
		assignments = append(assignments, ub.Assign("description", nullableText(*params.Description)))
	}
	if params.Endpoint != nil {
		assignments = append(assignments, ub.Assign("endpoint", *params.Endpoint))
	}
	if params.Method != nil {
		assignments = append(assignments, ub.Assign("method", *params.Method))
	}
	if params.Headers != nil {
		assignments = append(assignments, ub.Assign("headers", nullableHeaders(params.Headers)))
	}
	if params.Enabled != nil {
		assignments = append(assignments, ub.Assign("enabled", *params.Enabled))
	}
	if params.EventTypes != nil {
		assignments = append(assignments, ub.Assign("event_types", normalizeEventTypes(params.EventTypes)))
	}
	if len(assignments) == 0 {
		return s.Get(ctx, id)
	}

	ub.Set(assignments...)
	ub.Where(ub.E("id", id.UUID()))
	ub.Returning(webhookColumns...)

	query, args := ub.Build()
	hook, err := scanWebhook(s.exec.QueryRow(ctx, query, args...))
	if err != nil {
		return Webhook{}, mapErr(err)
	}
	return hook, nil
}

// Delete removes an endpoint; its delivery logs stay (FK sets the
// webhook_id to NULL) so the audit trail survives.
func (s *PostgresStore) Delete(ctx context.Context, id WebhookID) error {
	db := sqlbuilder.PostgreSQL.NewDeleteBuilder()
	db.DeleteFrom(endpointsTable)
	db.Where(db.E("id", id.UUID()))

	query, args := db.Build()
	tag, err := s.exec.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("webhook store: delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetSecret rotates the stored ciphertext.
func (s *PostgresStore) SetSecret(ctx context.Context, id WebhookID, encryptedSecret string) error {
	ub := sqlbuilder.PostgreSQL.NewUpdateBuilder()
	ub.Update(endpointsTable)
	ub.Set(ub.Assign("secret_enc", encryptedSecret))
	ub.Where(ub.E("id", id.UUID()))

	query, args := ub.Build()
	tag, err := s.exec.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("webhook store: set secret: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Subscribers returns the enabled endpoints for an event: an empty
// subscription list or the wildcard receives everything. The match is
// an array overlap (&&), the operator the GIN index on event_types
// serves.
func (s *PostgresStore) Subscribers(ctx context.Context, event string) ([]Webhook, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select(webhookColumns...)
	sb.From(endpointsTable)
	sb.Where(
		sb.E("enabled", true),
		sb.Or(
			"CARDINALITY(event_types) = 0",
			eventMatch(sb, event),
		),
	)
	sb.OrderBy("created_at ASC")

	query, args := sb.Build()
	rows, err := s.exec.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("webhook store: subscribers: %w", err)
	}
	defer rows.Close()

	out := []Webhook{}
	for rows.Next() {
		hook, scanErr := scanWebhook(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, hook)
	}
	return out, rows.Err()
}

// Secret returns the stored ciphertext for one endpoint.
func (s *PostgresStore) Secret(ctx context.Context, id WebhookID) (string, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select("secret_enc")
	sb.From(endpointsTable)
	sb.Where(sb.E("id", id.UUID()))

	query, args := sb.Build()
	var secret *string
	if err := s.exec.QueryRow(ctx, query, args...).Scan(&secret); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("webhook store: secret: %w", err)
	}
	if secret == nil || *secret == "" {
		return "", ErrNotFound
	}
	return *secret, nil
}

// InsertDelivery writes the outbox record through exec (pool or
// caller tx). The canonical body bytes are stored verbatim: every
// attempt signs and sends exactly these bytes.
func (s *PostgresStore) InsertDelivery(ctx context.Context, exec Executor, delivery *Delivery, body []byte) error {
	ib := sqlbuilder.PostgreSQL.NewInsertBuilder()
	ib.InsertInto(deliveriesTable)
	ib.Cols("webhook_id", "event", "body")
	ib.Values(uuidOrNull(delivery.WebhookID), delivery.Event, body)
	ib.Returning("id", "created_at")

	query, args := ib.Build()
	var (
		id      string
		created pgtype.Timestamptz
	)
	if err := exec.QueryRow(ctx, query, args...).Scan(&id, &created); err != nil {
		return fmt.Errorf("webhook store: insert delivery: %w", err)
	}

	delivery.ID = mustDeliveryID(id)
	delivery.CreatedAt = created.Time
	return nil
}

// RecordAttempt appends one attempt row (response status, error,
// duration, redacted response) and folds the outcome into the
// delivery row.
func (s *PostgresStore) RecordAttempt(ctx context.Context, id DeliveryID, result AttemptResult) error {
	var current int
	if err := s.exec.QueryRow(ctx,
		"SELECT attempt_count FROM "+deliveriesTable+" WHERE id = $1", id.UUID(),
	).Scan(&current); err != nil {
		return fmt.Errorf("webhook store: read attempt count: %w", err)
	}

	ib := sqlbuilder.PostgreSQL.NewInsertBuilder()
	ib.InsertInto(attemptsTable)
	ib.Cols("delivery_id", "attempt_number", "response_status", "error", "duration_ms", "response")
	ib.Values(id.UUID(), current+1, nullableInt(result.HTTPStatus), nullableError(result.Err),
		nullableDuration(result.Duration), nullableObject(result.Response))

	query, args := ib.Build()
	if _, err := s.exec.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("webhook store: insert attempt: %w", err)
	}

	ub := sqlbuilder.PostgreSQL.NewUpdateBuilder()
	ub.Update(deliveriesTable)
	if result.Succeeded {
		ub.Set(
			"attempt_count = attempt_count + 1",
			ub.Assign("status", "succeeded"),
			"delivered_at = COALESCE(delivered_at, CURRENT_TIMESTAMP)",
		)
	} else {
		ub.Set(
			"attempt_count = attempt_count + 1",
			ub.Assign("status", "failed"),
		)
	}
	ub.Where(ub.E("id", id.UUID()))

	query, args = ub.Build()
	if _, err := s.exec.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("webhook store: record attempt: %w", err)
	}
	return nil
}

// ListAttempts returns the attempt history of one delivery in
// attempt order.
func (s *PostgresStore) ListAttempts(ctx context.Context, id DeliveryID) ([]AttemptRecord, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select(
		"attempt_number", "response_status", "error", "duration_ms",
		"response", "created_at",
	)
	sb.From(attemptsTable)
	sb.Where(sb.E("delivery_id", id.UUID()))
	sb.OrderBy("attempt_number ASC")

	query, args := sb.Build()
	rows, err := s.exec.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("webhook store: list attempts: %w", err)
	}
	defer rows.Close()

	out := []AttemptRecord{}
	for rows.Next() {
		var (
			number    int
			status    pgtype.Int4
			errorText *string
			duration  pgtype.Int4
			response  map[string]any
			created   pgtype.Timestamptz
		)
		if scanErr := rows.Scan(&number, &status, &errorText, &duration, &response, &created); scanErr != nil {
			return nil, fmt.Errorf("webhook store: scan attempt: %w", scanErr)
		}
		record := AttemptRecord{Number: number, Error: errorText, Response: response, CreatedAt: created.Time}
		if status.Valid {
			value := int(status.Int32)
			record.HTTPStatus = &value
		}
		if duration.Valid {
			value := int(duration.Int32)
			record.DurationMs = &value
		}
		record.Succeeded = record.Error == nil && record.HTTPStatus != nil &&
			*record.HTTPStatus >= 200 && *record.HTTPStatus < 300
		out = append(out, record)
	}
	return out, rows.Err()
}

// DeliveryForSend loads the immutable delivery bytes and event name
// for one attempt. The body is returned exactly as committed.
func (s *PostgresStore) DeliveryForSend(ctx context.Context, id DeliveryID) (PendingDelivery, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select("event", "body")
	sb.From(deliveriesTable)
	sb.Where(sb.E("id", id.UUID()))

	query, args := sb.Build()
	var event string
	var body []byte
	err := s.exec.QueryRow(ctx, query, args...).Scan(&event, &body)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return PendingDelivery{}, ErrNotFound
		}
		return PendingDelivery{}, fmt.Errorf("webhook store: delivery for send: %w", err)
	}
	return PendingDelivery{ID: id, Event: event, Body: body}, nil
}

// ListDeliveries pages the delivery logs, newest first, optionally scoped
// to one endpoint.
func (s *PostgresStore) ListDeliveries(ctx context.Context, params ListParams, webhookID *WebhookID) ([]Delivery, int, error) {
	count := sqlbuilder.PostgreSQL.NewSelectBuilder()
	count.Select("count(*)")
	count.From(deliveriesTable)
	if webhookID != nil {
		count.Where(count.E("webhook_id", webhookID.UUID()))
	}

	countQuery, countArgs := count.Build()
	var total int
	if err := s.exec.QueryRow(ctx, countQuery, countArgs...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("webhook store: count deliveries: %w", err)
	}

	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select(deliveryColumns...)
	sb.From(deliveriesTable + " d")
	// The latest attempt rides along for the listing view; a pending
	// delivery has no attempt row yet.
	sb.JoinWithOption(sqlbuilder.LeftJoin, attemptsTable+" a", "a.delivery_id = d.id AND a.attempt_number = d.attempt_count")
	if webhookID != nil {
		sb.Where(sb.E("d.webhook_id", webhookID.UUID()))
	}
	if params.Event != "" {
		sb.Where(sb.E("d.event", params.Event))
	}
	sb.OrderBy("d.created_at DESC", "d.id DESC")
	if !params.All() && params.Limit > 0 {
		sb.Limit(params.Limit).Offset(params.Offset())
	}

	query, args := sb.Build()
	rows, err := s.exec.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("webhook store: list deliveries: %w", err)
	}
	defer rows.Close()

	out := []Delivery{}
	for rows.Next() {
		entry, scanErr := scanDelivery(rows)
		if scanErr != nil {
			return nil, 0, scanErr
		}
		out = append(out, entry)
	}
	return out, total, rows.Err()
}

// PruneDeliveries deletes deliveries recorded before the cutoff;
// their attempt rows cascade.
func (s *PostgresStore) PruneDeliveries(ctx context.Context, before time.Time) (int64, error) {
	db := sqlbuilder.PostgreSQL.NewDeleteBuilder()
	db.DeleteFrom(deliveriesTable)
	db.Where(db.LT("created_at", before))

	query, args := db.Build()
	tag, err := s.exec.Exec(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("webhook store: prune deliveries: %w", err)
	}
	return tag.RowsAffected(), nil
}

func applyEndpointFilters(sb *sqlbuilder.SelectBuilder, params ListParams) {
	if params.Enabled != nil {
		sb.Where(sb.E("enabled", *params.Enabled))
	}
	if params.Event != "" {
		sb.Where(sb.Or(
			"CARDINALITY(event_types) = 0",
			eventMatch(sb, params.Event),
		))
	}
}

// eventMatch matches a text[] subscription column against one event
// name plus the wildcard, using array containment so the GIN index on
// event_types applies. sqlbuilder's In() expands to a row comparison,
// which Postgres rejects against a text[] column.
func eventMatch(sb *sqlbuilder.SelectBuilder, event string) string {
	return "(event_types @> ARRAY[" + sb.Var(event) + "]::text[] OR event_types @> ARRAY[" +
		sb.Var(AllEvents) + "]::text[])"
}

// scanner covers pgx.Rows and pgx.Row.
type scanner interface {
	Scan(dest ...any) error
}

// scanWebhook scans one endpoint row; order follows webhookColumns.
func scanWebhook(row scanner) (Webhook, error) {
	var (
		id          string
		hook        Webhook
		description pgtype.Text
		headers     map[string]string
		eventTypes  []string
		created     pgtype.Timestamptz
		updated     pgtype.Timestamptz
	)
	err := row.Scan(&id, &hook.Name, &description, &hook.Endpoint, &hook.Method,
		&headers, &hook.Enabled, &eventTypes, &created, &updated)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Webhook{}, ErrNotFound
		}
		return Webhook{}, fmt.Errorf("webhook store: scan: %w", err)
	}

	hook.ID = mustWebhookID(id)
	if description.Valid {
		text := description.String
		hook.Description = &text
	}
	hook.Headers = headers
	hook.EventTypes = eventTypes
	if hook.EventTypes == nil {
		hook.EventTypes = []string{}
	}
	hook.CreatedAt = created.Time
	if updated.Valid {
		hook.UpdatedAt = &updated.Time
	}
	return hook, nil
}

// scanDelivery scans one delivery row joined with its latest attempt;
// order follows deliveryColumns.
func scanDelivery(row scanner) (Delivery, error) {
	var (
		id          string
		webhookID   *string
		entry       Delivery
		event       *string
		attempts    int
		state       string
		created     pgtype.Timestamptz
		delivered   pgtype.Timestamptz
		respStatus  pgtype.Int4
		errorText   *string
		respObject  map[string]any
	)
	err := row.Scan(&id, &webhookID, &event, &attempts, &state, &created, &delivered,
		&respStatus, &errorText, &respObject)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Delivery{}, ErrNotFound
		}
		return Delivery{}, fmt.Errorf("webhook store: scan delivery: %w", err)
	}

	entry.ID = mustDeliveryID(id)
	if webhookID != nil {
		parsed := mustWebhookID(*webhookID)
		entry.WebhookID = &parsed
	}
	entry.Event = event
	entry.Attempts = attempts
	entry.Succeeded = state == "succeeded"
	entry.CreatedAt = created.Time
	if delivered.Valid {
		entry.DeliveredAt = &delivered.Time
	}
	if respStatus.Valid {
		value := int(respStatus.Int32)
		entry.HTTPStatus = &value
	}
	entry.Error = errorText
	entry.Response = respObject
	return entry, nil
}

func mustWebhookID(uuidText string) WebhookID {
	id, err := typeid.FromUUID[WebhookID](uuidText)
	if err != nil {
		panic(fmt.Sprintf("webhook: stored id %q is not a UUID: %v", uuidText, err))
	}
	return id
}

func mustDeliveryID(uuidText string) DeliveryID {
	id, err := typeid.FromUUID[DeliveryID](uuidText)
	if err != nil {
		panic(fmt.Sprintf("webhook: stored delivery id %q is not a UUID: %v", uuidText, err))
	}
	return id
}

// mapErr translates constraint violations into module errors.
func mapErr(err error) error {
	return datastore.MapErr(err, "webhook store", ErrNotFound, ErrDuplicateName)
}

// uuidOrNull renders a typed ID as the bare UUID its column stores.
func uuidOrNull(id *WebhookID) any {
	if id == nil || id.IsZero() {
		return nil
	}
	return id.UUID()
}

func nullableHeaders(headers map[string]string) any {
	if len(headers) == 0 {
		return nil
	}
	return headers
}

// nullableObject returns nil for an empty map so the column stays
// NULL instead of an empty JSON object.
func nullableObject(value map[string]any) any {
	if len(value) == 0 {
		return nil
	}
	return value
}

// nullableDuration renders a duration as whole milliseconds; a
// non-positive or unset duration stays NULL.
func nullableDuration(d time.Duration) any {
	if d <= 0 {
		return nil
	}
	return int(d.Milliseconds())
}

func nullableText(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableInt(value int) any {
	if value == 0 {
		return nil
	}
	return value
}

func nullableError(err error) any {
	if err == nil {
		return nil
	}
	return err.Error()
}
