package session

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/huandu/go-sqlbuilder"
	"go.jetify.com/typeid"
	"uuid"

	"github.com/riipandi/tango/internal/datastore"
)

// Repository reads and writes the session rows the lifecycle procedures
// manage. Every method takes the query surface, so the service passes either
// the pool or the transaction it runs in.
type Repository struct{}

// NewRepository builds the repository over the shared pool.
func NewRepository() *Repository {
	return &Repository{}
}

// sessionColumns are the columns the session procedures read, in scan order.
var sessionColumns = []string{
	"id", "user_id", "provider", "user_agent", "ip_address",
	"remember", "created_at", "expires_at", "refreshed_at", "revoked_at",
	// The ender reads through its text form: the column is a nullable UUID,
	// and a nil is the empty string the scan carries rather than a type the
	// scanner has no plan for.
	"COALESCE(revoked_by::text, '')",
}

// scanSession reads one row into the schema. The identifier arrives as the
// UUID the column stores and leaves as the typed id the callers hold.
func scanSession(scan func(dest ...any) error) (SessionSchema, uuid.UUID, error) {
	var row SessionSchema
	var rawID, rawEnder string
	err := scan(
		&rawID, &row.UserID, &row.Provider, &row.UserAgent, &row.IPAddress,
		&row.Remember, &row.CreatedAt, &row.ExpiresAt, &row.RefreshedAt, &row.RevokedAt,
		&rawEnder,
	)
	if err != nil {
		return SessionSchema{}, uuid.Nil(), err
	}
	if rawEnder != "" {
		ender, parseErr := uuid.Parse(rawEnder)
		if parseErr != nil {
			return SessionSchema{}, uuid.Nil(), fmt.Errorf("session: revoked_by: %w", parseErr)
		}
		row.RevokedBy = &ender
	}
	parsed, err := typeid.FromUUID[SessionID](rawID)
	if err != nil {
		return SessionSchema{}, uuid.Nil(), fmt.Errorf("session: id: %w", err)
	}
	row.ID = parsed
	raw, err := uuid.Parse(rawID)
	if err != nil {
		return SessionSchema{}, uuid.Nil(), fmt.Errorf("session: id: %w", err)
	}
	return row, raw, nil
}

// GetSession reads one session by its identifier.
func (r *Repository) GetSession(ctx context.Context, db datastore.Querier, id SessionID) (SessionSchema, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select(sessionColumns...)
	sb.From(SessionTable)
	sb.Where(sb.Equal("id", id.UUID()))

	query, args := sb.Build()
	row, _, err := scanSession(func(dest ...any) error {
		return db.QueryRow(ctx, query, args...).Scan(dest...)
	})
	if errors.Is(err, datastore.ErrNoRows) {
		return SessionSchema{}, datastore.ErrNoRows
	}
	if err != nil {
		return SessionSchema{}, fmt.Errorf("session: get: %w", err)
	}
	return row, nil
}

// FindActiveByTokenHash reads the session a presented refresh token resolves
// to. The WHERE clause is the whole gate: the hash must match, the session
// must be unrevoked and unexpired — a spent, ended, or expired token answers
// the same not-found, and the caller refuses it as one.
func (r *Repository) FindActiveByTokenHash(ctx context.Context, db datastore.Querier, hash string, now time.Time) (SessionSchema, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select(sessionColumns...)
	sb.From(SessionTable)
	sb.Where(sb.Equal("token_hash", hash), sb.IsNull("revoked_at"), sb.GT("expires_at", now))

	query, args := sb.Build()
	row, _, err := scanSession(func(dest ...any) error {
		return db.QueryRow(ctx, query, args...).Scan(dest...)
	})
	if errors.Is(err, datastore.ErrNoRows) {
		return SessionSchema{}, datastore.ErrNoRows
	}
	if err != nil {
		return SessionSchema{}, fmt.Errorf("session: find by token: %w", err)
	}
	return row, nil
}

// Revoke stamps the end of one session: who ended it, and when. The WHERE
// clause excludes an already-ended row, so a second revocation answers false
// — the state it names is the one the session is already in — and the
// service treats that as the success it is. The refresh token dies with the
// stamp; the access token does not, by the statelessness the protocol
// settles.
func (r *Repository) Revoke(ctx context.Context, db datastore.Querier, id SessionID, by uuid.UUID, at time.Time) (bool, error) {
	ub := sqlbuilder.PostgreSQL.NewUpdateBuilder()
	ub.Update(SessionTable)
	ub.Set(
		ub.Assign("revoked_at", at),
		ub.Assign("revoked_by", by),
	)
	ub.Where(ub.Equal("id", id.UUID()), ub.IsNull("revoked_at"))

	query, args := ub.Build()
	tag, err := db.Exec(ctx, query, args...)
	if err != nil {
		return false, fmt.Errorf("session: revoke: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// Rotate replaces a live session's refresh token and window. The WHERE
// clause is the race: a session that was revoked or expired between the read
// and this write answers false, and the renewal dies before it spent anything
// — the new secret is thrown away and the caller tries again.
func (r *Repository) Rotate(ctx context.Context, db datastore.Querier, id SessionID, hash string, refreshedAt, expiresAt time.Time) (bool, error) {
	ub := sqlbuilder.PostgreSQL.NewUpdateBuilder()
	ub.Update(SessionTable)
	ub.Set(
		ub.Assign("token_hash", hash),
		ub.Assign("refreshed_at", refreshedAt),
		ub.Assign("expires_at", expiresAt),
	)
	ub.Where(ub.Equal("id", id.UUID()), ub.IsNull("revoked_at"))

	query, args := ub.Build()
	tag, err := db.Exec(ctx, query, args...)
	if err != nil {
		return false, fmt.Errorf("session: rotate: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// ListOwn answers one page of the account's sessions, newest first, ended
// ones included. An ended session stays listed: the stamp is the fact a
// holder reads, not something to hide.
func (r *Repository) ListOwn(ctx context.Context, db datastore.Querier, userID uuid.UUID, offset, limit int) ([]SessionSchema, int, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select(sessionColumns...)
	sb.From(SessionTable)
	sb.Where(sb.Equal("user_id", userID))
	sb.OrderBy("created_at DESC", "id DESC")
	sb.Limit(limit).Offset(offset)

	query, args := sb.Build()
	rows, err := db.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("session: list: %w", err)
	}
	defer rows.Close()

	sessions := []SessionSchema{}
	for rows.Next() {
		row, _, scanErr := scanSession(rows.Scan)
		if scanErr != nil {
			return nil, 0, fmt.Errorf("session: list: %w", scanErr)
		}
		sessions = append(sessions, row)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("session: list: %w", err)
	}

	cb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	cb.Select("count(*)")
	cb.From(SessionTable)
	cb.Where(cb.Equal("user_id", userID))
	query, args = cb.Build()
	var total int
	if err := db.QueryRow(ctx, query, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("session: count: %w", err)
	}
	return sessions, total, nil
}
