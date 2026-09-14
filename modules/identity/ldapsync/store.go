package ldapsync

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/huandu/go-sqlbuilder"
	"github.com/riipandi/tango/internal/datastore"
)

const (
	usersTable   = "public.users"
	groupsTable  = "public.user_groups"
	membersTable = "public.user_groups_users"
)

// Reconciler applies the desired LDAP state to identity stores inside
// one transaction. It works on UUID strings (the stores' typed IDs are
// parsed at the boundary) to keep this module independent of the
// user/usergroup service layer.
type Reconciler struct {
	exec datastore.Executor
}

// NewReconciler builds the reconciler over an executor (Store or a tx).
func NewReconciler(exec datastore.Executor) *Reconciler {
	return &Reconciler{exec: exec}
}

// desiredUser is one LDAP user mapped onto tango columns.
type desiredUser struct {
	LDAPID      string
	Username    string
	Email       string
	FirstName   string
	LastName    string
	DisplayName string
	IsAdmin     bool
}

// desiredGroup is one LDAP group with member usernames resolved.
type desiredGroup struct {
	LDAPID  string
	Name    string
	Members []string // usernames
}

// managedUser is an existing LDAP-managed row.
type managedUser struct {
	ID       string
	Username string
	LDAPID   string
	Disabled bool
}

// managedGroup is an existing LDAP-managed group row.
type managedGroup struct {
	ID     string
	Name   string
	LDAPID string
}

// loadManagedUsers lists users with ldap_id set.
func (r *Reconciler) loadManagedUsers(ctx context.Context) ([]managedUser, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select("id", "username", "ldap_id", "disabled")
	sb.From(usersTable)
	sb.Where(sb.IsNotNull("ldap_id"))
	query, args := sb.Build()
	rows, err := r.exec.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("ldapsync: load managed users: %w", err)
	}
	defer rows.Close()

	var users []managedUser
	for rows.Next() {
		var u managedUser
		var disabled bool
		if err := rows.Scan(&u.ID, &u.Username, &u.LDAPID, &disabled); err != nil {
			return nil, fmt.Errorf("ldapsync: scan managed user: %w", err)
		}
		u.Disabled = disabled
		users = append(users, u)
	}
	return users, rows.Err()
}

// loadManagedGroups lists groups with ldap_id set.
func (r *Reconciler) loadManagedGroups(ctx context.Context) ([]managedGroup, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select("id", "name", "ldap_id")
	sb.From(groupsTable)
	sb.Where(sb.IsNotNull("ldap_id"))
	query, args := sb.Build()
	rows, err := r.exec.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("ldapsync: load managed groups: %w", err)
	}
	defer rows.Close()

	var groups []managedGroup
	for rows.Next() {
		var g managedGroup
		if err := rows.Scan(&g.ID, &g.Name, &g.LDAPID); err != nil {
			return nil, fmt.Errorf("ldapsync: scan managed group: %w", err)
		}
		groups = append(groups, g)
	}
	return groups, rows.Err()
}

// upsertUser creates or updates one LDAP-managed user; returns the
// row UUID. Username conflicts with a non-LDAP row surface as
// user.ErrDuplicateUsername via the unique index.
func (r *Reconciler) upsertUser(ctx context.Context, u desiredUser) (string, error) {
	display := strings.TrimSpace(u.DisplayName)
	if display == "" {
		display = strings.TrimSpace(strings.Join([]string{u.FirstName, u.LastName}, " "))
	}
	if display == "" {
		display = u.Username
	}

	// Update first (most syncs are updates); fall back to insert.
	ub := sqlbuilder.PostgreSQL.NewUpdateBuilder()
	ub.Update(usersTable)
	ub.Set(
		ub.Assign("email", u.Email),
		ub.Assign("first_name", nullIfEmpty(u.FirstName)),
		ub.Assign("last_name", nullIfEmpty(u.LastName)),
		ub.Assign("display_name", display),
		ub.Assign("is_admin", u.IsAdmin),
		ub.Assign("disabled", false), // back in LDAP → re-enabled
	)
	ub.Where(ub.E("ldap_id", u.LDAPID))
	query, args := ub.Build()
	tag, err := r.exec.Exec(ctx, query, args...)
	if err != nil {
		return "", fmt.Errorf("ldapsync: update user %q: %w", u.Username, err)
	}
	if tag.RowsAffected() > 0 {
		return r.ldapUserID(ctx, u.LDAPID)
	}

	ib := sqlbuilder.PostgreSQL.NewInsertBuilder()
	ib.InsertInto(usersTable)
	ib.Cols("username", "email", "first_name", "last_name", "display_name", "is_admin", "ldap_id", "email_verified_at")
	ib.Values(u.Username, u.Email, nullIfEmpty(u.FirstName), nullIfEmpty(u.LastName), display, u.IsAdmin, u.LDAPID, sqlbuilder.Raw("now()"))
	query, args = ib.Build()
	if _, err := r.exec.Exec(ctx, query, args...); err != nil {
		return "", fmt.Errorf("ldapsync: create user %q: %w", u.Username, err)
	}
	return r.ldapUserID(ctx, u.LDAPID)
}

func (r *Reconciler) ldapUserID(ctx context.Context, ldapID string) (string, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select("id")
	sb.From(usersTable)
	sb.Where(sb.E("ldap_id", ldapID))
	query, args := sb.Build()
	var id string
	if err := r.exec.QueryRow(ctx, query, args...).Scan(&id); err != nil {
		return "", fmt.Errorf("ldapsync: resolve user id: %w", err)
	}
	return id, nil
}

// disableUser flips disabled on without touching LDAP ownership.
func (r *Reconciler) disableUser(ctx context.Context, id string) error {
	ub := sqlbuilder.PostgreSQL.NewUpdateBuilder()
	ub.Update(usersTable)
	ub.Set(ub.Assign("disabled", sqlbuilder.Raw("$2::boolean")))
	ub.Where(ub.E("id", sqlbuilder.Raw("$1::uuid")))
	query, _ := ub.Build()
	if _, err := r.exec.Exec(ctx, query, id, true); err != nil {
		return fmt.Errorf("ldapsync: disable user: %w", err)
	}
	return nil
}

// deleteUser removes an LDAP-managed user row (cascades sessions etc.).
func (r *Reconciler) deleteUser(ctx context.Context, id string) error {
	db := sqlbuilder.PostgreSQL.NewDeleteBuilder()
	db.DeleteFrom(usersTable)
	db.Where(db.E("id", sqlbuilder.Raw("$1::uuid")))
	query, _ := db.Build()
	if _, err := r.exec.Exec(ctx, query, id); err != nil {
		return fmt.Errorf("ldapsync: delete user: %w", err)
	}
	return nil
}

// upsertGroup creates or updates one LDAP-managed group and list-
// replaces its membership; returns the row UUID.
func (r *Reconciler) upsertGroup(ctx context.Context, g desiredGroup, memberUserIDs []string) (string, error) {
	// Update first, then insert on miss.
	ub := sqlbuilder.PostgreSQL.NewUpdateBuilder()
	ub.Update(groupsTable)
	ub.Set(ub.Assign("display_name", g.Name))
	ub.Where(ub.E("ldap_id", g.LDAPID))
	query, args := ub.Build()
	tag, err := r.exec.Exec(ctx, query, args...)
	if err != nil {
		return "", fmt.Errorf("ldapsync: update group %q: %w", g.Name, err)
	}
	if tag.RowsAffected() == 0 {
		ib := sqlbuilder.PostgreSQL.NewInsertBuilder()
		ib.InsertInto(groupsTable)
		ib.Cols("name", "display_name", "ldap_id")
		ib.Values(g.Name, g.Name, g.LDAPID)
		iQuery, iArgs := ib.Build()
		if _, insertErr := r.exec.Exec(ctx, iQuery, iArgs...); insertErr != nil {
			return "", fmt.Errorf("ldapsync: create group %q: %w", g.Name, insertErr)
		}
	}

	groupID, err := r.ldapGroupID(ctx, g.LDAPID)
	if err != nil {
		return "", err
	}

	// List-replace membership.
	db := sqlbuilder.PostgreSQL.NewDeleteBuilder()
	db.DeleteFrom(membersTable)
	db.Where(db.E("user_group_id", sqlbuilder.Raw("$1::uuid")))
	query, _ = db.Build()
	if _, err := r.exec.Exec(ctx, query, groupID); err != nil {
		return "", fmt.Errorf("ldapsync: clear group members: %w", err)
	}
	for _, uid := range memberUserIDs {
		ib := sqlbuilder.PostgreSQL.NewInsertBuilder()
		ib.InsertInto(membersTable)
		ib.Cols("user_id", "user_group_id")
		ib.Values(sqlbuilder.Raw("$1::uuid"), sqlbuilder.Raw("$2::uuid"))
		query, _ = ib.Build()
		if _, err := r.exec.Exec(ctx, query, uid, groupID); err != nil {
			return "", fmt.Errorf("ldapsync: add member: %w", err)
		}
	}
	return groupID, nil
}

func (r *Reconciler) ldapGroupID(ctx context.Context, ldapID string) (string, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select("id")
	sb.From(groupsTable)
	sb.Where(sb.E("ldap_id", ldapID))
	query, args := sb.Build()
	var id string
	if err := r.exec.QueryRow(ctx, query, args...).Scan(&id); err != nil {
		return "", fmt.Errorf("ldapsync: resolve group id: %w", err)
	}
	return id, nil
}

// deleteGroup removes an LDAP-managed group row.
func (r *Reconciler) deleteGroup(ctx context.Context, id string) error {
	db := sqlbuilder.PostgreSQL.NewDeleteBuilder()
	db.DeleteFrom(groupsTable)
	db.Where(db.E("id", sqlbuilder.Raw("$1::uuid")))
	query, _ := db.Build()
	if _, err := r.exec.Exec(ctx, query, id); err != nil {
		return fmt.Errorf("ldapsync: delete group: %w", err)
	}
	return nil
}

// Run executes the whole reconciliation in one transaction: users
// first, then groups (members reference fresh user rows).
func (r *Reconciler) Run(ctx context.Context, users []desiredUser, groups []desiredGroup, softDelete bool) (SyncStats, error) {
	var stats SyncStats
	store, ok := r.exec.(datastore.Store)
	if !ok {
		return stats, errors.New("ldapsync: reconciler needs a transactional store")
	}
	err := store.WithTx(ctx, func(tx datastore.Executor) error {
		txr := &Reconciler{exec: tx}
		var runErr error
		stats, runErr = txr.run(ctx, users, groups, softDelete)
		return runErr
	})
	return stats, err
}

func (r *Reconciler) run(ctx context.Context, users []desiredUser, groups []desiredGroup, softDelete bool) (SyncStats, error) {
	var stats SyncStats

	managed, err := r.loadManagedUsers(ctx)
	if err != nil {
		return stats, err
	}
	byLDAPID := make(map[string]managedUser, len(managed))
	byUsername := make(map[string]managedUser, len(managed))
	seenLDAP := make(map[string]struct{}, len(users))
	for _, u := range managed {
		byLDAPID[u.LDAPID] = u
		byUsername[u.Username] = u
	}

	for _, du := range users {
		existing, ok := byLDAPID[du.LDAPID]
		if !ok {
			if _, createErr := r.upsertUser(ctx, du); createErr != nil {
				if strings.Contains(createErr.Error(), "duplicate key") {
					continue // username collision with a non-LDAP user
				}
				return stats, createErr
			}
			stats.UsersCreated++
			continue
		}
		seenLDAP[du.LDAPID] = struct{}{}
		if _, updateErr := r.upsertUser(ctx, du); updateErr != nil {
			return stats, updateErr
		}
		if existing.Disabled {
			stats.UsersUpdated++
		}
	}

	// Remove users no longer in LDAP.
	for _, mu := range managed {
		if _, ok := seenLDAP[mu.LDAPID]; ok {
			continue
		}
		if softDelete {
			if disableErr := r.disableUser(ctx, mu.ID); disableErr != nil {
				return stats, disableErr
			}
			stats.UsersDisabled++
			continue
		}
		if deleteErr := r.deleteUser(ctx, mu.ID); deleteErr != nil {
			return stats, deleteErr
		}
		stats.UsersDeleted++
	}

	// Groups: resolve member usernames to fresh user IDs.
	managedGroups, err := r.loadManagedGroups(ctx)
	if err != nil {
		return stats, err
	}
	groupByLDAP := make(map[string]managedGroup, len(managedGroups))
	seenGroups := make(map[string]struct{}, len(groups))
	for _, g := range managedGroups {
		groupByLDAP[g.LDAPID] = g
	}

	// Reload username→ID map after user reconciliation.
	fresh, err := r.loadManagedUsers(ctx)
	if err != nil {
		return stats, err
	}
	userIDs := make(map[string]string, len(fresh))
	for _, u := range fresh {
		userIDs[u.Username] = u.ID
	}

	for _, dg := range groups {
		seenGroups[dg.LDAPID] = struct{}{}
		memberIDs := make([]string, 0, len(dg.Members))
		for _, name := range dg.Members {
			if id, ok := userIDs[name]; ok {
				memberIDs = append(memberIDs, id)
			}
		}
		if _, err := r.upsertGroup(ctx, dg, memberIDs); err != nil {
			return stats, err
		}
		if _, existed := groupByLDAP[dg.LDAPID]; existed {
			stats.GroupsUpdated++
		} else {
			stats.GroupsCreated++
		}
	}

	for _, mg := range managedGroups {
		if _, ok := seenGroups[mg.LDAPID]; ok {
			continue
		}
		if err := r.deleteGroup(ctx, mg.ID); err != nil {
			return stats, err
		}
		stats.GroupsDeleted++
	}

	return stats, nil
}

func nullIfEmpty(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

// SyncStats summarizes one reconciliation pass.
type SyncStats struct {
	UsersCreated  int `json:"users_created"`
	UsersUpdated  int `json:"users_updated"`
	UsersDisabled int `json:"users_disabled"`
	UsersDeleted  int `json:"users_deleted"`
	GroupsCreated int `json:"groups_created"`
	GroupsUpdated int `json:"groups_updated"`
	GroupsDeleted int `json:"groups_deleted"`
}
