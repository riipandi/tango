package jwks

import (
	"context"
	"fmt"
	"time"

	"github.com/huandu/go-sqlbuilder"

	"github.com/riipandi/tango/internal/datastore"
)

// Repository reads the keys the database publishes.
type Repository struct {
	db datastore.Querier
}

// NewRepository builds the repository over the shared pool.
func NewRepository(db datastore.Querier) *Repository {
	return &Repository{db: db}
}

// ActiveSigningKeys returns the keys that are currently valid for signature
// verification: active, marked `sig`, and either without an expiry or with one
// still in the future.
//
// The private key column is not selected, so a private key cannot reach a
// caller of this method. A row whose stored public key cannot be read as a
// key is skipped rather than failing the whole set: one unusable row must not
// take the published keyset down for every client.
func (r *Repository) ActiveSigningKeys(ctx context.Context) ([]StoredKey, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select(
		ColumnKeyID,
		ColumnAlgorithm,
		ColumnPublicKey,
		ColumnExpiresAt,
	)
	sb.From(TableJWKS)
	sb.Where(
		sb.Equal(ColumnIsActive, true),
		sb.Equal(ColumnUseFor, UseSignature),
		sb.Or(
			sb.IsNull(ColumnExpiresAt),
			sb.GreaterThan(ColumnExpiresAt, time.Now()),
		),
	)
	sb.OrderBy(ColumnKeyID)
	query, args := sb.Build()

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("jwks: read active keys: %w", err)
	}
	defer rows.Close()

	var keys []StoredKey
	for rows.Next() {
		var key StoredKey
		if err := rows.Scan(&key.KeyID, &key.Algorithm, &key.PublicKey, &key.ExpiresAt); err != nil {
			return nil, fmt.Errorf("jwks: scan active key: %w", err)
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("jwks: read active keys: %w", err)
	}
	return keys, nil
}
