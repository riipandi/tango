package session

import (
	"context"
	"fmt"
	"time"

	"github.com/huandu/go-sqlbuilder"
	"github.com/jackc/pgx/v5/pgtype"
	"go.jetify.com/typeid"

	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/modules/identity/user"
)

// sessionsTable is the table backing sign-in sessions.
const sessionsTable = "public.sessions"

// PostgresStore persists sessions in public.sessions; the token hash
// is the lookup key, the TypeID string is the public identifier.
type PostgresStore struct {
	exec datastore.Executor
}

var _ Store = (*PostgresStore)(nil)

// NewPostgresStore builds the production session store.
func NewPostgresStore(exec datastore.Executor) *PostgresStore {
	return &PostgresStore{exec: exec}
}

// Create inserts a session; ID, TokenHash, and ExpiresAt are preset
// by the service.
// sessionUUID converts the TypeID form to the bare UUID the column
// stores; the ID must already be a valid session TypeID.
func sessionUUID(id string) string {
	parsed, err := identity.ParseID[SessionID](id)
	if err != nil {
		return id
	}
	return parsed.UUID()
}

// sessionTypeID converts the stored UUID back to the TypeID form;
// unparseable values pass through unchanged.
func sessionTypeID(id string) string {
	parsed, err := typeid.FromUUID[SessionID](id)
	if err != nil {
		return id
	}
	return parsed.String()
}

func (s *PostgresStore) Create(ctx context.Context, se *Session) error {
	ib := sqlbuilder.PostgreSQL.NewInsertBuilder()
	ib.InsertInto(sessionsTable)
	ib.Cols("id", "user_id", "provider", "token_hash", "user_agent", "device_name", "ip_address", "expires_at")
	ib.Values(sessionUUID(se.ID), se.UserID.UUIDBytes(), se.Provider, se.TokenHash,
		textOrNull(datastore.Deref(se.UserAgent)), textOrNull(datastore.Deref(se.DeviceName)), textOrNull(datastore.Deref(se.IPAddress)), se.ExpiresAt)
	ib.Returning("created_at")

	query, args := ib.Build()
	var createdAt pgtype.Timestamptz
	if err := s.exec.QueryRow(ctx, query, args...).Scan(&createdAt); err != nil {
		return mapErr(err)
	}
	se.CreatedAt = createdAt.Time
	return nil
}

// ValidByTokenHash resolves one live session with its user; expired
// and revoked rows are invisible (indistinguishable from missing).
func (s *PostgresStore) ValidByTokenHash(ctx context.Context, tokenHash string) (Session, user.User, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select(
		"s.id", "s.provider", "s.token_hash", "s.user_agent", "s.device_name", "s.ip_address",
		"s.created_at", "s.expires_at", "s.refreshed_at", "s.revoked_at",
		"u.id", "u.username", "u.email", "u.first_name", "u.last_name",
		"u.display_name", "u.avatar_url", "u.locale", "u.is_admin", "u.disabled",
		"u.email_verified_at", "u.created_at", "u.updated_at", "u.last_login_at",
	)
	sb.From(sessionsTable + " s")
	sb.Join("public.users u ON u.id = s.user_id")
	sb.Where(sb.E("s.token_hash", tokenHash), sb.IsNull("s.revoked_at"),
		sb.GT("s.expires_at", time.Now().UTC()))

	query, args := sb.Build()

	var (
		se          Session
		userAgent   pgtype.Text
		deviceName  pgtype.Text
		ipAddress   pgtype.Text
		refreshedAt pgtype.Timestamptz
		revokedAt   pgtype.Timestamptz

		uid             string
		username        string
		email           string
		firstName       pgtype.Text
		lastName        pgtype.Text
		displayName     string
		avatarURL       pgtype.Text
		locale          pgtype.Text
		isAdmin         bool
		disabled        bool
		emailVerifiedAt pgtype.Timestamptz
		uCreatedAt      pgtype.Timestamptz
		uUpdatedAt      pgtype.Timestamptz
		uLastLoginAt    pgtype.Timestamptz
	)
	err := s.exec.QueryRow(ctx, query, args...).Scan(
		&se.ID, &se.Provider, &se.TokenHash, &userAgent, &deviceName, &ipAddress,
		&se.CreatedAt, &se.ExpiresAt, &refreshedAt, &revokedAt,
		&uid, &username, &email, &firstName, &lastName,
		&displayName, &avatarURL, &locale, &isAdmin, &disabled,
		&emailVerifiedAt, &uCreatedAt, &uUpdatedAt, &uLastLoginAt,
	)
	if err != nil {
		return Session{}, user.User{}, mapErr(err)
	}

	se.ID = sessionTypeID(se.ID)
	se.UserID = user.MustID(uid)
	se.UserAgent = datastore.TextPtr(userAgent)
	se.DeviceName = datastore.TextPtr(deviceName)
	se.IPAddress = datastore.TextPtr(ipAddress)
	se.RefreshedAt = datastore.TimePtr(refreshedAt)
	se.RevokedAt = datastore.TimePtr(revokedAt)

	u := user.User{
		ID:              se.UserID,
		Username:        username,
		Email:           email,
		FirstName:       datastore.TextPtr(firstName),
		LastName:        datastore.TextPtr(lastName),
		AvatarURL:       datastore.TextPtr(avatarURL),
		Locale:          datastore.TextPtr(locale),
		DisplayName:     displayName,
		IsAdmin:         isAdmin,
		Disabled:        disabled,
		EmailVerifiedAt: datastore.TimePtr(emailVerifiedAt),
		CreatedAt:       uCreatedAt.Time,
		UpdatedAt:       datastore.TimePtr(uUpdatedAt),
		LastLoginAt:     datastore.TimePtr(uLastLoginAt),
	}
	return se, u, nil
}

// Touch extends a live session's expiry (sliding window) and marks
// the refresh time.
func (s *PostgresStore) Touch(ctx context.Context, id string, expiresAt time.Time) error {
	ub := sqlbuilder.PostgreSQL.NewUpdateBuilder()
	ub.Update(sessionsTable)
	ub.Set(ub.Assign("expires_at", expiresAt), ub.Assign("refreshed_at", time.Now().UTC()))
	ub.Where(ub.E("id", id), ub.IsNull("revoked_at"))

	query, args := ub.Build()
	_, err := s.exec.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("session touch: %w", err)
	}
	return nil
}

// RevokeByTokenHash revokes the session matching a token hash.
func (s *PostgresStore) RevokeByTokenHash(ctx context.Context, tokenHash string) error {
	ub := sqlbuilder.PostgreSQL.NewUpdateBuilder()
	ub.Update(sessionsTable)
	ub.Set(ub.Assign("revoked_at", time.Now().UTC()))
	ub.Where(ub.E("token_hash", tokenHash), ub.IsNull("revoked_at"))

	query, args := ub.Build()
	if _, err := s.exec.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("session revoke: %w", err)
	}
	return nil
}

// RevokeForUser revokes one session owned by the user; unknown or
// foreign session IDs surface ErrNotFound.
func (s *PostgresStore) RevokeForUser(ctx context.Context, userID user.UserID, sessionID string) error {
	ub := sqlbuilder.PostgreSQL.NewUpdateBuilder()
	ub.Update(sessionsTable)
	ub.Set(ub.Assign("revoked_at", time.Now().UTC()))
	ub.Where(ub.E("id", sessionUUID(sessionID)), ub.E("user_id", userID.UUIDBytes()), ub.IsNull("revoked_at"))

	query, args := ub.Build()
	tag, err := s.exec.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("session revoke: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// RevokeAllForUser revokes every live session, optionally sparing
// one (the caller's current session).
func (s *PostgresStore) RevokeAllForUser(ctx context.Context, userID user.UserID, exceptID string) error {
	ub := sqlbuilder.PostgreSQL.NewUpdateBuilder()
	ub.Update(sessionsTable)
	ub.Set(ub.Assign("revoked_at", time.Now().UTC()))
	if exceptID == "" {
		ub.Where(ub.E("user_id", userID.UUIDBytes()), ub.IsNull("revoked_at"))
	} else {
		ub.Where(ub.E("user_id", userID.UUIDBytes()), ub.IsNull("revoked_at"),
			ub.NE("id", sessionUUID(exceptID)))
	}

	query, args := ub.Build()
	if _, err := s.exec.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("session revoke all: %w", err)
	}
	return nil
}

// ListActiveForUser returns live sessions, newest first.
func (s *PostgresStore) ListActiveForUser(ctx context.Context, userID user.UserID) ([]Session, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select("id", "user_id", "provider", "token_hash", "user_agent", "device_name",
		"ip_address", "created_at", "expires_at", "refreshed_at", "revoked_at")
	sb.From(sessionsTable)
	sb.Where(sb.E("user_id", userID.UUIDBytes()), sb.IsNull("revoked_at"),
		sb.GT("expires_at", time.Now().UTC()))
	sb.OrderBy("created_at DESC")

	query, args := sb.Build()
	rows, err := s.exec.Query(ctx, query, args...)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()

	out := []Session{}
	for rows.Next() {
		var (
			se          Session
			uid         string
			userAgent   pgtype.Text
			deviceName  pgtype.Text
			ipAddress   pgtype.Text
			createdAt   pgtype.Timestamptz
			expiresAt   pgtype.Timestamptz
			refreshedAt pgtype.Timestamptz
			revokedAt   pgtype.Timestamptz
		)
		if scanErr := rows.Scan(&se.ID, &uid, &se.Provider, &se.TokenHash, &userAgent,
			&deviceName, &ipAddress, &createdAt, &expiresAt, &refreshedAt, &revokedAt); scanErr != nil {
			continue
		}
		se.ID = sessionTypeID(se.ID)
		se.UserID = user.MustID(uid)
		se.UserAgent = datastore.TextPtr(userAgent)
		se.DeviceName = datastore.TextPtr(deviceName)
		se.IPAddress = datastore.TextPtr(ipAddress)
		se.CreatedAt = createdAt.Time
		se.ExpiresAt = expiresAt.Time
		se.RefreshedAt = datastore.TimePtr(refreshedAt)
		se.RevokedAt = datastore.TimePtr(revokedAt)
		out = append(out, se)
	}
	return out, rows.Err()
}

// mapErr folds driver errors into the Store contract.
func mapErr(err error) error {
	return datastore.MapErr(err, "session store", ErrNotFound, nil)
}

// textOrNull keeps empty strings as SQL NULL.
func textOrNull(s string) any {
	if s == "" {
		return nil
	}
	return s
}
