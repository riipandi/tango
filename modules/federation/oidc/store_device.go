package oidc

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

// Device authorization statuses; status drives the token poll.
const (
	DeviceStatusPending  = "pending"
	DeviceStatusApproved = "approved"
	DeviceStatusDenied   = "denied"
	DeviceStatusConsumed = "consumed"
)

// DeviceCode is one oidc_device_codes row. Both codes live as
// SHA-256 hashes; Scope/Resource/Nonce carry the original request
// so the token grant can mint from the committed authorization.
type DeviceCode struct {
	DeviceCodeHash string
	UserCodeHash   string
	Scope          string
	Resource       string
	Nonce          string
	ClientID       string
	UserID         *string
	Status         string
	CreatedAt      time.Time
	ExpiresAt      time.Time
	ApprovedAt     *time.Time
	LastPolledAt   *time.Time
}

// InsertDeviceCode stores a fresh device authorization.
func (s *PostgresStore) InsertDeviceCode(ctx context.Context, code DeviceCode) error {
	ib := sqlbuilder.PostgreSQL.NewInsertBuilder()
	ib.InsertInto(oidcDeviceCodesTable)
	ib.Cols("device_code_hash", "user_code_hash", "scope", "resource", "nonce", "client_id", "status", "expires_at")
	ib.Values(code.DeviceCodeHash, code.UserCodeHash, code.Scope, nullableString(code.Resource), nullableString(code.Nonce),
		code.ClientID, DeviceStatusPending, code.ExpiresAt)

	query, args := ib.Build()
	if _, err := s.exec.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("oidc store: insert device code: %w", err)
	}
	return nil
}

// GetDeviceCode loads one authorization by its device-code hash
// (the token poll path); an unknown or expired code is invalid.
func (s *PostgresStore) GetDeviceCode(ctx context.Context, deviceCodeHash string) (DeviceCode, error) {
	return s.scanDeviceCode(s.exec.QueryRow(ctx, deviceCodeSelect+deviceCodeWhere, deviceCodeHash))
}

// GetDeviceCodeByUserCode resolves one authorization by its user-code
// hash (the browser approve path).
func (s *PostgresStore) GetDeviceCodeByUserCode(ctx context.Context, userCodeHash string) (DeviceCode, error) {
	return s.scanDeviceCode(s.exec.QueryRow(ctx, deviceCodeSelect+"user_code_hash = $1", userCodeHash))
}

// ApproveDeviceCode binds the user, marks the authorization approved,
// and returns nothing: the device poll sees the status flip.
func (s *PostgresStore) ApproveDeviceCode(ctx context.Context, userCodeHash, userID string) error {
	ub := sqlbuilder.PostgreSQL.NewUpdateBuilder()
	ub.Update(oidcDeviceCodesTable)
	ub.Set(
		ub.Assign("status", DeviceStatusApproved),
		// The wire form is a TypeID; the column stores the UUID.
		ub.Assign("user_id", datastore.UserUUID(userID)),
		ub.Assign("approved_at", time.Now().UTC()),
	)
	ub.Where(ub.And(
		ub.E("user_code_hash", userCodeHash),
		ub.E("status", DeviceStatusPending),
	))

	query, args := ub.Build()
	tag, err := s.exec.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("oidc store: approve device code: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrInvalidGrant
	}
	return nil
}

// DenyDeviceCode marks the authorization denied (user clicked deny).
func (s *PostgresStore) DenyDeviceCode(ctx context.Context, userCodeHash string) error {
	ub := sqlbuilder.PostgreSQL.NewUpdateBuilder()
	ub.Update(oidcDeviceCodesTable)
	ub.Set(ub.Assign("status", DeviceStatusDenied))
	ub.Where(ub.And(
		ub.E("user_code_hash", userCodeHash),
		ub.E("status", DeviceStatusPending),
	))

	query, args := ub.Build()
	tag, err := s.exec.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("oidc store: deny device code: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrInvalidGrant
	}
	return nil
}

// ConsumeDeviceCode atomically flips an approved, unexpired
// authorization to consumed and returns it: the poll that sees
// approved wins the tokens, every other poll sees consumed.
func (s *PostgresStore) ConsumeDeviceCode(ctx context.Context, deviceCodeHash string) (DeviceCode, error) {
	db := sqlbuilder.PostgreSQL.NewDeleteBuilder()
	// Consumption deletes the row: a consumed authorization must not
	// answer another poll, and expired rows are swept by the index.
	db.DeleteFrom(oidcDeviceCodesTable)
	db.Where(db.And(
		db.E("device_code_hash", deviceCodeHash),
		db.E("status", DeviceStatusApproved),
		db.GT("expires_at", time.Now().UTC()),
	))
	db.Returning("device_code_hash", "user_code_hash", "scope", "resource", "nonce", "client_id", "user_id", "created_at", "expires_at", "approved_at")

	query, args := db.Build()
	row := s.exec.QueryRow(ctx, query, args...)

	var code DeviceCode
	var resource, nonce *string
	var userID *string
	var approvedAt pgtype.Timestamptz
	err := row.Scan(&code.DeviceCodeHash, &code.UserCodeHash, &code.Scope, &resource, &nonce,
		&code.ClientID, &userID, &code.CreatedAt, &code.ExpiresAt, &approvedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return code, ErrInvalidGrant
		}
		return code, fmt.Errorf("oidc store: consume device code: %w", err)
	}
	code.Status = DeviceStatusConsumed
	code.Resource = derefString(resource)
	code.Nonce = derefString(nonce)
	code.UserID = userID
	if approvedAt.Valid {
		code.ApprovedAt = &approvedAt.Time
	}
	return code, nil
}

// TouchDevicePoll stamps the last poll time for slow-down tracking.
func (s *PostgresStore) TouchDevicePoll(ctx context.Context, deviceCodeHash string, at time.Time) error {
	ub := sqlbuilder.PostgreSQL.NewUpdateBuilder()
	ub.Update(oidcDeviceCodesTable)
	ub.Set(ub.Assign("last_polled_at", at))
	ub.Where(ub.E("device_code_hash", deviceCodeHash))

	query, args := ub.Build()
	if _, err := s.exec.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("oidc store: touch device poll: %w", err)
	}
	return nil
}

// PruneDeviceCodes deletes expired device authorizations.
func (s *PostgresStore) PruneDeviceCodes(ctx context.Context, before time.Time) (int64, error) {
	db := sqlbuilder.PostgreSQL.NewDeleteBuilder()
	db.DeleteFrom(oidcDeviceCodesTable)
	db.Where(db.LT("expires_at", before))

	query, args := db.Build()
	tag, err := s.exec.Exec(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("oidc store: prune device codes: %w", err)
	}
	return tag.RowsAffected(), nil
}

const deviceCodeSelect = `SELECT device_code_hash, user_code_hash, scope, resource, nonce, ` +
	`client_id, user_id, status, created_at, expires_at, approved_at, last_polled_at FROM public.oidc_device_codes WHERE `

const deviceCodeWhere = "device_code_hash = $1"

func (s *PostgresStore) scanDeviceCode(row scanner) (DeviceCode, error) {
	var code DeviceCode
	var resource, nonce *string
	var userID *string
	var approvedAt, lastPolled pgtype.Timestamptz
	err := row.Scan(&code.DeviceCodeHash, &code.UserCodeHash, &code.Scope, &resource, &nonce,
		&code.ClientID, &userID, &code.Status, &code.CreatedAt, &code.ExpiresAt, &approvedAt, &lastPolled)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return code, ErrInvalidGrant
		}
		return code, fmt.Errorf("oidc store: scan device code: %w", err)
	}
	code.Resource = derefString(resource)
	code.Nonce = derefString(nonce)
	code.UserID = userID
	if approvedAt.Valid {
		code.ApprovedAt = &approvedAt.Time
	}
	if lastPolled.Valid {
		code.LastPolledAt = &lastPolled.Time
	}
	return code, nil
}

// UpdateDeviceCodeGrant stamps the resolved audience and granted
// scope subset on an approval, so the poll mints exactly what the
// user consented to.
func (s *PostgresStore) UpdateDeviceCodeGrant(ctx context.Context, userCodeHash, audience, scope string) error {
	ub := sqlbuilder.PostgreSQL.NewUpdateBuilder()
	ub.Update(oidcDeviceCodesTable)
	ub.Set(
		ub.Assign("resource", nullableString(audience)),
		ub.Assign("scope", scope),
	)
	ub.Where(ub.E("user_code_hash", userCodeHash))

	query, args := ub.Build()
	if _, err := s.exec.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("oidc store: update device grant: %w", err)
	}
	return nil
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
