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

// Shared conversion and database error helpers for module stores.

// UserUUID converts a typed ID string (or bare UUID) to the UUID
// column form. Stores decoupled from the identity packages use this
// for user_id columns.
func UserUUID(raw string) string {
	if id, err := typeid.FromString(raw); err == nil && !id.IsZero() {
		return id.UUID()
	}
	return raw
}

// TimePtr converts an invalid timestamptz to nil.
func TimePtr(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	return &t.Time
}

// TextPtr converts invalid text to nil.
func TextPtr(t pgtype.Text) *string {
	if !t.Valid {
		return nil
	}
	return &t.String
}

// Ptr returns a pointer to v.
func Ptr[T any](v T) *T { return &v }

// Deref returns the pointed value or its zero value when p is nil.
func Deref[T any](p *T) T {
	if p == nil {
		var zero T
		return zero
	}
	return *p
}

// MapErr maps common PostgreSQL errors to module sentinels.
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

// Wrap adds store and operation context without changing the error type.
func Wrap(store, op string, err error) error {
	return fmt.Errorf("%s: %s: %w", store, op, err)
}
