package jwks

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/huandu/go-sqlbuilder"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/riipandi/tango/internal/datastore"
)

// Rotation is a pair of writes: insert the new active key, retire
// the previous one with an expiry equal to the overlap window.
type Store interface {
	// Insert adds a generated key. encryptedPrivate must arrive
	// already encrypted.
	Insert(ctx context.Context, key *GeneratedKey, encryptedPrivate []byte) error
	// ActiveSigningKey returns the newest active signing key with a
	// usable private half; ErrNoActiveKey when none exists yet.
	ActiveSigningKey(ctx context.Context) (*StoredKey, error)
	// PublishedKeys returns public halves of every key visible in
	// JWKS: active keys plus retired ones inside the overlap window.
	PublishedKeys(ctx context.Context) ([]StoredKey, error)
	// RetireActive demotes the current signing key, keeping it
	// published for the overlap duration.
	RetireActive(ctx context.Context, retiredAt time.Time, overlap time.Duration) error
}

// StoredKey is one jwks row. PrivatePEM is decrypted only for the
// active signing key; the published JWKS path never sees it.
type StoredKey struct {
	KeyID      string
	Algorithm  string
	KeyType    string
	PublicPEM  []byte
	PrivatePEM []byte
	IsActive   bool
	ExpiresAt  *time.Time
	CreatedAt  time.Time
}

// ErrNoActiveKey is returned when no usable signing key exists.
var ErrNoActiveKey = errors.New("jwks: no active signing key")

// PostgresStore persists signing keys in public.jwks.
type PostgresStore struct {
	exec datastore.Executor
}

var _ Store = (*PostgresStore)(nil)

// NewPostgresStore builds the production key store.
func NewPostgresStore(store datastore.Store) *PostgresStore {
	return &PostgresStore{exec: store}
}

// keyColumns is the SELECT list; keep order in sync with scanKey.
var keyColumns = []string{"key_id", "algorithm", "key_type", "public_key", "private_key", "is_active", "expires_at", "created_at"}

// Insert adds a generated key. Private half arrives encrypted.
func (s *PostgresStore) Insert(ctx context.Context, key *GeneratedKey, encryptedPrivate []byte) error {
	ib := sqlbuilder.PostgreSQL.NewInsertBuilder()
	ib.InsertInto(jwksTable)
	ib.Cols("id", "key_id", "algorithm", "key_type", "public_key", "private_key", "use_for", "is_active", "created_at")
	ib.Values(key.ID.UUID(), key.KeyID, key.Algorithm, key.KeyType, key.PublicPEM, encryptedPrivate, UseSignature, true, key.ActivatedAt)

	query, args := ib.Build()
	_, err := s.exec.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("jwks store: insert: %w", err)
	}
	return nil
}

// ActiveSigningKey resolves the current signing key.
func (s *PostgresStore) ActiveSigningKey(ctx context.Context) (*StoredKey, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select(keyColumns...)
	sb.From(jwksTable)
	sb.Where(sb.And(
		sb.E("is_active", true),
		sb.E("use_for", UseSignature),
		sb.IsNotNull("private_key"),
	))
	sb.OrderBy("created_at DESC")
	sb.Limit(1)

	query, args := sb.Build()
	key, err := scanKey(s.exec.QueryRow(ctx, query, args...))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNoActiveKey
		}
		return nil, fmt.Errorf("jwks store: active key: %w", err)
	}
	return key, nil
}

// PublishedKeys lists keys for the JWKS document: active signing
// keys plus retired keys still inside their overlap window.
func (s *PostgresStore) PublishedKeys(ctx context.Context) ([]StoredKey, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select(keyColumns...)
	sb.From(jwksTable)
	sb.Where(sb.And(
		sb.E("use_for", UseSignature),
		sb.Or(
			sb.E("is_active", true),
			sb.GT("expires_at", time.Now().UTC()),
		),
	))
	sb.OrderBy("created_at DESC")

	query, args := sb.Build()
	rows, err := s.exec.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("jwks store: published keys: %w", err)
	}
	defer rows.Close()

	keys := []StoredKey{}
	for rows.Next() {
		key, scanErr := scanKey(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		keys = append(keys, *key)
	}
	return keys, rows.Err()
}

// RetireActive demotes every active signing key, keeping it
// published until now+overlap so pre-rotation tokens verify.
func (s *PostgresStore) RetireActive(ctx context.Context, retiredAt time.Time, overlap time.Duration) error {
	ub := sqlbuilder.PostgreSQL.NewUpdateBuilder()
	ub.Update(jwksTable)
	ub.Set(
		ub.Assign("is_active", false),
		ub.Assign("expires_at", retiredAt.Add(overlap)),
		ub.Assign("updated_at", retiredAt),
	)
	ub.Where(ub.And(ub.E("is_active", true), ub.E("use_for", UseSignature)))

	query, args := ub.Build()
	if _, err := s.exec.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("jwks store: retire: %w", err)
	}
	return nil
}

// scanner covers pgx.Rows and pgx.Row.
type scanner interface {
	Scan(dest ...any) error
}

// scanKey scans one row; keep order in sync with keyColumns.
func scanKey(row scanner) (*StoredKey, error) {
	var (
		expiresAt pgtype.Timestamptz
		createdAt pgtype.Timestamptz
		key       StoredKey
	)
	err := row.Scan(&key.KeyID, &key.Algorithm, &key.KeyType, &key.PublicPEM, &key.PrivatePEM, &key.IsActive, &expiresAt, &createdAt)
	if err != nil {
		return nil, err
	}
	if expiresAt.Valid {
		key.ExpiresAt = &expiresAt.Time
	}
	key.CreatedAt = createdAt.Time
	return &key, nil
}
