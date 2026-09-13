package devicelogin

import (
	"context"
	"fmt"
	"time"

	"github.com/huandu/go-sqlbuilder"
	"github.com/jackc/pgx/v5/pgtype"
	"go.jetify.com/typeid"

	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/modules/identity/user"
)

// Store persists device login requests.
type Store interface {
	// Insert adds a pending request; collision on the code retried
	// by the caller (rare).
	Insert(ctx context.Context, request *Request) error
	// GetByCode resolves one request by its user code.
	GetByCode(ctx context.Context, code string) (*Request, error)
	// GetByID resolves one request by its row UUID (the exchange
	// path key).
	GetByID(ctx context.Context, id string) (*Request, error)
	// Decide approves or denies a pending request for a user;
	// non-pending rows fail (ErrInvalidRequest).
	Decide(ctx context.Context, code, decision, userID string) error
	// Consume claims one approved exchange atomically: only the
	// first exchange of an approved request mints a session.
	Consume(ctx context.Context, id string) error
}

// PostgresStore implements Store over the shared pool.
type PostgresStore struct {
	exec datastore.Executor
}

var _ Store = (*PostgresStore)(nil)

// NewPostgresStore builds the production store.
func NewPostgresStore(store datastore.Store) *PostgresStore {
	return &PostgresStore{exec: store}
}

// Insert adds a pending request.
func (s *PostgresStore) Insert(ctx context.Context, request *Request) error {
	ib := sqlbuilder.PostgreSQL.NewInsertBuilder()
	ib.InsertInto(requestsTable)
	ib.Cols("code", "device_token_hash", "status", "ip_address", "user_agent", "expires_at")
	ib.Values(request.Code, request.DeviceTokenHash, StatusPending, request.IPAddress, request.UserAgent, request.ExpiresAt)
	ib.Returning("id", "created_at")

	query, args := ib.Build()
	if err := s.exec.QueryRow(ctx, query, args...).Scan(&request.ID, &request.CreatedAt); err != nil {
		return fmt.Errorf("devicelogin store: insert: %w", err)
	}
	return nil
}

// GetByCode resolves one request by user code.
func (s *PostgresStore) GetByCode(ctx context.Context, code string) (*Request, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select(requestColumns...)
	sb.From(requestsTable)
	sb.Where(sb.E("code", code))

	query, args := sb.Build()
	return scanRequest(s.exec.QueryRow(ctx, query, args...))
}

// GetByID resolves one request by its row UUID.
func (s *PostgresStore) GetByID(ctx context.Context, id string) (*Request, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select(requestColumns...)
	sb.From(requestsTable)
	sb.Where(sb.E("id", id))

	query, args := sb.Build()
	return scanRequest(s.exec.QueryRow(ctx, query, args...))
}

// Decide transitions one pending request to approved/denied for a
// user; the atomic UPDATE enforces single-decision races. userID
// arrives as a typed ID string — normalized to the UUID column.
func (s *PostgresStore) Decide(ctx context.Context, code, decision, userID string) error {
	ub := sqlbuilder.PostgreSQL.NewUpdateBuilder()
	ub.Update(requestsTable)
	ub.Set(
		ub.Assign("status", decision),
		ub.Assign("user_id", userUUID(userID)),
	)
	ub.Where(ub.And(ub.E("code", code), ub.E("status", StatusPending), ub.GT("expires_at", time.Now().UTC())))

	query, args := ub.Build()
	tag, err := s.exec.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("devicelogin store: decide: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrInvalidRequest
	}
	return nil
}

// Consume claims one approved exchange atomically — only the first
// exchange mints a session.
func (s *PostgresStore) Consume(ctx context.Context, id string) error {
	ub := sqlbuilder.PostgreSQL.NewUpdateBuilder()
	ub.Update(requestsTable)
	ub.Set(ub.Assign("status", "consumed"))
	ub.Where(ub.And(ub.E("id", id), ub.E("status", StatusApproved), ub.GT("expires_at", time.Now().UTC())))

	query, args := ub.Build()
	tag, err := s.exec.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("devicelogin store: consume: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrInvalidRequest
	}
	return nil
}

// requestColumns is the SELECT list; keep in sync with scanRequest.
var requestColumns = []string{
	"id", "code", "status", "user_id", "device_token_hash", "ip_address", "user_agent", "created_at", "expires_at",
}

// scanRequest scans one row; keep in sync with requestColumns.
// user_id normalizes to the typeid string form (principals carry
// typeid strings; the column is a UUID).
func scanRequest(row scanner) (*Request, error) {
	var (
		userID  *string
		r       Request
		created pgtype.Timestamptz
		expires pgtype.Timestamptz
	)
	if err := row.Scan(&r.ID, &r.Code, &r.Status, &userID, &r.DeviceTokenHash, &r.IPAddress, &r.UserAgent, &created, &expires); err != nil {
		return nil, err
	}
	if userID != nil {
		typed := user.MustID(*userID).String()
		r.UserID = &typed
	}
	r.CreatedAt = created.Time
	r.ExpiresAt = expires.Time
	return &r, nil
}

// scanner covers pgx.Rows and pgx.Row.
type scanner interface {
	Scan(dest ...any) error
}

// userUUID normalizes a typed ID string (or bare UUID) to the UUID
// column form — user_id is a UUID column while principals carry
// typeid strings.
func userUUID(raw string) string {
	if id, err := typeid.FromString(raw); err == nil && !id.IsZero() {
		return id.UUID()
	}
	return raw
}
