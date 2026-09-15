package scimsync

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/huandu/go-sqlbuilder"
	"go.jetify.com/typeid"

	"github.com/riipandi/tango/internal/datastore"
)

const providersTable = "public.scim_service_providers"

// PostgresStore persists service providers. Tokens are stored
// encrypted with the configured cipher; reads decrypt transparently.
type PostgresStore struct {
	exec   datastore.Executor
	cipher tokenCipher
}

// tokenCipher is the subset of pkg/crypto.Cipher used here; an
// interface keeps this module's tests free of crypto wiring.
type tokenCipher interface {
	Encrypt(plaintext string) (string, error)
	Decrypt(encoded string) (string, error)
}

// NewPostgresStore builds the store.
func NewPostgresStore(exec datastore.Executor, cipher tokenCipher) *PostgresStore {
	return &PostgresStore{exec: exec, cipher: cipher}
}

func scanProvider(row scanner) (ServiceProvider, error) {
	var (
		uuidText   string
		token      string
		lastSynced *time.Time
		p          ServiceProvider
	)
	if err := row.Scan(&uuidText, &p.Endpoint, &token, &p.OIDCClientID, &lastSynced, &p.CreatedAt); err != nil {
		return ServiceProvider{}, err
	}
	id, err := typeid.FromUUID[SCIMServiceProviderID](uuidText)
	if err != nil {
		return ServiceProvider{}, fmt.Errorf("scimsync: parse provider id: %w", err)
	}
	p.ID = id
	p.Token = token
	p.LastSyncedAt = lastSynced
	return p, nil
}

// scanner abstracts pgx.Rows and pgx.Row for row scanning.
type scanner interface {
	Scan(dest ...any) error
}

// providerSelect is the base column list ordered for scanProvider.
const providerCols = "id, endpoint, token, oidc_client_id, last_synced_at, created_at"

// GetByID loads one provider; ErrNotFound when missing.
func (s *PostgresStore) GetByID(ctx context.Context, id SCIMServiceProviderID) (ServiceProvider, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select(providerCols)
	sb.From(providersTable)
	sb.Where(sb.E("id", id.UUIDBytes()))
	query, args := sb.Build()
	p, err := s.queryProvider(ctx, query, args)
	if err != nil {
		return ServiceProvider{}, fmt.Errorf("scimsync: get provider: %w", err)
	}
	return s.decrypt(p), nil
}

// GetByClient loads the provider bound to an OIDC client; ErrNotFound
// when the client has none.
func (s *PostgresStore) GetByClient(ctx context.Context, clientID string) (ServiceProvider, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select(providerCols)
	sb.From(providersTable)
	sb.Where(sb.E("oidc_client_id", clientID))
	query, args := sb.Build()
	p, err := s.queryProvider(ctx, query, args)
	if err != nil {
		return ServiceProvider{}, fmt.Errorf("scimsync: get provider by client: %w", err)
	}
	return s.decrypt(p), nil
}

// List returns all providers (admin surface; the set is small).
func (s *PostgresStore) List(ctx context.Context) ([]ServiceProvider, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select(providerCols)
	sb.From(providersTable)
	sb.OrderBy("created_at")
	query, args := sb.Build()
	rows, err := s.exec.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("scimsync: list providers: %w", err)
	}
	defer rows.Close()

	var out []ServiceProvider
	for rows.Next() {
		p, err := scanProvider(rows)
		if err != nil {
			return nil, fmt.Errorf("scimsync: scan provider: %w", err)
		}
		out = append(out, s.decrypt(p))
	}
	return out, rows.Err()
}

// Create inserts a provider for a client; ErrDuplicate when the
// client already has one, ErrUnknownClient when the client is missing.
func (s *PostgresStore) Create(ctx context.Context, params UpsertParams) (ServiceProvider, error) {
	if err := s.ensureClient(ctx, params.OIDCClientID); err != nil {
		return ServiceProvider{}, err
	}
	encrypted, err := s.cipher.Encrypt(params.Token)
	if err != nil {
		return ServiceProvider{}, fmt.Errorf("scimsync: encrypt token: %w", err)
	}

	ib := sqlbuilder.PostgreSQL.NewInsertBuilder()
	ib.InsertInto(providersTable)
	ib.Cols("endpoint", "token", "oidc_client_id")
	ib.Values(params.Endpoint, encrypted, params.OIDCClientID)
	ib.SQL("RETURNING " + providerCols)
	query, args := ib.Build()
	p, err := s.queryProvider(ctx, query, args)
	if err != nil {
		if isUniqueViolation(err) {
			return ServiceProvider{}, ErrDuplicate
		}
		return ServiceProvider{}, fmt.Errorf("scimsync: create provider: %w", err)
	}
	// RETURNING hands back the ciphertext; callers get the plaintext
	// view like on reads (the token is shown once, on write).
	return s.decrypt(p), nil
}

// Update replaces endpoint/token for one provider.
func (s *PostgresStore) Update(ctx context.Context, id SCIMServiceProviderID, params UpsertParams) (ServiceProvider, error) {
	if err := s.ensureClient(ctx, params.OIDCClientID); err != nil {
		return ServiceProvider{}, err
	}
	encrypted, err := s.cipher.Encrypt(params.Token)
	if err != nil {
		return ServiceProvider{}, fmt.Errorf("scimsync: encrypt token: %w", err)
	}

	ub := sqlbuilder.PostgreSQL.NewUpdateBuilder()
	ub.Update(providersTable)
	ub.Set(
		ub.Assign("endpoint", params.Endpoint),
		ub.Assign("token", encrypted),
		ub.Assign("oidc_client_id", params.OIDCClientID),
	)
	ub.Where(ub.E("id", id.UUIDBytes()))
	ub.SQL("RETURNING " + providerCols)
	query, args := ub.Build()
	p, err := s.queryProvider(ctx, query, args)
	if err != nil {
		if isUniqueViolation(err) {
			return ServiceProvider{}, ErrDuplicate
		}
		if errors.Is(err, ErrNotFound) {
			return ServiceProvider{}, ErrNotFound
		}
		return ServiceProvider{}, fmt.Errorf("scimsync: update provider: %w", err)
	}
	// RETURNING hands back the ciphertext; callers get the plaintext
	// view like on reads (the token is shown once, on write).
	return s.decrypt(p), nil
}

// queryProvider runs a RETURNING query and drains it fully: pgx
// surfaces statement failures (unique violations included) through
// rows.Err(), not the Query return, so the error check happens after
// iteration. No rows at all maps to ErrNotFound.
func (s *PostgresStore) queryProvider(ctx context.Context, query string, args []any) (ServiceProvider, error) {
	rows, err := s.exec.Query(ctx, query, args...)
	if err != nil {
		return ServiceProvider{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return ServiceProvider{}, err
		}
		return ServiceProvider{}, ErrNotFound
	}
	p, scanErr := scanProvider(rows)
	if scanErr != nil {
		return ServiceProvider{}, scanErr
	}
	return p, rows.Err()
}

// Delete removes one provider.
func (s *PostgresStore) Delete(ctx context.Context, id SCIMServiceProviderID) error {
	db := sqlbuilder.PostgreSQL.NewDeleteBuilder()
	db.DeleteFrom(providersTable)
	db.Where(db.E("id", id.UUIDBytes()))
	query, args := db.Build()
	tag, err := s.exec.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("scimsync: delete provider: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// MarkSynced stamps last_synced_at after a successful push.
func (s *PostgresStore) MarkSynced(ctx context.Context, id SCIMServiceProviderID, at time.Time) error {
	ub := sqlbuilder.PostgreSQL.NewUpdateBuilder()
	ub.Update(providersTable)
	ub.Set(ub.Assign("last_synced_at", at))
	ub.Where(ub.E("id", id.UUIDBytes()))
	query, args := ub.Build()
	if _, err := s.exec.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("scimsync: mark synced: %w", err)
	}
	return nil
}

// ensureClient verifies the referenced OIDC client exists.
func (s *PostgresStore) ensureClient(ctx context.Context, clientID string) error {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select("count(*)")
	sb.From("public.oidc_clients")
	sb.Where(sb.E("id", clientID))
	query, args := sb.Build()
	var count int
	if err := s.exec.QueryRow(ctx, query, args...).Scan(&count); err != nil {
		return fmt.Errorf("scimsync: check client: %w", err)
	}
	if count == 0 {
		return ErrUnknownClient
	}
	return nil
}

func (s *PostgresStore) decrypt(p ServiceProvider) ServiceProvider {
	if plain, err := s.cipher.Decrypt(p.Token); err == nil {
		p.Token = plain
	}
	return p
}

func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "23505")
}

// snapshot source for the sync: allowed users and groups of a client.
// The oidc store answers both; the interface keeps scimsync decoupled.
type SnapshotSource interface {
	UsersForClient(ctx context.Context, clientID string) ([]ScimUserRow, error)
	GroupsForClient(ctx context.Context, clientID string) ([]ScimGroupRow, error)
}

// ScimUserRow is one user in a sync snapshot (plain strings: the SCIM
// payload is external-facing, typed IDs stop at the store).
type ScimUserRow struct {
	ID          string
	Username    string
	DisplayName string
	FirstName   string
	LastName    string
	Email       string
	Active      bool
}

// ScimGroupRow is one group in a sync snapshot with member user IDs.
type ScimGroupRow struct {
	ID      string
	Name    string
	Members []string
}
