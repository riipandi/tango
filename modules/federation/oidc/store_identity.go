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

// UserProfile is the claim source for tokens.
type UserProfile struct {
	ID              string
	Username        string
	Email           string
	DisplayName     string
	EmailVerifiedAt *time.Time
	Disabled        bool
}

// UserInGroup checks membership (restricted-client allowlist).
func (s *PostgresStore) UserInGroup(ctx context.Context, userID, groupID string) bool {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select("count(*)")
	sb.From(userGroupsUsersTable)
	sb.Where(sb.And(sb.E("user_id", datastore.UserUUID(userID)), sb.E("user_group_id", groupID)))

	query, args := sb.Build()
	var count int
	if err := s.exec.QueryRow(ctx, query, args...).Scan(&count); err != nil {
		return false
	}
	return count > 0
}

// UserByID reads the token-claim source user row.
func (s *PostgresStore) UserByID(ctx context.Context, userID string) (UserProfile, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select("id", "username", "email", "display_name", "email_verified_at", "disabled")
	sb.From(usersTable)
	sb.Where(sb.E("id", datastore.UserUUID(userID)))

	query, args := sb.Build()
	var (
		profile    UserProfile
		verifiedAt pgtype.Timestamptz
		disabled   bool
	)
	err := s.exec.QueryRow(ctx, query, args...).Scan(
		&profile.ID, &profile.Username, &profile.Email, &profile.DisplayName, &verifiedAt, &disabled,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return profile, ErrInvalidGrant
		}
		return profile, fmt.Errorf("oidc store: user by id: %w", err)
	}
	if verifiedAt.Valid {
		profile.EmailVerifiedAt = &verifiedAt.Time
	}
	profile.Disabled = disabled
	return profile, nil
}

// UserGroups lists group names for the groups claim.
func (s *PostgresStore) UserGroups(ctx context.Context, userID string) ([]string, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select("g.name")
	sb.From(userGroupsTable + " g")
	sb.Join(userGroupsUsersTable + " m ON m.user_group_id = g.id")
	sb.Where(sb.E("m.user_id", datastore.UserUUID(userID)))
	sb.OrderBy("g.name")

	query, args := sb.Build()
	rows, err := s.exec.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("oidc store: user groups: %w", err)
	}
	defer rows.Close()

	groups := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		groups = append(groups, name)
	}
	return groups, rows.Err()
}

// CustomClaims merges user-scoped and group-scoped claims.
func (s *PostgresStore) CustomClaims(ctx context.Context, userID string) (map[string]string, error) {
	groupIDs, err := s.userGroupIDs(ctx, userID)
	if err != nil {
		return nil, err
	}

	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select("key", "value")
	sb.From(customClaimsTable)

	condition := sb.E("user_id", datastore.UserUUID(userID))
	if len(groupIDs) > 0 {
		condition = sb.Or(condition, sb.In("user_group_id", sqlbuilder.List(groupIDs)))
	}
	sb.Where(condition)

	query, args := sb.Build()
	rows, err := s.exec.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("oidc store: custom claims: %w", err)
	}
	defer rows.Close()

	claims := map[string]string{}
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		claims[key] = value
	}
	return claims, rows.Err()
}

// userGroupIDs collects the user's group UUIDs for claim queries.
func (s *PostgresStore) userGroupIDs(ctx context.Context, userID string) ([]string, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select("user_group_id")
	sb.From(userGroupsUsersTable)
	sb.Where(sb.E("user_id", datastore.UserUUID(userID)))

	query, args := sb.Build()
	rows, err := s.exec.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("oidc store: user group ids: %w", err)
	}
	defer rows.Close()

	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
