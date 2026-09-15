package apikey

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/huandu/go-sqlbuilder"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"go.jetify.com/typeid"

	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/modules/identity/user"
)

// Table constant: the machine credential store.
const apiKeysTable = "public.api_keys"

// keyPrefix is the recognizable key head stored alongside the hash;
// the raw token itself never carries a prefix.
const keyPrefix = "pik"

// PostgresStore persists keys in public.api_keys.
type PostgresStore struct {
	exec datastore.Executor
}

var _ Store = (*PostgresStore)(nil)

// NewPostgresStore builds the production key store.
func NewPostgresStore(exec datastore.Executor) *PostgresStore {
	return &PostgresStore{exec: exec}
}

// keyColumns is the SELECT list; keep order in sync with scanKey.
var keyColumns = []string{"id", "name", "description", "expires_at", "created_at", "last_used_at", "expiration_email_sent_at"}

// Create inserts a key; uuidv7() fills the ID.
func (s *PostgresStore) Create(ctx context.Context, userID string, keyHash string, params CreateParams) (APIKey, error) {
	ib := sqlbuilder.PostgreSQL.NewInsertBuilder()
	ib.InsertInto(apiKeysTable)
	ib.Cols("user_id", "name", "prefix", "description", "key_hash", "expires_at")
	// prefix is the recognizable key head shown in the UI; the
	// lookup key itself is only the SHA-256 hash.
	ib.Values(datastore.UserUUID(userID), params.Name, keyPrefix, params.Description, []byte(keyHash), params.ExpiresAt)
	ib.Returning(keyColumns...)

	query, args := ib.Build()
	k, err := scanKey(s.exec.QueryRow(ctx, query, args...))
	if err != nil {
		return APIKey{}, mapErr(err)
	}
	return k, nil
}

// ListForUser pages the keys owned by one user, newest first.
func (s *PostgresStore) ListForUser(ctx context.Context, userID string, params ListParams) ([]APIKey, int, error) {
	csb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	csb.Select("count(*)")
	csb.From(apiKeysTable)
	csb.Where(csb.E("user_id", datastore.UserUUID(userID)))

	countQuery, countArgs := csb.Build()
	var total int
	if err := s.exec.QueryRow(ctx, countQuery, countArgs...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("apikey store: count: %w", err)
	}

	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select(keyColumns...)
	sb.From(apiKeysTable)
	sb.Where(sb.E("user_id", datastore.UserUUID(userID)))
	sb.OrderBy("created_at DESC", "id DESC")
	if !params.All() && params.Limit > 0 {
		sb.Limit(params.Limit).Offset(params.Offset())
	}

	query, args := sb.Build()
	rows, err := s.exec.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("apikey store: list: %w", err)
	}
	defer rows.Close()

	out := []APIKey{}
	for rows.Next() {
		k, scanErr := scanKey(rows)
		if scanErr != nil {
			continue
		}
		out = append(out, k)
	}
	return out, total, rows.Err()
}

// Revoke removes one key owned by the user.
func (s *PostgresStore) Revoke(ctx context.Context, userID string, id APIKeyID) error {
	db := sqlbuilder.PostgreSQL.NewDeleteBuilder()
	db.DeleteFrom(apiKeysTable)
	db.Where(db.E("id", id.UUIDBytes()), db.E("user_id", datastore.UserUUID(userID)))

	query, args := db.Build()
	tag, err := s.exec.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("apikey store: revoke: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Renew rotates the token hash and expiry of an expired key.
func (s *PostgresStore) Renew(ctx context.Context, userID string, id APIKeyID, keyHash string, expiresAt time.Time) (APIKey, error) {
	ub := sqlbuilder.PostgreSQL.NewUpdateBuilder()
	ub.Update(apiKeysTable)
	ub.Set(ub.Assign("key_hash", []byte(keyHash)), ub.Assign("expires_at", expiresAt))
	ub.Where(ub.E("id", id.UUIDBytes()), ub.E("user_id", datastore.UserUUID(userID)),
		"expires_at <= CURRENT_TIMESTAMP")
	ub.Returning(keyColumns...)

	query, args := ub.Build()
	k, err := scanKey(s.exec.QueryRow(ctx, query, args...))
	if err != nil {
		// scanKey maps ErrNoRows to ErrNotFound; distinguish
		// "unknown key" from "not expired yet" by re-probing.
		if errors.Is(err, ErrNotFound) {
			gsb := sqlbuilder.PostgreSQL.NewSelectBuilder()
			gsb.Select("count(*)")
			gsb.From(apiKeysTable)
			gsb.Where(gsb.E("id", id.UUIDBytes()), gsb.E("user_id", datastore.UserUUID(userID)))

			gQuery, gArgs := gsb.Build()
			var known int
			if countErr := s.exec.QueryRow(ctx, gQuery, gArgs...).Scan(&known); countErr == nil && known == 1 {
				return APIKey{}, ErrNotExpired
			}
			return APIKey{}, ErrNotFound
		}
		return APIKey{}, wrapErr("renew", err)
	}
	return k, nil
}

// ValidByHash resolves an unexpired key by hash, atomically bumping
// last_used_at in the same statement.
func (s *PostgresStore) ValidByHash(ctx context.Context, keyHash string) (APIKey, user.User, error) {
	ub := sqlbuilder.PostgreSQL.NewUpdateBuilder()
	ub.Update(apiKeysTable)
	ub.Set(ub.Assign("last_used_at", time.Now().UTC()))
	ub.Where(ub.E("key_hash", []byte(keyHash)), "expires_at > CURRENT_TIMESTAMP")
	ub.Returning(keyColumns...)

	query, args := ub.Build()
	k, err := scanKey(s.exec.QueryRow(ctx, query, args...))
	if err != nil {
		// scanKey maps ErrNoRows to ErrNotFound; an unexpired or
		// revoked key must not leak as "not found".
		if errors.Is(err, ErrNotFound) {
			return APIKey{}, user.User{}, ErrInvalidCreds
		}
		return APIKey{}, user.User{}, fmt.Errorf("apikey store: validate: %w", err)
	}
	u, err := s.userByID(ctx, k.ID)
	if err != nil {
		return APIKey{}, user.User{}, err
	}
	return k, u, nil
}

// userByID loads the owning user via the join on user_id.
func (s *PostgresStore) userByID(ctx context.Context, id APIKeyID) (user.User, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select("u.id")
	sb.From(apiKeysTable + " k")
	sb.Join("public.users u ON u.id = k.user_id")
	sb.Where(sb.E("k.id", id.UUIDBytes()))

	query, args := sb.Build()
	var uuidText string
	if err := s.exec.QueryRow(ctx, query, args...).Scan(&uuidText); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return user.User{}, ErrInvalidCreds
		}
		return user.User{}, fmt.Errorf("apikey store: owner: %w", err)
	}
	id2, parseErr := typeid.FromUUID[user.UserID](uuidText)
	if parseErr != nil {
		return user.User{}, fmt.Errorf("apikey store: owner id: %w", parseErr)
	}
	return user.NewPostgresStore(s.exec).GetByID(ctx, id2)
}

// scanner covers pgx.Rows and pgx.Row.
type scanner interface {
	Scan(dest ...any) error
}

// scanKey scans one row; keep order in sync with keyColumns.
func scanKey(row scanner) (APIKey, error) {
	var (
		id       string
		k        APIKey
		desc     pgtype.Text
		expires  pgtype.Timestamptz
		created  pgtype.Timestamptz
		lastUsed pgtype.Timestamptz
		emailAt  pgtype.Timestamptz
	)
	err := row.Scan(&id, &k.Name, &desc, &expires, &created, &lastUsed, &emailAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return APIKey{}, ErrNotFound
		}
		return APIKey{}, err
	}

	k.ID = mustKeyID(id)
	if desc.Valid {
		text := desc.String
		k.Description = &text
	}
	k.ExpiresAt = expires.Time
	k.CreatedAt = created.Time
	k.LastUsedAt = datastore.TimePtr(lastUsed)
	k.ExpirationEmailSentAt = datastore.TimePtr(emailAt)
	return k, nil
}

func mustKeyID(uuidText string) APIKeyID {
	id, err := typeid.FromUUID[APIKeyID](uuidText)
	if err != nil {
		panic(fmt.Sprintf("apikey: stored id %q is not a UUID: %v", uuidText, err))
	}
	return id
}

func mapErr(err error) error {
	return datastore.MapErr(err, "apikey store", ErrNotFound, ErrDuplicate)
}

// wrapErr wraps a store error without re-mapping ErrNoRows (callers
// that pre-map keep their sentinel).
func wrapErr(op string, err error) error {
	return datastore.Wrap("apikey store", op, err)
}

// NewToken draws an opaque 256-bit token, base64url-encoded.
func NewToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// HashToken derives the stored lookup key for a token.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return fmt.Sprintf("%x", sum)
}
