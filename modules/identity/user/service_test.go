package user

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/huandu/go-sqlbuilder"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"connectrpc.com/authn"
	"connectrpc.com/connect"

	identityv1 "github.com/riipandi/tango/codegen/proto/go/tango/identity/v1"
	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/pkg/jwtutils"
	"github.com/riipandi/tango/pkg/testutils"

	"uuid"
)

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
		ApplicationName: "user_test",
	})
	require.NoError(t, err)
	t.Cleanup(func() { pool.Shutdown(t.Context()) })
	return pool
}

func testService(t *testing.T, pool *datastore.Postgres) *Service {
	t.Helper()
	return NewService(pool, nil)
}

// passwordCount reads how many credentials an account carries. An account
// created without a password carries none.
func passwordCount(t *testing.T, pool *datastore.Postgres, userID string) int {
	t.Helper()

	id, err := uuid.Parse(userID)
	require.NoError(t, err)

	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select("count(*)")
	sb.From("public.user_passwords")
	sb.Where(sb.Equal("user_id", id))

	query, args := sb.Build()
	var count int
	require.NoError(t, pool.QueryRow(t.Context(), query, args...).Scan(&count))
	return count
}

func TestCreateUserStoresTheAccountAndTheCredential(t *testing.T) {
	testutils.SkipWithoutDocker(t)

	pool := migratedPool(t)
	service := testService(t, pool)

	created, err := service.CreateUser(t.Context(), CreateParams{
		Username:      "ada",
		Email:         "ada@example.com",
		Password:      "correct horse",
		FirstName:     "Ada",
		LastName:      "Lovelace",
		IsAdmin:       true,
		EmailVerified: true,
	})
	require.NoError(t, err)

	assert.NotEmpty(t, created.ID)
	assert.Equal(t, "ada", created.Username)
	assert.Equal(t, "ada@example.com", created.Email)
	assert.Equal(t, "Ada Lovelace", created.DisplayName)
	assert.True(t, created.IsAdmin)
	assert.True(t, created.EmailVerified)
	assert.False(t, created.CreatedAt.IsZero())
	assert.Equal(t, 1, passwordCount(t, pool, created.ID))

	read, err := service.GetUser(t.Context(), created.ID)
	require.NoError(t, err)
	assert.Equal(t, "Ada Lovelace", read.DisplayName)
	require.NotNil(t, read.FirstName)
	assert.Equal(t, "Ada", *read.FirstName)
}

func TestCreateUserWithoutAPasswordCarriesNoCredential(t *testing.T) {
	testutils.SkipWithoutDocker(t)

	pool := migratedPool(t)
	service := testService(t, pool)

	created, err := service.CreateUser(t.Context(), CreateParams{FirstName: "Grace", LastName: "Hopper",
		Username: "grace",
		Email:    "grace@example.com",
	})
	require.NoError(t, err)

	// The display name is composed from the mandatory names, and no
	// credential row exists to sign in with.
	assert.Equal(t, "Grace Hopper", created.DisplayName)
	assert.False(t, created.EmailVerified)
	assert.Equal(t, 0, passwordCount(t, pool, created.ID))
}

func TestCreateUserRefusesADuplicateAccount(t *testing.T) {
	testutils.SkipWithoutDocker(t)

	pool := migratedPool(t)
	service := testService(t, pool)

	_, err := service.CreateUser(t.Context(), CreateParams{FirstName: "Ada", LastName: "Lovelace", Username: "ada", Email: "ada@example.com"})
	require.NoError(t, err)

	// The username matches case-insensitively, the way its unique index does.
	_, err = service.CreateUser(t.Context(), CreateParams{FirstName: "Ada", LastName: "Lovelace", Username: "ADA", Email: "other@example.com"})
	assert.ErrorIs(t, err, ErrAccountExists)

	_, err = service.CreateUser(t.Context(), CreateParams{FirstName: "Ada", LastName: "Lovelace", Username: "other", Email: "ada@example.com"})
	assert.ErrorIs(t, err, ErrAccountExists)
}

func TestGetUserRefusesAnUnknownIdentifier(t *testing.T) {
	testutils.SkipWithoutDocker(t)

	pool := migratedPool(t)
	service := testService(t, pool)

	_, err := service.GetUser(t.Context(), uuid.NewV7().String())
	assert.ErrorIs(t, err, ErrUserNotFound)

	_, err = service.GetUser(t.Context(), "not-a-uuid")
	assert.ErrorIs(t, err, ErrUserNotFound)
}

func TestListUsersSearchesAndPaginates(t *testing.T) {
	testutils.SkipWithoutDocker(t)

	pool := migratedPool(t)
	service := testService(t, pool)

	for _, name := range []string{"ada", "grace", "alan", "marie"} {
		_, err := service.CreateUser(t.Context(), CreateParams{FirstName: "Ada", LastName: "Lovelace", Username: name, Email: name + "@example.com"})
		require.NoError(t, err)
	}

	users, pagination, err := service.ListUsers(t.Context(), "", 1, 2)
	require.NoError(t, err)
	assert.Len(t, users, 2)
	require.NotNil(t, pagination.TotalItems)
	assert.Equal(t, 4, *pagination.TotalItems)

	// The search matches the username, case-insensitively, and answers only
	// the accounts it names.
	users, _, err = service.ListUsers(t.Context(), "GRA", 1, 10)
	require.NoError(t, err)
	require.Len(t, users, 1)
	assert.Equal(t, "grace", users[0].Username)
}

func TestUpdateUserReplacesTheFields(t *testing.T) {
	testutils.SkipWithoutDocker(t)

	pool := migratedPool(t)
	service := testService(t, pool)

	created, err := service.CreateUser(t.Context(), CreateParams{
		Username:  "ada",
		Email:     "ada@example.com",
		FirstName: "Ada",
	})
	require.NoError(t, err)

	// The full replace is the whole shape: every field is specified, and the
	// names are mandatory, so a name part left empty is a contract refusal
	// the transport answers before the service runs.
	updated, err := service.UpdateUser(t.Context(), created.ID, UpdateParams{
		Username:    "ada",
		Email:       "countess@example.com",
		FirstName:   "Ada",
		LastName:    "King",
		DisplayName: "The Countess",
		Locale:      "en-GB",
		Disabled:    true,
	})
	require.NoError(t, err)
	assert.Equal(t, "The Countess", updated.DisplayName)
	assert.Equal(t, "countess@example.com", updated.Email)
	assert.True(t, updated.Disabled)
	require.NotNil(t, updated.FirstName)
	assert.Equal(t, "Ada", *updated.FirstName)
	require.NotNil(t, updated.LastName)
	assert.Equal(t, "King", *updated.LastName)
	require.NotNil(t, updated.Locale)
	assert.Equal(t, "en-GB", *updated.Locale)

	read, err := service.GetUser(t.Context(), created.ID)
	require.NoError(t, err)
	assert.Equal(t, "King", *read.LastName)
}

func TestUpdateUserAppliesAndLiftsTheBan(t *testing.T) {
	testutils.SkipWithoutDocker(t)

	pool := migratedPool(t)
	service := testService(t, pool)
	service.now = func() time.Time { return time.Unix(2000000000, 0) }

	created, err := service.CreateUser(t.Context(), CreateParams{FirstName: "Ada", LastName: "Lovelace", Username: "ada", Email: "ada@example.com"})
	require.NoError(t, err)

	expires := service.now().Add(24 * time.Hour)
	banned, err := service.UpdateUser(t.Context(), created.ID, UpdateParams{
		Username:     "ada",
		Email:        "ada@example.com",
		FirstName:    "Ada",
		LastName:     "Lovelace",
		DisplayName:  "ada",
		BanExpiresAt: &expires,
		BanReason:    strPtr("unruly behaviour"),
	})
	require.NoError(t, err)
	require.NotNil(t, banned.BannedAt)
	assert.Equal(t, service.now(), *banned.BannedAt)
	require.NotNil(t, banned.BanExpires)
	require.NotNil(t, banned.BanReason)

	// A re-ban keeps the start instant on record: the field answers "since
	// when", so applying a new expiry does not move it.
	later := expires.Add(24 * time.Hour)
	rebanned, err := service.UpdateUser(t.Context(), created.ID, UpdateParams{
		Username:     "ada",
		Email:        "ada@example.com",
		FirstName:    "Ada",
		LastName:     "Lovelace",
		DisplayName:  "ada",
		BanExpiresAt: &later,
	})
	require.NoError(t, err)
	assert.Equal(t, service.now(), *rebanned.BannedAt)

	lifted, err := service.UpdateUser(t.Context(), created.ID, UpdateParams{
		Username:    "ada",
		Email:       "ada@example.com",
		DisplayName: "ada",
	})
	require.NoError(t, err)
	assert.Nil(t, lifted.BannedAt)
	assert.Nil(t, lifted.BanExpires)
	assert.Nil(t, lifted.BanReason)
}

func TestUpdateUserRefusesAnUnknownIdentifierAndADuplicate(t *testing.T) {
	testutils.SkipWithoutDocker(t)

	pool := migratedPool(t)
	service := testService(t, pool)

	_, err := service.CreateUser(t.Context(), CreateParams{FirstName: "Ada", LastName: "Lovelace", Username: "ada", Email: "ada@example.com"})
	require.NoError(t, err)

	_, err = service.UpdateUser(t.Context(), uuid.NewV7().String(), UpdateParams{
		Username: "ada", Email: "x@example.com", DisplayName: "x", FirstName: "Ada", LastName: "Lovelace",
	})
	assert.ErrorIs(t, err, ErrUserNotFound)

	other, err := service.CreateUser(t.Context(), CreateParams{FirstName: "Ada", LastName: "Lovelace", Username: "grace", Email: "grace@example.com"})
	require.NoError(t, err)

	_, err = service.UpdateUser(t.Context(), other.ID, UpdateParams{
		Username: "ada", Email: "grace@example.com", DisplayName: "grace", FirstName: "Ada", LastName: "Lovelace",
	})
	assert.ErrorIs(t, err, ErrAccountExists)
}

func TestDeleteUserRefusesTheSignedInAccount(t *testing.T) {
	testutils.SkipWithoutDocker(t)

	pool := migratedPool(t)
	service := testService(t, pool)

	created, err := service.CreateUser(t.Context(), CreateParams{FirstName: "Ada", LastName: "Lovelace", Username: "ada", Email: "ada@example.com"})
	require.NoError(t, err)

	// The signed-in account is refused, whatever case the claims carry it in.
	err = service.DeleteUser(t.Context(), created.ID, "ADA")
	assert.ErrorIs(t, err, ErrSelfDeletion)

	other, err := service.CreateUser(t.Context(), CreateParams{FirstName: "Ada", LastName: "Lovelace", Username: "grace", Email: "grace@example.com"})
	require.NoError(t, err)
	require.NoError(t, service.DeleteUser(t.Context(), other.ID, "ada"))

	_, err = service.GetUser(t.Context(), other.ID)
	assert.ErrorIs(t, err, ErrUserNotFound)

	err = service.DeleteUser(t.Context(), other.ID, "ada")
	assert.ErrorIs(t, err, ErrUserNotFound)
}

// strPtr hands a ban reason its pointer form.
func strPtr(value string) *string {
	return &value
}

func TestUserProceduresRefuseACallerWithoutTheRole(t *testing.T) {
	handler := &rpcHandler{service: nil} // the gate runs before the service

	for name, ctx := range map[string]context.Context{
		"no identity": t.Context(),
		"non-admin":   authn.SetInfo(t.Context(), &jwtutils.AccessClaims{IsAdmin: false}),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := handler.ListUsers(ctx, connect.NewRequest(&identityv1.ListUsersRequest{}))
			assert.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))

			_, err = handler.GetUser(ctx, connect.NewRequest(&identityv1.GetUserRequest{Id: "00000000-0000-0000-0000-000000000000"}))
			assert.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))

			_, err = handler.CreateUser(ctx, connect.NewRequest(&identityv1.CreateUserRequest{}))
			assert.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))

			_, err = handler.UpdateUser(ctx, connect.NewRequest(&identityv1.UpdateUserRequest{}))
			assert.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))

			_, err = handler.DeleteUser(ctx, connect.NewRequest(&identityv1.DeleteUserRequest{Id: "00000000-0000-0000-0000-000000000000"}))
			assert.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
		})
	}
}

func TestMapErrorCarriesTheConnectCodes(t *testing.T) {
	cases := []struct {
		err  error
		code connect.Code
	}{
		{ErrUserNotFound, connect.CodeNotFound},
		{ErrAccountExists, connect.CodeAlreadyExists},
		{ErrSelfDeletion, connect.CodeFailedPrecondition},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.code, connect.CodeOf(mapError(tc.err)), "%v", tc.err)
	}
	assert.Equal(t, connect.CodeInternal, connect.CodeOf(mapError(errors.New("boom"))))
}
