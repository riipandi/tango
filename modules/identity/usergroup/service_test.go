package usergroup

import (
	"log/slog"
	"testing"

	"github.com/huandu/go-sqlbuilder"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/internal/audit"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/pkg/testutils"
)

// migratedPool opens a database the migrations have built, so the group
// tables exist.
func migratedPool(t *testing.T) *datastore.Postgres {
	t.Helper()

	dsn := testutils.StartPostgres(t.Context(), t).NewDatabase(t)

	migrationDB, err := datastore.OpenMigrationDB(t.Context(), datastore.PostgresOptions{DSN: dsn})
	require.NoError(t, err)
	migrator, err := database.NewMigrator(t.Context(), migrationDB, database.MigratorOptions{})
	require.NoError(t, err)
	_, err = migrator.Up(t.Context())
	require.NoError(t, err)
	require.NoError(t, migrationDB.Close())

	pool, err := datastore.NewPostgres(t.Context(), datastore.PostgresOptions{
		DSN:             dsn,
		ApplicationName: "usergroup_test",
	})
	require.NoError(t, err)
	t.Cleanup(func() { pool.Shutdown(t.Context()) })
	return pool
}

// testService builds the service with a recorder that writes for real — a
// record is part of the transaction it describes, so the assertions read the
// table the writer fills.
func testService(t *testing.T, pool *datastore.Postgres) *Service {
	t.Helper()
	return NewService(pool, audit.NewRecorder(slog.New(slog.DiscardHandler)), slog.New(slog.DiscardHandler))
}

// seedAccount inserts an account directly, the way a fixture does, and
// answers its identifier. A member must exist before it can be named.
func seedAccount(t *testing.T, pool *datastore.Postgres, username string) string {
	t.Helper()

	_, err := pool.Exec(t.Context(), `
		INSERT INTO public.users (username, email, first_name, last_name, display_name)
		VALUES ($1::citext, $1::text || '@example.com', 'Hermione', 'Granger', 'Hermione Granger')`,
		username)
	require.NoError(t, err)

	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select("id")
	sb.From("public.users")
	sb.Where(sb.Equal("username", username))

	query, args := sb.Build()
	var id string
	require.NoError(t, pool.QueryRow(t.Context(), query, args...).Scan(&id))
	return id
}

// membershipCount reads how many accounts belong to a group, straight from
// the junction table the foreign keys guard.
func membershipCount(t *testing.T, pool *datastore.Postgres, groupID string) int {
	t.Helper()

	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select("count(*)")
	sb.From(GroupMemberTable)
	sb.Where(sb.Equal("user_group_id", groupID))

	query, args := sb.Build()
	var count int
	require.NoError(t, pool.QueryRow(t.Context(), query, args...).Scan(&count))
	return count
}

func TestCreateGroupStoresTheRowAndRefusesADuplicateName(t *testing.T) {
	testutils.SkipWithoutDocker(t)

	pool := migratedPool(t)
	service := testService(t, pool)

	created, err := service.CreateUserGroup(t.Context(), CreateParams{
		Name:        "gryffindor",
		DisplayName: "Gryffindor",
	})
	require.NoError(t, err)
	assert.Equal(t, "gryffindor", created.Name)
	assert.Equal(t, "Gryffindor", created.DisplayName)
	assert.Equal(t, 0, created.UserCount, "a new group holds no member")
	assert.NotEmpty(t, created.ID)
	assert.Nil(t, created.UpdatedAt, "a row never updated carries no update instant")

	// The name is unique by index, and the failure is the duplicate the
	// caller is told about, not an internal error.
	_, err = service.CreateUserGroup(t.Context(), CreateParams{
		Name:        "gryffindor",
		DisplayName: "Another Gryffindor",
	})
	assert.ErrorIs(t, err, ErrGroupExists)

	// The record of the creation is in the log, and it names the group.
	assert.Equal(t, 1, auditCount(t, pool, audit.EventGroupCreated, created.ID.String()))
}

func TestListGroupsSearchesPaginatesAndSorts(t *testing.T) {
	testutils.SkipWithoutDocker(t)

	pool := migratedPool(t)
	service := testService(t, pool)

	gryffindor := created(t, service, "gryffindor", "Gryffindor")
	slytherin := created(t, service, "slytherin", "Slytherin")
	created(t, service, "ravenclaw", "Ravenclaw")

	// The member counts travel with the rows: one group holds two accounts,
	// another one, the last none.
	hermione := seedAccount(t, pool, "hermione")
	ron := seedAccount(t, pool, "ron")
	_, err := service.SetUserGroupMembers(t.Context(), gryffindor.ID.String(), []string{hermione, ron})
	require.NoError(t, err)
	_, err = service.SetUserGroupMembers(t.Context(), slytherin.ID.String(), []string{hermione})
	require.NoError(t, err)

	// The default order is the display name, ascending.
	page, pagination, err := service.ListGroups(t.Context(), "", "", true, 1, 10)
	require.NoError(t, err)
	require.Len(t, page, 3)
	assert.Equal(t, "Gryffindor", page[0].DisplayName)
	assert.Equal(t, "Ravenclaw", page[1].DisplayName)
	assert.Equal(t, "Slytherin", page[2].DisplayName)
	require.NotNil(t, pagination.TotalItems)
	assert.Equal(t, 3, *pagination.TotalItems)

	// The count rides the row: two, one, zero in the page above.
	assert.Equal(t, 2, page[0].UserCount)
	assert.Equal(t, 0, page[1].UserCount)
	assert.Equal(t, 1, page[2].UserCount)

	// The member count is sortable, descending first.
	page, _, err = service.ListGroups(t.Context(), "", "user_count", false, 1, 10)
	require.NoError(t, err)
	require.Len(t, page, 3)
	assert.Equal(t, 2, page[0].UserCount)

	// The name is sortable, and case-insensitively: an upper-case display
	// name does not push its row past every lower-case one.
	page, _, err = service.ListGroups(t.Context(), "", "name", true, 1, 10)
	require.NoError(t, err)
	require.Len(t, page, 3)
	assert.Equal(t, "gryffindor", page[0].Name)

	// The search matches both name columns and filters the page.
	page, pagination, err = service.ListGroups(t.Context(), "slyth", "", true, 1, 10)
	require.NoError(t, err)
	require.Len(t, page, 1)
	assert.Equal(t, slytherin.ID.String(), page[0].ID.String())
	assert.Equal(t, 1, *pagination.TotalItems)
}

func TestUpdateGroupReplacesTheFieldsAndRefusesADuplicate(t *testing.T) {
	testutils.SkipWithoutDocker(t)

	pool := migratedPool(t)
	service := testService(t, pool)

	first := created(t, service, "gryffindor", "Gryffindor")
	second := created(t, service, "slytherin", "Slytherin")

	updated, err := service.UpdateUserGroup(t.Context(), first.ID.String(), CreateParams{
		Name:        "gryffindor-house",
		DisplayName: "Gryffindor House",
	})
	require.NoError(t, err)
	assert.Equal(t, "gryffindor-house", updated.Name)
	assert.Equal(t, "Gryffindor House", updated.DisplayName)
	assert.NotNil(t, updated.UpdatedAt, "an updated row carries its update instant")

	// A name another group holds is refused, and the refused write leaves
	// the other group intact.
	_, err = service.UpdateUserGroup(t.Context(), first.ID.String(), CreateParams{
		Name:        "slytherin",
		DisplayName: "Not Slytherin",
	})
	assert.ErrorIs(t, err, ErrGroupExists)
	intact, err := service.GetGroup(t.Context(), second.ID.String())
	require.NoError(t, err)
	assert.Equal(t, "Slytherin", intact.DisplayName)

	// An unknown identifier is the not-found failure.
	_, err = service.UpdateUserGroup(t.Context(), "01a0da3e-1111-7000-8000-000000000009", CreateParams{
		Name:        "hufflepuff",
		DisplayName: "Hufflepuff",
	})
	assert.ErrorIs(t, err, ErrGroupNotFound)
}

func TestDeleteGroupRemovesTheMemberships(t *testing.T) {
	testutils.SkipWithoutDocker(t)

	pool := migratedPool(t)
	service := testService(t, pool)

	group := created(t, service, "gryffindor", "Gryffindor")
	hermione := seedAccount(t, pool, "hermione")
	_, err := service.SetUserGroupMembers(t.Context(), group.ID.String(), []string{hermione})
	require.NoError(t, err)

	require.NoError(t, service.DeleteUserGroup(t.Context(), group.ID.String()))

	// The junction rows died with the group by the cascade, and the account
	// itself is untouched.
	assert.Equal(t, 0, membershipCount(t, pool, group.ID.String()))
	assert.Equal(t, 1, auditCount(t, pool, audit.EventGroupDeleted, group.ID.String()))

	_, err = service.GetGroup(t.Context(), group.ID.String())
	assert.ErrorIs(t, err, ErrGroupNotFound)

	// A second deletion names nothing, which is the not-found failure.
	assert.ErrorIs(t, service.DeleteUserGroup(t.Context(), group.ID.String()), ErrGroupNotFound)
}

func TestSetMembersReplacesTheWholeSet(t *testing.T) {
	testutils.SkipWithoutDocker(t)

	pool := migratedPool(t)
	service := testService(t, pool)

	group := created(t, service, "gryffindor", "Gryffindor")
	hermione := seedAccount(t, pool, "hermione")
	ron := seedAccount(t, pool, "ron")
	neville := seedAccount(t, pool, "neville")

	// Two members in, one different member in: the replacement is the whole
	// set, not a delta, so the answer is one member and the junction holds
	// one row.
	_, err := service.SetUserGroupMembers(t.Context(), group.ID.String(), []string{hermione, ron})
	require.NoError(t, err)
	updated, err := service.SetUserGroupMembers(t.Context(), group.ID.String(), []string{neville})
	require.NoError(t, err)
	assert.Equal(t, 1, updated.UserCount)
	require.Len(t, updated.Members, 1)
	assert.Equal(t, "neville", updated.Members[0].Username)
	assert.Equal(t, 1, membershipCount(t, pool, group.ID.String()))

	// A member that does not exist refuses the replacement whole: the set
	// the group holds is the one it held before.
	_, err = service.SetUserGroupMembers(t.Context(), group.ID.String(), []string{hermione, "01a0da3e-1111-7000-8000-000000000009"})
	assert.ErrorIs(t, err, ErrMemberNotFound)
	unchanged, err := service.GetGroup(t.Context(), group.ID.String())
	require.NoError(t, err)
	assert.Equal(t, 1, unchanged.UserCount)

	// The empty list empties the group — a group may be emptied.
	_, err = service.SetUserGroupMembers(t.Context(), group.ID.String(), nil)
	require.NoError(t, err)
	assert.Equal(t, 0, membershipCount(t, pool, group.ID.String()))
	assert.Equal(t, 3, auditCount(t, pool, audit.EventGroupMembersUpdated, group.ID.String()))
}

// created is a fixture group with a fixed display name, for the tests that
// need more than one.
func created(t *testing.T, service *Service, name, displayName string) GroupDetailView {
	t.Helper()

	group, err := service.CreateUserGroup(t.Context(), CreateParams{
		Name:        name,
		DisplayName: displayName,
	})
	require.NoError(t, err)
	return group
}

// auditCount reads how many records the log holds of one event about one
// group.
func auditCount(t *testing.T, pool *datastore.Postgres, event, groupID string) int {
	t.Helper()

	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select("count(*)")
	sb.From("public.audit_logs")
	sb.Where(sb.Equal("event", event), sb.Equal("resource_id", groupID))

	query, args := sb.Build()
	var count int
	require.NoError(t, pool.QueryRow(t.Context(), query, args...).Scan(&count))
	return count
}
