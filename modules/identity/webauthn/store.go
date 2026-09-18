package webauthn

import (
	"context"
	"errors"
	"fmt"
	"time"

	jsonv2 "encoding/json/v2"

	"github.com/huandu/go-sqlbuilder"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/modules/identity/user"
	"go.jetify.com/typeid"
)

// Store persists ceremony sessions and credentials.
type Store interface {
	// SaveChallengeSession inserts a ceremony session row; the ID is
	// the uuidv7 default — returned for the consume-by-ID exchange.
	SaveChallengeSession(ctx context.Context, session ChallengeSession) (string, error)
	// ConsumeSessionByID deletes the session row (one-time) and
	// returns it; expired or unknown sessions fail.
	ConsumeSessionByID(ctx context.Context, id string) (*ChallengeSession, error)
	// ListCredentials returns a user's passkeys, newest first.
	ListCredentials(ctx context.Context, userID user.UserID) ([]StoredCredential, error)
	// InsertCredential stores a freshly registered passkey; the row
	// ID is the uuidv7 default.
	InsertCredential(ctx context.Context, credential *StoredCredential) error
	// DeleteCredential removes one passkey owned by the user.
	DeleteCredential(ctx context.Context, userID user.UserID, credentialID CredentialID) (bool, error)
	// RenameCredential updates the display name.
	RenameCredential(ctx context.Context, userID user.UserID, credentialID CredentialID, name string) (*StoredCredential, error)
}

// ChallengeSession is one webauthn_sessions row (ceremony state).
type ChallengeSession struct {
	ID           string
	UserID       *string
	Challenge    string
	Type         string
	Verification string
	CredParams   []byte
	Extensions   []byte
	ExpiresAt    time.Time
}

// StoredCredential is one webauthn_credentials row.
type StoredCredential struct {
	ID              CredentialID
	UserID          user.UserID
	Name            string
	CredentialID    []byte
	PublicKey       []byte
	DeviceType      string
	AttestationType string
	Transport       []string
	BackupEligible  bool
	BackupState     bool
	AAGUID          *string
	CreatedAt       time.Time
	LastUsedAt      *time.Time
}

// PostgresStore implements Store over the shared pool.
type PostgresStore struct {
	exec datastore.Executor
}

var _ Store = (*PostgresStore)(nil)

// NewPostgresStore builds the production store.
func NewPostgresStore(store datastore.Store) *PostgresStore {
	return &PostgresStore{exec: store}
}

// SaveChallengeSession inserts the ceremony row and returns its ID.
func (s *PostgresStore) SaveChallengeSession(ctx context.Context, session ChallengeSession) (string, error) {
	var userID any
	if session.UserID != nil {
		userID = *session.UserID
	}

	ib := sqlbuilder.PostgreSQL.NewInsertBuilder()
	ib.InsertInto(sessionsTable)
	ib.Cols("user_id", "challenge", "challenge_type", "user_verification", "credential_params", "extensions", "expires_at")
	ib.Values(userID, session.Challenge, session.Type, session.Verification, session.CredParams, session.Extensions, session.ExpiresAt)
	ib.Returning("id")

	query, args := ib.Build()
	var id string
	if err := s.exec.QueryRow(ctx, query, args...).Scan(&id); err != nil {
		return "", fmt.Errorf("webauthn store: save session: %w", err)
	}
	return id, nil
}

// ConsumeSessionByID deletes and returns the ceremony row —
// one-time use via the DELETE itself; expired rows return no rows.
func (s *PostgresStore) ConsumeSessionByID(ctx context.Context, id string) (*ChallengeSession, error) {
	db := sqlbuilder.PostgreSQL.NewDeleteBuilder()
	db.DeleteFrom(sessionsTable)
	db.Where(db.And(db.E("id", id), db.GT("expires_at", time.Now().UTC())))
	db.Returning("user_id", "challenge", "challenge_type", "user_verification", "credential_params", "extensions", "expires_at")

	query, args := db.Build()
	var session ChallengeSession
	var userID *string
	var expiresAt pgtype.Timestamptz
	err := s.exec.QueryRow(ctx, query, args...).Scan(
		&userID, &session.Challenge, &session.Type, &session.Verification, &session.CredParams, &session.Extensions, &expiresAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrInvalidSession
		}
		return nil, fmt.Errorf("webauthn store: consume session: %w", err)
	}
	session.ID = id
	session.UserID = userID
	session.ExpiresAt = expiresAt.Time
	return &session, nil
}

// credentialColumns is the SELECT list; keep in sync with scanCredential.
var credentialColumns = []string{
	"id", "user_id", "name", "credential_id", "public_key", "device_type", "attestation_type",
	"transport", "backup_eligible", "backup_state", "aaguid", "created_at", "last_used_at",
}

// ListCredentials returns a user's passkeys, newest first.
func (s *PostgresStore) ListCredentials(ctx context.Context, userID user.UserID) ([]StoredCredential, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select(credentialColumns...)
	sb.From(credentialsTable)
	sb.Where(sb.E("user_id", userID.UUIDBytes()))
	sb.OrderBy("created_at DESC")

	query, args := sb.Build()
	rows, err := s.exec.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("webauthn store: list credentials: %w", err)
	}
	defer rows.Close()

	out := []StoredCredential{}
	for rows.Next() {
		row, scanErr := scanCredential(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, *row)
	}
	return out, rows.Err()
}

// InsertCredential stores a fresh passkey.
func (s *PostgresStore) InsertCredential(ctx context.Context, credential *StoredCredential) error {
	ib := sqlbuilder.PostgreSQL.NewInsertBuilder()
	ib.InsertInto(credentialsTable)
	ib.Cols("user_id", "name", "credential_id", "public_key", "device_type", "attestation_type", "transport", "backup_eligible", "backup_state", "aaguid")
	ib.Values(
		credential.UserID.UUIDBytes(), credential.Name, credential.CredentialID, credential.PublicKey,
		credential.DeviceType, credential.AttestationType, jsonOrNil(credential.Transport),
		credential.BackupEligible, credential.BackupState, credential.AAGUID,
	)
	ib.Returning(credentialColumns...)

	query, args := ib.Build()
	row, err := scanCredential(s.exec.QueryRow(ctx, query, args...))
	if err != nil {
		return fmt.Errorf("webauthn store: insert credential: %w", err)
	}
	*credential = *row
	return nil
}

// DeleteCredential removes one passkey; false when not found.
func (s *PostgresStore) DeleteCredential(ctx context.Context, userID user.UserID, credentialID CredentialID) (bool, error) {
	db := sqlbuilder.PostgreSQL.NewDeleteBuilder()
	db.DeleteFrom(credentialsTable)
	db.Where(db.And(db.E("id", credentialID.UUIDBytes()), db.E("user_id", userID.UUIDBytes())))

	query, args := db.Build()
	tag, err := s.exec.Exec(ctx, query, args...)
	if err != nil {
		return false, fmt.Errorf("webauthn store: delete credential: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// RenameCredential updates the passkey display name.
func (s *PostgresStore) RenameCredential(ctx context.Context, userID user.UserID, credentialID CredentialID, name string) (*StoredCredential, error) {
	ub := sqlbuilder.PostgreSQL.NewUpdateBuilder()
	ub.Update(credentialsTable)
	ub.Set(ub.Assign("name", name))
	ub.Where(ub.And(ub.E("id", credentialID.UUIDBytes()), ub.E("user_id", userID.UUIDBytes())))
	ub.Returning(credentialColumns...)

	query, args := ub.Build()
	row, err := scanCredential(s.exec.QueryRow(ctx, query, args...))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("webauthn store: rename credential: %w", err)
	}
	return row, nil
}

// scanner covers pgx.Rows and pgx.Row.
type scanner interface {
	Scan(dest ...any) error
}

// scanCredential scans one row; keep in sync with credentialColumns.
func scanCredential(row scanner) (*StoredCredential, error) {
	var (
		id         string
		userID     string
		c          StoredCredential
		transport  []byte
		aaguid     *string
		createdAt  pgtype.Timestamptz
		lastUsedAt pgtype.Timestamptz
	)
	if err := row.Scan(
		&id, &userID, &c.Name, &c.CredentialID, &c.PublicKey, &c.DeviceType, &c.AttestationType,
		&transport, &c.BackupEligible, &c.BackupState, &aaguid, &createdAt, &lastUsedAt,
	); err != nil {
		return nil, err
	}

	credentialID, err := typeid.FromUUID[CredentialID](id)
	if err != nil {
		return nil, fmt.Errorf("webauthn store: credential id %q is not a UUID: %w", id, err)
	}
	c.ID = credentialID
	c.UserID = user.MustID(userID)
	c.AAGUID = aaguid
	c.CreatedAt = createdAt.Time
	if lastUsedAt.Valid {
		c.LastUsedAt = &lastUsedAt.Time
	}
	_ = jsonv2.Unmarshal(transport, &c.Transport)
	if c.Transport == nil {
		c.Transport = []string{}
	}
	return &c, nil
}

// jsonOrNil encodes a slice for a JSONB column.
func jsonOrNil(value []string) []byte {
	if value == nil {
		return []byte("[]")
	}
	encoded, _ := jsonv2.Marshal(value)
	return encoded
}
