package customclaim

import (
	"context"
	"errors"
	"fmt"

	"github.com/huandu/go-sqlbuilder"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"go.jetify.com/typeid"

	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/modules/identity/usergroup"
)

// customClaimsTable backs user/group claims.
const customClaimsTable = "public.custom_claims"

// PostgresStore persists claims in public.custom_claims. It keeps
// the parent Store for WithTx (list-replace runs in a transaction).
type PostgresStore struct {
	store datastore.Store
	exec  datastore.Executor
}

var _ Store = (*PostgresStore)(nil)

// NewPostgresStore builds the production claim store.
func NewPostgresStore(store datastore.Store) *PostgresStore {
	return &PostgresStore{store: store, exec: store}
}

// ReplaceForUser swaps a user's whole claim set atomically.
func (s *PostgresStore) ReplaceForUser(ctx context.Context, userID user.UserID, params []UpsertParams) ([]CustomClaim, error) {
	return s.replace(ctx, func(exec datastore.Executor) error {
		return s.deleteFor(exec, "user_id", userID.UUIDBytes())
	}, params)
}

// ReplaceForGroup swaps a group's whole claim set atomically.
func (s *PostgresStore) ReplaceForGroup(ctx context.Context, groupID usergroup.UserGroupID, params []UpsertParams) ([]CustomClaim, error) {
	return s.replace(ctx, func(exec datastore.Executor) error {
		return s.deleteFor(exec, "user_group_id", groupID.UUIDBytes())
	}, params)
}

// replace deletes then re-inserts inside one transaction.
func (s *PostgresStore) replace(ctx context.Context, wipe func(datastore.Executor) error, params []UpsertParams) ([]CustomClaim, error) {
	var created []CustomClaim
	err := s.store.WithTx(ctx, func(tx datastore.Executor) error {
		if err := wipe(tx); err != nil {
			return err
		}
		created = created[:0]
		for _, param := range params {
			ib := sqlbuilder.PostgreSQL.NewInsertBuilder()
			ib.InsertInto(customClaimsTable)
			ib.Cols("key", "value", "user_id", "user_group_id")
			ib.Values(param.Key, param.Value, uuidOrNull(param.UserID), uuidOrNull(param.UserGroupID))
			ib.Returning(claimColumns...)

			query, args := ib.Build()
			row, err := scanClaim(tx.QueryRow(ctx, query, args...))
			if err != nil {
				return mapErr(err)
			}
			created = append(created, row)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("customclaim store: replace: %w", err)
	}
	if created == nil {
		created = []CustomClaim{}
	}
	return created, nil
}

// deleteFor removes every claim for one owner column.
func (s *PostgresStore) deleteFor(exec datastore.Executor, column string, id any) error {
	db := sqlbuilder.PostgreSQL.NewDeleteBuilder()
	db.DeleteFrom(customClaimsTable)
	db.Where(db.E(column, id))

	query, args := db.Build()
	if _, err := exec.Exec(context.Background(), query, args...); err != nil {
		return err
	}
	return nil
}

// claimColumns is the SELECT list; keep order in sync with scanClaim.
var claimColumns = []string{"id", "key", "value", "user_id", "user_group_id", "created_at"}

// Create inserts one claim; uuidv7() fills the ID.
func (s *PostgresStore) Create(ctx context.Context, params UpsertParams) (CustomClaim, error) {
	ib := sqlbuilder.PostgreSQL.NewInsertBuilder()
	ib.InsertInto(customClaimsTable)
	ib.Cols("key", "value", "user_id", "user_group_id")
	ib.Values(params.Key, params.Value, uuidOrNull(params.UserID), uuidOrNull(params.UserGroupID))
	ib.Returning(claimColumns...)

	query, args := ib.Build()
	claim, err := scanClaim(s.exec.QueryRow(ctx, query, args...))
	if err != nil {
		return CustomClaim{}, mapErr(err)
	}
	return claim, nil
}

// ExistsForOwner reports whether the (key, owner) pair is present.
func (s *PostgresStore) ExistsForOwner(ctx context.Context, params UpsertParams) (bool, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select("count(*)")
	sb.From(customClaimsTable)
	sb.Where(sb.E("key", params.Key))
	if params.UserID != nil {
		sb.Where(sb.E("user_id", *params.UserID))
	}
	if params.UserGroupID != nil {
		sb.Where(sb.E("user_group_id", *params.UserGroupID))
	}

	query, args := sb.Build()
	var n int
	if err := s.exec.QueryRow(ctx, query, args...).Scan(&n); err != nil {
		return false, fmt.Errorf("customclaim store: exists: %w", err)
	}
	return n > 0, nil
}

// Update replaces the value of one claim.
func (s *PostgresStore) Update(ctx context.Context, id CustomClaimID, value string) (CustomClaim, error) {
	ub := sqlbuilder.PostgreSQL.NewUpdateBuilder()
	ub.Update(customClaimsTable)
	ub.Set(ub.Assign("value", value))
	ub.Where(ub.E("id", id.UUIDBytes()))
	ub.Returning(claimColumns...)

	query, args := ub.Build()
	claim, err := scanClaim(s.exec.QueryRow(ctx, query, args...))
	if err != nil {
		return CustomClaim{}, mapErr(err)
	}
	return claim, nil
}

// Delete removes one claim.
func (s *PostgresStore) Delete(ctx context.Context, id CustomClaimID) error {
	db := sqlbuilder.PostgreSQL.NewDeleteBuilder()
	db.DeleteFrom(customClaimsTable)
	db.Where(db.E("id", id.UUIDBytes()))

	query, args := db.Build()
	tag, err := s.exec.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("customclaim store: delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ListByUser returns one user's claims, by key then value.
func (s *PostgresStore) ListByUser(ctx context.Context, userID user.UserID) ([]CustomClaim, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select(claimColumns...)
	sb.From(customClaimsTable)
	sb.Where(sb.E("user_id", userID.UUIDBytes()))
	sb.OrderBy("key")

	query, args := sb.Build()
	return s.list(ctx, sb, query, args)
}

// ListByGroup returns one group's claims.
func (s *PostgresStore) ListByGroup(ctx context.Context, groupID usergroup.UserGroupID) ([]CustomClaim, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select(claimColumns...)
	sb.From(customClaimsTable)
	sb.Where(sb.E("user_group_id", groupID.UUIDBytes()))
	sb.OrderBy("key")

	query, args := sb.Build()
	return s.list(ctx, sb, query, args)
}

// list runs the composed query and scans all rows.
func (s *PostgresStore) list(ctx context.Context, sb *sqlbuilder.SelectBuilder, query string, args []any) ([]CustomClaim, error) {
	rows, err := s.exec.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("customclaim store: list: %w", err)
	}
	defer rows.Close()

	out := []CustomClaim{}
	for rows.Next() {
		claim, scanErr := scanClaim(rows)
		if scanErr != nil {
			continue
		}
		out = append(out, claim)
	}
	return out, rows.Err()
}

// SuggestedKeys lists distinct claim keys already in use.
func (s *PostgresStore) SuggestedKeys(ctx context.Context) ([]string, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select("DISTINCT key")
	sb.From(customClaimsTable)
	sb.OrderBy("key")

	query, args := sb.Build()
	rows, err := s.exec.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("customclaim store: suggestions: %w", err)
	}
	defer rows.Close()

	out := []string{}
	for rows.Next() {
		var key string
		if scanErr := rows.Scan(&key); scanErr == nil && key != "" {
			out = append(out, key)
		}
	}
	return out, rows.Err()
}

// scanner covers pgx.Rows and pgx.Row.
type scanner interface {
	Scan(dest ...any) error
}

// scanClaim scans one row; keep order in sync with claimColumns.
func scanClaim(row scanner) (CustomClaim, error) {
	var (
		id      string
		claim   CustomClaim
		userID  *string
		group   *string
		created pgtype.Timestamptz
	)
	err := row.Scan(&id, &claim.Key, &claim.Value, &userID, &group, &created)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return CustomClaim{}, ErrNotFound
		}
		return CustomClaim{}, err
	}

	claim.ID = mustClaimID(id)
	claim.UserID = userID
	claim.UserGroupID = group
	claim.CreatedAt = created.Time
	return claim, nil
}

func mustClaimID(uuidText string) CustomClaimID {
	id, err := typeid.FromUUID[CustomClaimID](uuidText)
	if err != nil {
		panic(fmt.Sprintf("customclaim: stored id %q is not a UUID: %v", uuidText, err))
	}
	return id
}

func mapErr(err error) error {
	return datastore.MapErr(err, "customclaim store", ErrNotFound, ErrDuplicate)
}

func uuidOrNull(s *string) any {
	if s == nil || *s == "" {
		return nil
	}
	return *s
}
