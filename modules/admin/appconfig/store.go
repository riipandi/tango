package appconfig

import (
	"context"
	"fmt"
	"time"

	"github.com/huandu/go-sqlbuilder"

	"github.com/riipandi/tango/internal/datastore"
)

// Table constant: the settings key/value rows.
const appConfigTable = "public.app_config"

// Store persists configuration overrides. Catalog defaults are the
// bottom layer; rows here win. Environment-backed settings never
// reach this store.
type Store interface {
	List(ctx context.Context) (map[string]string, error)
	Upsert(ctx context.Context, values map[string]string) error
}

// PostgresStore persists overrides in public.app_config.
type PostgresStore struct {
	store datastore.Store
}

var _ Store = (*PostgresStore)(nil)

// NewPostgresStore builds the production config store. The store
// supplies both the read/write executor and transactions.
func NewPostgresStore(store datastore.Store) *PostgresStore {
	return &PostgresStore{store: store}
}

// List returns every stored override.
func (s *PostgresStore) List(ctx context.Context) (map[string]string, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select("key", "value")
	sb.From(appConfigTable)

	query, args := sb.Build()
	rows, err := s.store.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("appconfig store: list: %w", err)
	}
	defer rows.Close()

	out := map[string]string{}
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, fmt.Errorf("appconfig store: list: %w", err)
		}
		out[key] = value
	}
	return out, rows.Err()
}

// Upsert writes the given keys in one transaction.
func (s *PostgresStore) Upsert(ctx context.Context, values map[string]string) error {
	if len(values) == 0 {
		return nil
	}
	return s.store.WithTx(ctx, func(tx datastore.Executor) error {
		ib := sqlbuilder.PostgreSQL.NewInsertBuilder()
		ib.InsertInto(appConfigTable)
		ib.Cols("key", "value", "updated_at")
		now := time.Now().UTC()
		for key, value := range values {
			ib.Values(key, value, now)
		}
		ib.SQL("ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = EXCLUDED.updated_at")

		query, args := ib.Build()
		if _, err := tx.Exec(ctx, query, args...); err != nil {
			return fmt.Errorf("appconfig store: upsert: %w", err)
		}
		return nil
	})
}
