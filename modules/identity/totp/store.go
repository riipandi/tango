package totp

// store.go persists the three MFA tables: the enrollment row, the
// hashed recovery codes, and the pending-auth bridge.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/huandu/go-sqlbuilder"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/modules/identity/user"
)

// Enrollment is one user_mfa_totp row.
type Enrollment struct {
	UserID       user.UserID
	SecretEnc    string
	Digits       int
	Period       int
	Algorithm    string
	ConfirmedAt  *time.Time
	LastUsedStep *int64
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Confirmed reports whether the enrollment is live.
func (e Enrollment) Confirmed() bool { return e.ConfirmedAt != nil }

// RecoveryCode is one user_mfa_recovery_codes row.
type RecoveryCode struct {
	CodeHash string
	UsedAt   *time.Time
}

// Store persists MFA state.
type Store interface {
	// UpsertUnconfirmed replaces an unconfirmed enrollment; a
	// confirmed row blocks re-enrollment.
	UpsertUnconfirmed(ctx context.Context, userID user.UserID, secretEnc string, digits, period int, algorithm string) error
	State(ctx context.Context, userID user.UserID) (Enrollment, error)
	MarkConfirmed(ctx context.Context, userID user.UserID, confirmedAt time.Time) error
	UpdateLastUsedStep(ctx context.Context, userID user.UserID, step int64) error
	DeleteState(ctx context.Context, userID user.UserID) error

	// ReplaceRecoveryCodes swaps the whole code set in one write.
	ReplaceRecoveryCodes(ctx context.Context, userID user.UserID, codeHashes []string) error
	RemainingRecoveryCodes(ctx context.Context, userID user.UserID) (int, error)
	// ConsumeRecoveryCode burns one code atomically; false means the
	// code was unknown or already spent.
	ConsumeRecoveryCode(ctx context.Context, userID user.UserID, codeHash string) (bool, error)

	// PutPending replaces the single pending-auth row.
	PutPending(ctx context.Context, userID user.UserID, tokenHash string, ttl time.Duration) error
	// PeekPending resolves the bridge owner without consuming it;
	// verification must succeed before the bridge is deleted.
	PeekPending(ctx context.Context, tokenHash string) (user.UserID, error)
	// DeletePending removes the bridge by its token hash.
	DeletePending(ctx context.Context, tokenHash string) error
	ConsumePending(ctx context.Context, tokenHash string) (user.UserID, error)
	DeletePendingForUser(ctx context.Context, userID user.UserID) error
}

// PostgresStore implements Store over the shared pool.
type PostgresStore struct {
	exec datastore.Executor
}

var _ Store = (*PostgresStore)(nil)

// NewPostgresStore builds the production MFA store.
func NewPostgresStore(exec datastore.Executor) *PostgresStore {
	return &PostgresStore{exec: exec}
}

const (
	totpTable     = "public.user_mfa_totp"
	codesTable    = "public.user_mfa_recovery_codes"
	pendingTable  = "public.user_mfa_pending"
	totpColumns   = "user_id, secret_enc, digits, period, algorithm, confirmed_at, last_used_step, created_at, updated_at"
	pendingColumn = "user_id"
)

// UpsertUnconfirmed replaces an unconfirmed enrollment; a confirmed
// row blocks re-enrollment.
func (s *PostgresStore) UpsertUnconfirmed(ctx context.Context, userID user.UserID, secretEnc string, digits, period int, algorithm string) error {
	ib := sqlbuilder.PostgreSQL.NewInsertBuilder()
	ib.InsertInto(totpTable)
	ib.Cols("user_id", "secret_enc", "digits", "period", "algorithm")
	ib.Values(userID.UUIDBytes(), secretEnc, digits, period, algorithm)
	ib.SQL("ON CONFLICT (user_id) DO UPDATE SET")
	ib.SQL("secret_enc = EXCLUDED.secret_enc, digits = EXCLUDED.digits, period = EXCLUDED.period,")
	ib.SQL("algorithm = EXCLUDED.algorithm, confirmed_at = NULL, last_used_step = NULL, updated_at = CURRENT_TIMESTAMP")
	// The guard keeps a confirmed enrollment: only unconfirmed rows
	// are replaceable.
	ib.SQL("WHERE " + totpTable + ".confirmed_at IS NULL")

	query, args := ib.Build()
	if _, err := s.exec.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("totp store: upsert unconfirmed: %w", err)
	}
	return nil
}

// State resolves the enrollment row for one user.
func (s *PostgresStore) State(ctx context.Context, userID user.UserID) (Enrollment, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select("secret_enc", "digits", "period", "algorithm", "confirmed_at", "last_used_step", "created_at", "updated_at")
	sb.From(totpTable)
	sb.Where(sb.E("user_id", userID.UUIDBytes()))

	query, args := sb.Build()
	var (
		secretEnc    string
		digits       int
		period       int
		algorithm    string
		confirmedAt  pgtype.Timestamptz
		lastUsedStep *int64
		createdAt    pgtype.Timestamptz
		updatedAt    pgtype.Timestamptz
	)
	err := s.exec.QueryRow(ctx, query, args...).Scan(&secretEnc, &digits, &period, &algorithm, &confirmedAt, &lastUsedStep, &createdAt, &updatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Enrollment{}, ErrNoEnrollment
		}
		return Enrollment{}, fmt.Errorf("totp store: state: %w", err)
	}
	row := Enrollment{
		UserID:       userID,
		SecretEnc:    secretEnc,
		Digits:       digits,
		Period:       period,
		Algorithm:    algorithm,
		LastUsedStep: lastUsedStep,
	}
	if confirmedAt.Valid {
		row.ConfirmedAt = &confirmedAt.Time
	}
	row.CreatedAt = createdAt.Time
	row.UpdatedAt = updatedAt.Time
	return row, nil
}

// MarkConfirmed stamps the confirmation timestamp; only an
// unconfirmed row flips.
func (s *PostgresStore) MarkConfirmed(ctx context.Context, userID user.UserID, confirmedAt time.Time) error {
	ub := sqlbuilder.PostgreSQL.NewUpdateBuilder()
	ub.Update(totpTable)
	ub.Set(ub.Assign("confirmed_at", confirmedAt.UTC()), ub.Assign("updated_at", time.Now().UTC()))
	ub.Where(ub.And(ub.E("user_id", userID.UUIDBytes()), ub.IsNull("confirmed_at")))

	query, args := ub.Build()
	tag, err := s.exec.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("totp store: confirm: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrAlreadyConfirmed
	}
	return nil
}

// UpdateLastUsedStep pins the replay watermark.
func (s *PostgresStore) UpdateLastUsedStep(ctx context.Context, userID user.UserID, step int64) error {
	ub := sqlbuilder.PostgreSQL.NewUpdateBuilder()
	ub.Update(totpTable)
	ub.Set(ub.Assign("last_used_step", step), ub.Assign("updated_at", time.Now().UTC()))
	ub.Where(ub.E("user_id", userID.UUIDBytes()))

	query, args := ub.Build()
	if _, err := s.exec.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("totp store: last used step: %w", err)
	}
	return nil
}

// DeleteState removes the enrollment and the recovery codes.
func (s *PostgresStore) DeleteState(ctx context.Context, userID user.UserID) error {
	db := sqlbuilder.PostgreSQL.NewDeleteBuilder()
	db.DeleteFrom(totpTable)
	db.Where(db.E("user_id", userID.UUIDBytes()))

	query, args := db.Build()
	if _, err := s.exec.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("totp store: delete state: %w", err)
	}
	return nil
}

// ReplaceRecoveryCodes swaps the whole code set in one write.
func (s *PostgresStore) ReplaceRecoveryCodes(ctx context.Context, userID user.UserID, codeHashes []string) error {
	db := sqlbuilder.PostgreSQL.NewDeleteBuilder()
	db.DeleteFrom(codesTable)
	db.Where(db.E("user_id", userID.UUIDBytes()))
	deleteQuery, deleteArgs := db.Build()

	if _, err := s.exec.Exec(ctx, deleteQuery, deleteArgs...); err != nil {
		return fmt.Errorf("totp store: clear codes: %w", err)
	}

	for _, codeHash := range codeHashes {
		ib := sqlbuilder.PostgreSQL.NewInsertBuilder()
		ib.InsertInto(codesTable)
		ib.Cols("user_id", "code_hash")
		ib.Values(userID.UUIDBytes(), codeHash)

		query, args := ib.Build()
		if _, err := s.exec.Exec(ctx, query, args...); err != nil {
			return fmt.Errorf("totp store: insert code: %w", err)
		}
	}
	return nil
}

// RemainingRecoveryCodes counts the unspent codes.
func (s *PostgresStore) RemainingRecoveryCodes(ctx context.Context, userID user.UserID) (int, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select("count(*)")
	sb.From(codesTable)
	sb.Where(sb.And(sb.E("user_id", userID.UUIDBytes()), sb.IsNull("used_at")))

	query, args := sb.Build()
	var remaining int
	if err := s.exec.QueryRow(ctx, query, args...).Scan(&remaining); err != nil {
		return 0, fmt.Errorf("totp store: remaining codes: %w", err)
	}
	return remaining, nil
}

// ConsumeRecoveryCode burns one code atomically; false means the
// code was unknown or already spent.
func (s *PostgresStore) ConsumeRecoveryCode(ctx context.Context, userID user.UserID, codeHash string) (bool, error) {
	ub := sqlbuilder.PostgreSQL.NewUpdateBuilder()
	ub.Update(codesTable)
	ub.Set(ub.Assign("used_at", time.Now().UTC()))
	ub.Where(ub.And(
		ub.E("user_id", userID.UUIDBytes()),
		ub.E("code_hash", codeHash),
		ub.IsNull("used_at"),
	))

	query, args := ub.Build()
	tag, err := s.exec.Exec(ctx, query, args...)
	if err != nil {
		return false, fmt.Errorf("totp store: consume code: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// PutPending replaces the single pending-auth row (one per user).
func (s *PostgresStore) PutPending(ctx context.Context, userID user.UserID, tokenHash string, ttl time.Duration) error {
	ib := sqlbuilder.PostgreSQL.NewInsertBuilder()
	ib.InsertInto(pendingTable)
	ib.Cols("user_id", "token_hash", "expires_at")
	ib.Values(userID.UUIDBytes(), tokenHash, time.Now().UTC().Add(ttl))
	ib.SQL("ON CONFLICT (user_id) DO UPDATE SET token_hash = EXCLUDED.token_hash, expires_at = EXCLUDED.expires_at, created_at = CURRENT_TIMESTAMP")

	query, args := ib.Build()
	if _, err := s.exec.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("totp store: put pending: %w", err)
	}
	return nil
}

// PeekPending resolves the bridge owner without consuming it; the
// bridge only dies after a successful verification.
func (s *PostgresStore) PeekPending(ctx context.Context, tokenHash string) (user.UserID, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select("user_id")
	sb.From(pendingTable)
	sb.Where(sb.And(sb.E("token_hash", tokenHash), sb.GT("expires_at", time.Now().UTC())))

	query, args := sb.Build()
	var userUUID string
	err := s.exec.QueryRow(ctx, query, args...).Scan(&userUUID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return user.UserID{}, ErrNoEnrollment
		}
		return user.UserID{}, fmt.Errorf("totp store: peek pending: %w", err)
	}
	return user.MustID(userUUID), nil
}

// DeletePending removes the bridge by its token hash.
func (s *PostgresStore) DeletePending(ctx context.Context, tokenHash string) error {
	db := sqlbuilder.PostgreSQL.NewDeleteBuilder()
	db.DeleteFrom(pendingTable)
	db.Where(db.E("token_hash", tokenHash))

	query, args := db.Build()
	if _, err := s.exec.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("totp store: delete pending: %w", err)
	}
	return nil
}

// ConsumePending deletes the pending row and returns its user; an
// unknown or expired bridge is not found.
func (s *PostgresStore) ConsumePending(ctx context.Context, tokenHash string) (user.UserID, error) {
	db := sqlbuilder.PostgreSQL.NewDeleteBuilder()
	db.DeleteFrom(pendingTable)
	db.Where(db.And(db.E("token_hash", tokenHash), db.GT("expires_at", time.Now().UTC())))
	db.Returning("user_id")

	query, args := db.Build()
	var userUUID string
	err := s.exec.QueryRow(ctx, query, args...).Scan(&userUUID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return user.UserID{}, ErrNoEnrollment
		}
		return user.UserID{}, fmt.Errorf("totp store: consume pending: %w", err)
	}
	return user.MustID(userUUID), nil
}

// DeletePendingForUser clears the bridge on sign-out.
func (s *PostgresStore) DeletePendingForUser(ctx context.Context, userID user.UserID) error {
	db := sqlbuilder.PostgreSQL.NewDeleteBuilder()
	db.DeleteFrom(pendingTable)
	db.Where(db.E("user_id", userID.UUIDBytes()))

	query, args := db.Build()
	if _, err := s.exec.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("totp store: clear pending: %w", err)
	}
	return nil
}
