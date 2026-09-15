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

// Tables owned by this module (upstream DDL, extended by migration
// 00027).
const (
	eventsTable = "public.webhook_events"
	logsTable   = "public.webhook_logs"
)

// PostgresStore persists endpoints and delivery logs.
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

// logColumns is the SELECT list for delivery logs; keep in sync with
// scanLog.
var logColumns = []string{
	"id", "webhook_id", "event", "http_status", "request", "response",
	"attempts", "succeeded", "error", "created_at", "updated_at",
}

// List pages the endpoints, newest first.
func (s *PostgresStore) List(ctx context.Context, params ListParams) ([]Webhook, int, error) {
	count := sqlbuilder.PostgreSQL.NewSelectBuilder()
	count.Select("count(*)")
	count.From(eventsTable)
	applyEndpointFilters(count, params)

	countQuery, countArgs := count.Build()
	var total int
	if err := s.exec.QueryRow(ctx, countQuery, countArgs...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("webhook store: count: %w", err)
	}

	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select(webhookColumns...)
	sb.From(eventsTable)
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
	sb.From(eventsTable)
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
	ib.InsertInto(eventsTable)
	ib.Cols("name", "description", "endpoint", "method", "headers", "enabled", "event_types", "secret")
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
	ub.Update(eventsTable)

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
	db.DeleteFrom(eventsTable)
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
	ub.Update(eventsTable)
	ub.Set(ub.Assign("secret", encryptedSecret))
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
	sb.From(eventsTable)
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
	sb.Select("secret")
	sb.From(eventsTable)
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

// InsertLog writes the outbox record through exec (pool or caller tx).
func (s *PostgresStore) InsertLog(ctx context.Context, exec Executor, log *DeliveryLog) error {
	ib := sqlbuilder.PostgreSQL.NewInsertBuilder()
	ib.InsertInto(logsTable)
	ib.Cols("webhook_id", "event", "request")
	ib.Values(uuidOrNull(log.WebhookID), log.Event, nullableObject(log.Request))
	ib.Returning("id", "created_at")

	query, args := ib.Build()
	var (
		id      string
		created pgtype.Timestamptz
	)
	if err := exec.QueryRow(ctx, query, args...).Scan(&id, &created); err != nil {
		return fmt.Errorf("webhook store: insert log: %w", err)
	}

	log.ID = mustLogID(id)
	log.CreatedAt = created.Time
	return nil
}

// RecordAttempt stores the latest delivery outcome on the log row,
// including the rendered request, so the operator can audit exactly
// what was sent and whether the signature verifies.
func (s *PostgresStore) RecordAttempt(ctx context.Context, id DeliveryLogID, result AttemptResult) error {
	ub := sqlbuilder.PostgreSQL.NewUpdateBuilder()
	ub.Update(logsTable)
	ub.Set(
		"attempts = attempts + 1",
		ub.Assign("succeeded", result.Succeeded),
		ub.Assign("http_status", nullableInt(result.HTTPStatus)),
		ub.Assign("request", nullableObject(result.Request)),
		ub.Assign("response", nullableObject(result.Response)),
		ub.Assign("error", nullableError(result.Err)),
	)
	ub.Where(ub.E("id", id.UUID()))

	query, args := ub.Build()
	if _, err := s.exec.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("webhook store: record attempt: %w", err)
	}
	return nil
}

// ListLogs pages the delivery logs, newest first, optionally scoped
// to one endpoint.
func (s *PostgresStore) ListLogs(ctx context.Context, params ListParams, webhookID *WebhookID) ([]DeliveryLog, int, error) {
	count := sqlbuilder.PostgreSQL.NewSelectBuilder()
	count.Select("count(*)")
	count.From(logsTable)
	if webhookID != nil {
		count.Where(count.E("webhook_id", webhookID.UUID()))
	}

	countQuery, countArgs := count.Build()
	var total int
	if err := s.exec.QueryRow(ctx, countQuery, countArgs...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("webhook store: count logs: %w", err)
	}

	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select(logColumns...)
	sb.From(logsTable)
	if webhookID != nil {
		sb.Where(sb.E("webhook_id", webhookID.UUID()))
	}
	if params.Event != "" {
		sb.Where(sb.E("event", params.Event))
	}
	sb.OrderBy("created_at DESC", "id DESC")
	if !params.All() && params.Limit > 0 {
		sb.Limit(params.Limit).Offset(params.Offset())
	}

	query, args := sb.Build()
	rows, err := s.exec.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("webhook store: list logs: %w", err)
	}
	defer rows.Close()

	out := []DeliveryLog{}
	for rows.Next() {
		entry, scanErr := scanLog(rows)
		if scanErr != nil {
			return nil, 0, scanErr
		}
		out = append(out, entry)
	}
	return out, total, rows.Err()
}

// PruneLogs deletes delivery logs recorded before the cutoff.
func (s *PostgresStore) PruneLogs(ctx context.Context, before time.Time) (int64, error) {
	db := sqlbuilder.PostgreSQL.NewDeleteBuilder()
	db.DeleteFrom(logsTable)
	db.Where(db.LT("created_at", before))

	query, args := db.Build()
	tag, err := s.exec.Exec(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("webhook store: prune logs: %w", err)
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

// scanLog scans one delivery log row; order follows logColumns.
func scanLog(row scanner) (DeliveryLog, error) {
	var (
		id        string
		webhookID *string
		entry     DeliveryLog
		event     *string
		status    pgtype.Int4
		request   map[string]any
		response  map[string]any
		attempts  int
		errorText *string
		created   pgtype.Timestamptz
		updated   pgtype.Timestamptz
		succeeded bool
	)
	err := row.Scan(&id, &webhookID, &event, &status, &request, &response,
		&attempts, &succeeded, &errorText, &created, &updated)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return DeliveryLog{}, ErrNotFound
		}
		return DeliveryLog{}, fmt.Errorf("webhook store: scan log: %w", err)
	}

	entry.ID = mustLogID(id)
	if webhookID != nil {
		parsed := mustWebhookID(*webhookID)
		entry.WebhookID = &parsed
	}
	entry.Event = event
	if status.Valid {
		value := int(status.Int32)
		entry.HTTPStatus = &value
	}
	entry.Request = request
	entry.Response = response
	entry.Attempts = attempts
	entry.Succeeded = succeeded
	entry.Error = errorText
	entry.CreatedAt = created.Time
	if updated.Valid {
		entry.UpdatedAt = &updated.Time
	}
	return entry, nil
}

func mustWebhookID(uuidText string) WebhookID {
	id, err := typeid.FromUUID[WebhookID](uuidText)
	if err != nil {
		panic(fmt.Sprintf("webhook: stored id %q is not a UUID: %v", uuidText, err))
	}
	return id
}

func mustLogID(uuidText string) DeliveryLogID {
	id, err := typeid.FromUUID[DeliveryLogID](uuidText)
	if err != nil {
		panic(fmt.Sprintf("webhook: stored log id %q is not a UUID: %v", uuidText, err))
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

func nullableObject(value map[string]any) any {
	if len(value) == 0 {
		return nil
	}
	return value
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
