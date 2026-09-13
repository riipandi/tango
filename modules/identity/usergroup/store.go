package usergroup

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/huandu/go-sqlbuilder"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"go.jetify.com/typeid"

	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/modules/identity/user"
)

// Table constants: groups plus the membership junction.
const (
	userGroupsTable      = "public.user_groups"
	userGroupsUsersTable = "public.user_groups_users"
)

// PostgresStore persists groups in public.user_groups. Membership
// writes run inside a WithTx transaction.
type PostgresStore struct {
	store datastore.Store
	exec  datastore.Executor
}

var _ Store = (*PostgresStore)(nil)

// NewPostgresStore builds the production group store. The store arg
// supplies transactions; exec is the pool (kept for symmetric use
// with read paths).
func NewPostgresStore(store datastore.Store) *PostgresStore {
	return &PostgresStore{store: store, exec: store}
}

// groupColumns is the SELECT list; keep order in sync with scanGroup.
var groupColumns = []string{"id", "name", "display_name", "created_at", "updated_at"}

// Create inserts a group; uuidv7() fills the ID.
func (s *PostgresStore) Create(ctx context.Context, params CreateParams) (UserGroup, error) {
	ib := sqlbuilder.PostgreSQL.NewInsertBuilder()
	ib.InsertInto(userGroupsTable)
	ib.Cols("name", "display_name")
	ib.Values(params.Name, params.DisplayName)
	ib.Returning(groupColumns...)

	query, args := ib.Build()
	g, err := scanGroup(s.exec.QueryRow(ctx, query, args...))
	if err != nil {
		return UserGroup{}, mapErr(err)
	}
	return g, nil
}

// GetByID resolves one group.
func (s *PostgresStore) GetByID(ctx context.Context, id UserGroupID) (UserGroup, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select(groupColumns...)
	sb.From(userGroupsTable)
	sb.Where(sb.E("id", id.UUIDBytes()))

	query, args := sb.Build()
	g, err := scanGroup(s.exec.QueryRow(ctx, query, args...))
	if err != nil {
		return UserGroup{}, mapErr(err)
	}
	return g, nil
}

// Update patches fields and returns the fresh row.
func (s *PostgresStore) Update(ctx context.Context, id UserGroupID, params UpdateParams) (UserGroup, error) {
	ub := sqlbuilder.PostgreSQL.NewUpdateBuilder()

	var assignments []string
	if params.Name != nil {
		assignments = append(assignments, ub.Assign("name", *params.Name))
	}
	if params.DisplayName != nil {
		assignments = append(assignments, ub.Assign("display_name", *params.DisplayName))
	}
	if len(assignments) == 0 {
		return s.GetByID(ctx, id)
	}

	ub.Update(userGroupsTable)
	ub.Set(assignments...)
	ub.Where(ub.E("id", id.UUIDBytes()))
	ub.Returning(groupColumns...)

	query, args := ub.Build()
	g, err := scanGroup(s.exec.QueryRow(ctx, query, args...))
	if err != nil {
		return UserGroup{}, mapErr(err)
	}
	return g, nil
}

// Delete removes the group; memberships cascade.
func (s *PostgresStore) Delete(ctx context.Context, id UserGroupID) error {
	db := sqlbuilder.PostgreSQL.NewDeleteBuilder()
	db.DeleteFrom(userGroupsTable)
	db.Where(db.E("id", id.UUIDBytes()))

	query, args := db.Build()
	tag, err := s.exec.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("usergroup store: delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// List returns matching groups newest first plus the total count.
func (s *PostgresStore) List(ctx context.Context, params ListParams) ([]UserGroup, int, error) {
	csb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	csb.Select("count(*)")
	csb.From(userGroupsTable)
	if params.Query != "" {
		pattern := "%" + params.Query + "%"
		csb.Where(csb.Or(csb.Like("name", pattern), csb.Like("display_name", pattern)))
	}

	countQuery, countArgs := csb.Build()
	var total int
	if err := s.exec.QueryRow(ctx, countQuery, countArgs...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("usergroup store: count: %w", err)
	}

	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select(groupColumns...)
	sb.From(userGroupsTable)
	if params.Query != "" {
		pattern := "%" + params.Query + "%"
		sb.Where(sb.Or(sb.Like("name", pattern), sb.Like("display_name", pattern)))
	}
	sb.OrderBy("created_at DESC", "id DESC")
	if !params.All() && params.Limit > 0 {
		sb.Limit(params.Limit).Offset(params.Offset())
	}

	query, args := sb.Build()
	rows, err := s.exec.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("usergroup store: list: %w", err)
	}
	defer rows.Close()

	out := []UserGroup{}
	for rows.Next() {
		g, scanErr := scanGroup(rows)
		if scanErr != nil {
			continue
		}
		out = append(out, g)
	}
	return out, total, rows.Err()
}

// SetMembers atomically replaces the membership of one group. Every
// member ID must reference an existing user.
func (s *PostgresStore) SetMembers(ctx context.Context, id UserGroupID, memberIDs []user.UserID) error {
	return s.store.WithTx(ctx, func(tx datastore.Executor) error {
		db := sqlbuilder.PostgreSQL.NewDeleteBuilder()
		db.DeleteFrom(userGroupsUsersTable)
		db.Where(db.E("user_group_id", id.UUIDBytes()))

		delQuery, delArgs := db.Build()
		if _, err := tx.Exec(ctx, delQuery, delArgs...); err != nil {
			return fmt.Errorf("usergroup store: clear members: %w", err)
		}

		if len(memberIDs) == 0 {
			return nil
		}

		// FK violations would surface as a 500 downstream; verify the
		// whole batch exists for a deterministic domain error.
		sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
		sb.Select("count(DISTINCT id)")
		sb.From("public.users")
		ids := make([]any, 0, len(memberIDs))
		for _, member := range memberIDs {
			ids = append(ids, member.UUIDBytes())
		}
		sb.Where(sb.In("id", ids...))

		query, args := sb.Build()
		var known int
		if err := tx.QueryRow(ctx, query, args...).Scan(&known); err != nil {
			return fmt.Errorf("usergroup store: check members: %w", err)
		}
		if known != len(memberIDs) {
			return ErrInvalidIDs
		}

		ib := sqlbuilder.PostgreSQL.NewInsertBuilder()
		ib.InsertInto(userGroupsUsersTable)
		ib.Cols("user_id", "user_group_id")
		for _, member := range memberIDs {
			ib.Values(member.UUIDBytes(), id.UUIDBytes())
		}

		query, args = ib.Build()
		if _, err := tx.Exec(ctx, query, args...); err != nil {
			return fmt.Errorf("usergroup store: add members: %w", err)
		}
		return nil
	})
}

// ReplaceGroupsForUser atomically replaces the groups one user
// belongs to (inverse of SetMembers; upstream PUT
// /users/{id}/user-groups). Every group ID must exist.
func (s *PostgresStore) ReplaceGroupsForUser(ctx context.Context, id user.UserID, groupIDs []UserGroupID) error {
	return s.store.WithTx(ctx, func(tx datastore.Executor) error {
		db := sqlbuilder.PostgreSQL.NewDeleteBuilder()
		db.DeleteFrom(userGroupsUsersTable)
		db.Where(db.E("user_id", id.UUIDBytes()))

		delQuery, delArgs := db.Build()
		if _, err := tx.Exec(ctx, delQuery, delArgs...); err != nil {
			return fmt.Errorf("usergroup store: clear memberships: %w", err)
		}
		if len(groupIDs) == 0 {
			return nil
		}

		sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
		sb.Select("count(DISTINCT id)")
		sb.From(userGroupsTable)
		ids := make([]any, 0, len(groupIDs))
		for _, group := range groupIDs {
			ids = append(ids, group.UUIDBytes())
		}
		sb.Where(sb.In("id", ids...))

		query, args := sb.Build()
		var known int
		if err := tx.QueryRow(ctx, query, args...).Scan(&known); err != nil {
			return fmt.Errorf("usergroup store: check groups: %w", err)
		}
		if known != len(groupIDs) {
			return ErrInvalidIDs
		}

		ib := sqlbuilder.PostgreSQL.NewInsertBuilder()
		ib.InsertInto(userGroupsUsersTable)
		ib.Cols("user_id", "user_group_id")
		for _, group := range groupIDs {
			ib.Values(id.UUIDBytes(), group.UUIDBytes())
		}

		query, args = ib.Build()
		if _, err := tx.Exec(ctx, query, args...); err != nil {
			return fmt.Errorf("usergroup store: add memberships: %w", err)
		}
		return nil
	})
}

// MemberIDs lists the user IDs of one group.
func (s *PostgresStore) MemberIDs(ctx context.Context, id UserGroupID) ([]user.UserID, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select("user_id")
	sb.From(userGroupsUsersTable)
	sb.Where(sb.E("user_group_id", id.UUIDBytes()))
	sb.OrderBy("user_id")

	query, args := sb.Build()
	rows, err := s.exec.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("usergroup store: members: %w", err)
	}
	defer rows.Close()

	out := []user.UserID{}
	for rows.Next() {
		var raw string
		if scanErr := rows.Scan(&raw); scanErr != nil {
			continue
		}
		out = append(out, user.MustID(raw))
	}
	return out, rows.Err()
}

// GroupIDsForUser lists the groups a user belongs to.
func (s *PostgresStore) GroupIDsForUser(ctx context.Context, id user.UserID) ([]UserGroup, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select("g.id", "g.name", "g.display_name", "g.created_at", "g.updated_at")
	sb.From(userGroupsTable + " g")
	sb.Join(userGroupsUsersTable + " m ON m.user_group_id = g.id")
	sb.Where(sb.E("m.user_id", id.UUIDBytes()))
	sb.OrderBy("g.name")

	query, args := sb.Build()
	rows, err := s.exec.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("usergroup store: groups for user: %w", err)
	}
	defer rows.Close()

	out := []UserGroup{}
	for rows.Next() {
		g, scanErr := scanGroup(rows)
		if scanErr != nil {
			continue
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// scanner covers pgx.Rows and pgx.Row.
type scanner interface {
	Scan(dest ...any) error
}

// scanGroup scans one row; keep order in sync with groupColumns.
func scanGroup(row scanner) (UserGroup, error) {
	var (
		id        string
		g         UserGroup
		updatedAt pgtype.Timestamptz
		createdAt pgtype.Timestamptz
	)
	err := row.Scan(&id, &g.Name, &g.DisplayName, &createdAt, &updatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return UserGroup{}, ErrNotFound
		}
		return UserGroup{}, err
	}

	g.ID = mustGroupID(id)
	g.CreatedAt = createdAt.Time
	g.UpdatedAt = timePtr(updatedAt)
	return g, nil
}

func mustGroupID(uuidText string) UserGroupID {
	id, err := typeid.FromUUID[UserGroupID](uuidText)
	if err != nil {
		panic(fmt.Sprintf("usergroup: stored id %q is not a UUID: %v", uuidText, err))
	}
	return id
}

func mapErr(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique_violation
		return ErrDuplicate
	}
	return fmt.Errorf("usergroup store: %w", err)
}

func timePtr(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	return &t.Time
}
