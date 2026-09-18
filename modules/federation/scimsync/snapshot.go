package scimsync

import (
	"context"
	"fmt"

	"github.com/huandu/go-sqlbuilder"

	"github.com/riipandi/tango/internal/datastore"
)

// SnapshotSource adapts the identity stores to the SCIM snapshot
// contract, scoped by the client's group allowlist. It reads the
// identity tables directly (raw SQL, no identity import) because the
// rows are cross-module projections.
type IdentitySnapshotSource struct {
	db datastore.Store
}

// NewSnapshotSource builds the identity-backed snapshot source.
func NewIdentitySnapshotSource(db datastore.Store) IdentitySnapshotSource {
	return IdentitySnapshotSource{db: db}
}

func (s IdentitySnapshotSource) UsersForClient(ctx context.Context, clientID string) ([]ScimUserRow, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select("DISTINCT u.id", "u.username", "u.display_name", "u.first_name", "u.last_name", "u.email", "u.disabled")
	sb.From("public.users AS u")
	sb.Join("public.user_groups_users AS ugu", "ugu.user_id = u.id")
	sb.Join("public.oidc_clients_allowed_user_groups AS ag", "ag.user_group_id = ugu.user_group_id")
	sb.Where(sb.E("ag.oidc_client_id", clientID))
	return scimUserRows(ctx, s.db, sb)
}

func (s IdentitySnapshotSource) GroupsForClient(ctx context.Context, clientID string) ([]ScimGroupRow, error) {
	// Groups the client may see, with their member user IDs.
	groups := sqlbuilder.PostgreSQL.NewSelectBuilder()
	groups.Select("g.id", "g.name")
	groups.From("public.user_groups AS g")
	groups.Join("public.oidc_clients_allowed_user_groups AS ag", "ag.user_group_id = g.id")
	groups.Where(groups.E("ag.oidc_client_id", clientID))
	gQuery, gArgs := groups.Build()

	rows, err := s.db.Query(ctx, gQuery, gArgs...)
	if err != nil {
		return nil, fmt.Errorf("scimsync: snapshot groups: %w", err)
	}
	defer rows.Close()

	var out []ScimGroupRow
	ids := make([]string, 0, 8)
	for rows.Next() {
		var row ScimGroupRow
		var idText string
		if scanErr := rows.Scan(&idText, &row.Name); scanErr != nil {
			return nil, fmt.Errorf("scimsync: scan group: %w", scanErr)
		}
		row.ID = idText
		ids = append(ids, idText)
		out = append(out, row)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, rowsErr
	}
	if len(out) == 0 {
		return out, nil
	}

	// Members of those groups.
	members := sqlbuilder.PostgreSQL.NewSelectBuilder()
	members.Select("user_group_id", "user_id")
	members.From("public.user_groups_users")
	members.Where(members.In("user_group_id", toAny(ids)...))
	mQuery, mArgs := members.Build()
	mRows, err := s.db.Query(ctx, mQuery, mArgs...)
	if err != nil {
		return nil, fmt.Errorf("scimsync: snapshot members: %w", err)
	}
	defer mRows.Close()

	byGroup := map[string][]string{}
	for mRows.Next() {
		var groupID, userID string
		if scanErr := mRows.Scan(&groupID, &userID); scanErr != nil {
			return nil, fmt.Errorf("scimsync: scan member: %w", scanErr)
		}
		byGroup[groupID] = append(byGroup[groupID], userID)
	}
	for i := range out {
		out[i].Members = byGroup[out[i].ID]
	}
	return out, mRows.Err()
}

func scimUserRows(ctx context.Context, db datastore.Executor, sb *sqlbuilder.SelectBuilder) ([]ScimUserRow, error) {
	query, args := sb.Build()
	rows, err := db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("scimsync: snapshot users: %w", err)
	}
	defer rows.Close()

	var out []ScimUserRow
	for rows.Next() {
		var row ScimUserRow
		var disabled bool
		if err := rows.Scan(&row.ID, &row.Username, &row.DisplayName, &row.FirstName, &row.LastName, &row.Email, &disabled); err != nil {
			return nil, fmt.Errorf("scimsync: scan user: %w", err)
		}
		row.Active = !disabled
		out = append(out, row)
	}
	return out, rows.Err()
}

func toAny[T any](in []T) []any {
	out := make([]any, len(in))
	for i, v := range in {
		out[i] = v
	}
	return out
}
