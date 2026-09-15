package auditlog

import (
	"context"
	"fmt"
	"time"

	"github.com/huandu/go-sqlbuilder"
	"github.com/jackc/pgx/v5/pgtype"
	"go.jetify.com/typeid"

	"github.com/riipandi/tango/internal/datastore"
)

// PostgresStore persists audit entries in public.audit_logs. It
// builds on the shared Executor — never owns connections. Queries
// are composed with go-sqlbuilder (PostgreSQL flavor).
type PostgresStore struct {
	exec datastore.Executor
}

var _ Store = (*PostgresStore)(nil)

// NewPostgresStore builds the production audit log store.
func NewPostgresStore(exec datastore.Executor) *PostgresStore {
	return &PostgresStore{exec: exec}
}

// entryColumns is the SELECT list; keep order in sync with scanEntry.
var entryColumns = []string{
	"id", "event", "trigger_type", "action_status", "payload",
	"resource_type", "resource_id", "user_id", "ip_address", "user_agent", "created_at",
}

// Record inserts one entry; the row's uuidv7() default and
// CURRENT_TIMESTAMP fill zero ID/CreatedAt. pgx encodes the args by
// the target column OIDs (jsonb, enums, uuid, inet).
func (s *PostgresStore) Record(ctx context.Context, entry *Entry) error {
	payload := entry.Payload
	if payload == nil {
		payload = map[string]any{}
	}

	ib := sqlbuilder.PostgreSQL.NewInsertBuilder()
	ib.InsertInto("public.audit_logs")
	ib.Cols("event", "trigger_type", "action_status", "payload",
		"resource_type", "resource_id", "user_id", "ip_address", "user_agent", "created_at")
	ib.Values(
		entry.Event,
		string(orDefault(entry.Trigger, TriggerUser)),
		string(orDefault(entry.Status, StatusPending)),
		payload,
		valueOrNull(entry.ResourceType),
		uuidOrNull(entry.ResourceID),
		uuidOrNull(entry.UserID),
		uuidOrNull(entry.IPAddress),
		uuidOrNull(entry.UserAgent),
		createdAtOrNull(entry),
	)
	ib.Returning("id", "created_at")

	query, args := ib.Build()

	var (
		id        string
		createdAt pgtype.Timestamptz
	)
	if err := s.exec.QueryRow(ctx, query, args...).Scan(&id, &createdAt); err != nil {
		return fmt.Errorf("auditlog store: record: %w", err)
	}

	typed, convErr := typeid.FromUUID[AuditLogID](id)
	if convErr != nil {
		return fmt.Errorf("auditlog store: record: %w", convErr)
	}
	entry.ID = typed
	entry.CreatedAt = createdAt.Time
	return nil
}

// List returns matching entries newest first plus the total count.
// All-page params (-1) skip LIMIT/OFFSET.
func (s *PostgresStore) List(ctx context.Context, filters ListFilters, params Page) ([]Entry, int, error) {
	csb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	csb.Select("count(*)")
	csb.From("public.audit_logs")
	applyFilters(csb, filters)

	countQuery, countArgs := csb.Build()
	var total int
	if err := s.exec.QueryRow(ctx, countQuery, countArgs...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("auditlog store: count: %w", err)
	}

	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select(entryColumns...)
	sb.From("public.audit_logs")
	applyFilters(sb, filters)
	sb.OrderBy("created_at DESC", "id DESC")
	if !params.All() && params.Limit > 0 {
		sb.Limit(params.Limit).Offset(params.Offset())
	}

	query, args := sb.Build()
	rows, err := s.exec.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("auditlog store: list: %w", err)
	}
	defer rows.Close()

	entries := []Entry{}
	for rows.Next() {
		entry, scanErr := scanEntry(rows)
		if scanErr != nil {
			continue
		}
		entries = append(entries, entry)
	}
	return entries, total, nil
}

// UserFilterValues lists distinct users appearing in the log as
// "id:username" pairs joined from users.
func (s *PostgresStore) UserFilterValues(ctx context.Context) ([]string, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select("DISTINCT l.user_id, u.username")
	sb.From("public.audit_logs l")
	sb.Join("public.users u ON u.id = l.user_id")
	sb.OrderBy("u.username")

	query, args := sb.Build()
	rows, err := s.exec.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("auditlog store: user filters: %w", err)
	}
	defer rows.Close()

	out := []string{}
	for rows.Next() {
		var id, username string
		if scanErr := rows.Scan(&id, &username); scanErr != nil {
			continue
		}
		out = append(out, id+":"+username)
	}
	return out, rows.Err()
}

// ClientNameFilterValues lists distinct client names recorded in
// payloads (oidc clients carry client_name).
func (s *PostgresStore) ClientNameFilterValues(ctx context.Context) ([]string, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select("DISTINCT payload->>'client_name'")
	sb.From("public.audit_logs")
	sb.Where(sb.IsNotNull("payload->>'client_name'"))
	sb.OrderBy("payload->>'client_name'")

	query, args := sb.Build()
	rows, err := s.exec.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("auditlog store: client name filters: %w", err)
	}
	defer rows.Close()

	out := []string{}
	for rows.Next() {
		var name string
		if scanErr := rows.Scan(&name); scanErr != nil || name == "" {
			continue
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

// applyFilters composes the shared WHERE clauses.
func applyFilters(sb *sqlbuilder.SelectBuilder, filters ListFilters) {
	if filters.UserID != "" {
		sb.Where(sb.E("user_id", filters.UserID))
	}
	if filters.Event != "" {
		sb.Where(sb.E("event", filters.Event))
	}
	if filters.From != nil {
		sb.Where(sb.GE("created_at", *filters.From))
	}
	if filters.To != nil {
		sb.Where(sb.LE("created_at", *filters.To))
	}
}

// scanner covers pgx.Rows and pgx.Row.
type scanner interface {
	Scan(dest ...any) error
}

// scanEntry scans one row; keep order in sync with entryColumns.
func scanEntry(row scanner) (Entry, error) {
	var (
		id           string
		event        string
		trigger      string
		status       *string
		payload      map[string]any
		resourceType *string
		resourceID   *string
		userID       *string
		ipAddress    *string
		userAgent    *string
		createdAt    pgtype.Timestamptz
	)
	err := row.Scan(&id, &event, &trigger, &status, &payload,
		&resourceType, &resourceID, &userID, &ipAddress, &userAgent, &createdAt)
	if err != nil {
		return Entry{}, fmt.Errorf("auditlog store: scan: %w", err)
	}

	return Entry{
		ID:           mustEntryID(id),
		Event:        event,
		Trigger:      Trigger(trigger),
		Status:       statusPtr(status),
		Payload:      payload,
		ResourceType: valueOrZero(resourceType),
		ResourceID:   resourceID,
		UserID:       userID,
		IPAddress:    ipAddress,
		UserAgent:    userAgent,
		CreatedAt:    createdAt.Time,
	}, nil
}

func mustEntryID(uuidText string) AuditLogID {
	id, err := typeid.FromUUID[AuditLogID](uuidText)
	if err != nil {
		panic(fmt.Sprintf("auditlog: stored id %q is not a UUID: %v", uuidText, err))
	}
	return id
}

func statusPtr(s *string) Status {
	if s == nil {
		return StatusUnknown
	}
	return Status(*s)
}

func valueOrZero(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func orDefault[T ~string](value, fallback T) T {
	if value == "" {
		return fallback
	}
	return value
}

func uuidOrNull(s *string) any {
	if s == nil || *s == "" {
		return nil
	}
	return *s
}

// valueOrNull maps empty strings to SQL NULL.
func valueOrNull(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// createdAtOrNull maps a zero CreatedAt to the current time — an
// explicit NULL would violate the NOT NULL column instead of
// triggering the row default.
func createdAtOrNull(entry *Entry) any {
	if entry.CreatedAt.IsZero() {
		return time.Now().UTC()
	}
	return entry.CreatedAt
}
