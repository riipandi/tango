package datastore

import (
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"go.jetify.com/typeid"
)

// Conversion and error-mapping helpers shared by module stores. They
// exist so each module does not re-declare the same pgx glue.

// UserUUID converts a typed ID string (or bare UUID) to the UUID
// column form. Stores decoupled from the identity packages use this
// for user_id columns.
func UserUUID(raw string) string {
	if id, err := typeid.FromString(raw); err == nil && !id.IsZero() {
		return id.UUID()
	}
	return raw
}

// TimePtr flattens a timestamptz (invalid → nil).
func TimePtr(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	return &t.Time
}

// TextPtr flattens a text column (invalid → nil).
func TextPtr(t pgtype.Text) *string {
	if !t.Valid {
		return nil
	}
	return &t.String
}

func Ptr[T any](v T) *T { return &v }

// Deref flattens an optional value to its zero type (nil → zero).

// Deref flattens an optional value to its zero type (nil → zero).
func Deref[T any](p *T) T {
	if p == nil {
		var zero T
		return zero
	}
	return *p
}

// MapErr folds driver errors onto module sentinels: pgx.ErrNoRows →
// notFound, unique_violation (23505) → duplicate (pass nil to keep
// the wrap), everything else wraps with the store prefix.
func MapErr(err error, store string, notFound, duplicate error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return notFound
	}
	var pgErr *pgconn.PgError
	if duplicate != nil && errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return duplicate
	}
	return fmt.Errorf("%s: %w", store, err)
}

// Wrap adds operation context ("store: op: err") without re-mapping
// sentinels — callers that pre-mapped keep their error.
func Wrap(store, op string, err error) error {
	return fmt.Errorf("%s: %s: %w", store, op, err)
}
