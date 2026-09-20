package seeders_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/database/seeders"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/pkg/crypto"
	"github.com/riipandi/tango/pkg/testutils"
)

// newSeededPool returns a pool on a fresh database with the schema applied, so
// a seeder has the tables it writes to.
func newSeededPool(t *testing.T) *datastore.Postgres {
	t.Helper()

	container := testutils.StartPostgres(t.Context(), t)
	dsn := container.NewDatabase(t)

	db, err := datastore.OpenMigrationDB(t.Context(), datastore.PostgresOptions{DSN: dsn})
	require.NoError(t, err)

	migrator, err := database.NewMigrator(t.Context(), db, database.MigratorOptions{})
	require.NoError(t, err)

	applied, err := migrator.Up(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, applied)
	require.NoError(t, db.Close())

	pool, err := datastore.NewPostgres(t.Context(), datastore.PostgresOptions{DSN: dsn})
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

// runSeeders runs every seeder inside one transaction, the way the command does.
func runSeeders(t *testing.T, pool *datastore.Postgres, dryRun bool) []seeders.Result {
	t.Helper()

	if dryRun {
		results, err := seeders.Run(t.Context(), pool, true, seeders.All()...)
		require.NoError(t, err)
		return results
	}

	var results []seeders.Result
	err := pool.WithTx(t.Context(), func(ctx context.Context, tx pgx.Tx) error {
		var seedErr error
		results, seedErr = seeders.Run(ctx, tx, false, seeders.All()...)
		return seedErr
	})
	require.NoError(t, err)
	return results
}

// userCount counts the accounts in the database.
func userCount(t *testing.T, pool *datastore.Postgres) int {
	t.Helper()

	var count int
	require.NoError(t, pool.QueryRow(t.Context(), "SELECT count(*) FROM public.users").Scan(&count))
	return count
}

// storedHash reads the password hash of the default account.
func storedHash(t *testing.T, pool *datastore.Postgres) string {
	t.Helper()

	var hash string
	err := pool.QueryRow(t.Context(),
		"SELECT p.password_hash FROM public.user_passwords p JOIN public.users u ON u.id = p.user_id WHERE u.email = $1",
		seeders.DefaultUser.Email).Scan(&hash)
	require.NoError(t, err)
	return hash
}

// Every seeder must be reachable from All, or it never runs.
func TestAllIncludesTheUserSeeder(t *testing.T) {
	names := make([]string, 0, len(seeders.All()))
	for _, seeder := range seeders.All() {
		names = append(names, seeder.Name)
	}
	assert.Contains(t, names, seeders.UserSeederName)
}

func TestUserSeederCreatesTheDefaultAccount(t *testing.T) {
	pool := newSeededPool(t)

	results := runSeeders(t, pool, false)

	require.Len(t, results, 1)
	assert.Equal(t, seeders.UserSeederName, results[0].Name)
	assert.Equal(t, []string{seeders.DefaultUser.Email}, results[0].Created)
	assert.Empty(t, results[0].Skipped)

	var (
		username    string
		displayName string
		firstName   string
		lastName    string
		isAdmin     bool
	)
	require.NoError(t, pool.QueryRow(t.Context(),
		"SELECT username, display_name, first_name, last_name, is_admin FROM public.users WHERE email = $1",
		seeders.DefaultUser.Email).Scan(&username, &displayName, &firstName, &lastName, &isAdmin))

	assert.Equal(t, seeders.DefaultUser.Username, username)
	assert.Equal(t, seeders.DefaultUser.DisplayName(), displayName)
	assert.Equal(t, seeders.DefaultUser.FirstName, firstName)
	assert.Equal(t, seeders.DefaultUser.LastName, lastName)
	// The bootstrap account must be able to grant access to everyone else.
	assert.True(t, isAdmin)
}

// DisplayName is what the UI shows, so it must combine both names and survive
// an account that carries only one of them.
func TestUserCredentialsDisplayName(t *testing.T) {
	tests := []struct {
		name  string
		creds seeders.UserCredentials
		want  string
	}{
		{
			name:  "both names",
			creds: seeders.UserCredentials{FirstName: "Admin", LastName: "Sistem"},
			want:  "Admin Sistem",
		},
		{
			name:  "first name only",
			creds: seeders.UserCredentials{FirstName: "Admin"},
			want:  "Admin",
		},
		{
			name:  "last name only",
			creds: seeders.UserCredentials{LastName: "Sistem"},
			want:  "Sistem",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.creds.DisplayName())
		})
	}

	// The default account must produce the name the seed writes.
	assert.Equal(t, "Admin Sistem", seeders.DefaultUser.DisplayName())
}

// The stored hash must verify against the documented password, or the account
// exists but nobody can log in.
func TestUserSeederStoresAVerifiablePassword(t *testing.T) {
	pool := newSeededPool(t)

	runSeeders(t, pool, false)
	hash := storedHash(t, pool)

	assert.Contains(t, hash, "$scrypt$", "the default hasher is scrypt")

	hasher := crypto.NewPasswordHasher()
	ok, err := hasher.Verify(seeders.DefaultUser.Password, hash)
	require.NoError(t, err)
	assert.True(t, ok, "the stored hash must match the documented password")

	wrong, err := hasher.Verify("@dmin124", hash)
	require.NoError(t, err)
	assert.False(t, wrong)
}

// A second run must change nothing, so migrate:seed is safe to repeat.
func TestUserSeederIsIdempotent(t *testing.T) {
	pool := newSeededPool(t)

	first := runSeeders(t, pool, false)
	require.Equal(t, []string{seeders.DefaultUser.Email}, first[0].Created)
	hash := storedHash(t, pool)

	second := runSeeders(t, pool, false)

	assert.Empty(t, second[0].Created)
	assert.Equal(t, []string{seeders.DefaultUser.Email}, second[0].Skipped)
	assert.Equal(t, 1, userCount(t, pool))
	assert.Equal(t, hash, storedHash(t, pool), "a skipped account must keep its password")
}

// public.users.email is TEXT, not citext, so the lookup is case-sensitive. The
// seeder claims its own address and leaves a differently-cased one alone, which
// is what the unique index on email does.
func TestUserSeederTreatsEmailCaseSensitively(t *testing.T) {
	pool := newSeededPool(t)
	ctx := t.Context()

	_, err := pool.Exec(ctx, `INSERT INTO public.users (username, email, display_name) VALUES ($1, $2, $3)`,
		"someone", "ADMIN@EXAMPLE.COM", "Someone")
	require.NoError(t, err)

	results := runSeeders(t, pool, false)

	assert.Equal(t, []string{seeders.DefaultUser.Email}, results[0].Created)
	assert.Empty(t, results[0].Skipped)
	assert.Equal(t, 2, userCount(t, pool))
}

// A conflict on the username alone must also leave the existing account in
// place: the seeder inserts without a conflict target for exactly this reason.
func TestUserSeederKeepsAccountWithConflictingUsername(t *testing.T) {
	pool := newSeededPool(t)
	ctx := t.Context()

	_, err := pool.Exec(ctx, `INSERT INTO public.users (username, email, display_name) VALUES ($1, $2, $3)`,
		seeders.DefaultUser.Username, "other@example.com", "Other")
	require.NoError(t, err)

	results := runSeeders(t, pool, false)

	assert.Empty(t, results[0].Created)
	assert.Equal(t, []string{seeders.DefaultUser.Email}, results[0].Skipped)
	assert.Equal(t, 1, userCount(t, pool))
}

// --dry-run must report the work without doing it.
func TestUserSeederDryRunWritesNothing(t *testing.T) {
	pool := newSeededPool(t)

	results := runSeeders(t, pool, true)

	assert.Equal(t, []string{seeders.DefaultUser.Email}, results[0].Created)
	assert.Zero(t, userCount(t, pool), "a dry run must not insert the account")

	// Once the account exists, the dry run reports it as skipped instead.
	runSeeders(t, pool, false)
	results = runSeeders(t, pool, true)

	assert.Empty(t, results[0].Created)
	assert.Equal(t, []string{seeders.DefaultUser.Email}, results[0].Skipped)
	assert.Equal(t, 1, userCount(t, pool))
}

// A failing seeder must abort the run and roll back what earlier seeders wrote,
// so a partial seed is never left behind.
func TestRunRollsBackWhenASeederFails(t *testing.T) {
	pool := newSeededPool(t)
	failure := assert.AnError

	err := pool.WithTx(t.Context(), func(ctx context.Context, tx pgx.Tx) error {
		_, seedErr := seeders.Run(ctx, tx, false,
			seeders.User(),
			seeders.Seeder{
				Name: "boom",
				Apply: func(context.Context, datastore.Querier, bool) ([]string, []string, error) {
					return nil, nil, failure
				},
			},
		)
		return seedErr
	})

	require.ErrorIs(t, err, failure)
	assert.Zero(t, userCount(t, pool), "the user the first seeder created must be rolled back")
}

// Run must name the seeder that failed, so a partial run points at one file.
func TestRunStopsAtTheFailingSeeder(t *testing.T) {
	pool := newSeededPool(t)
	failure := assert.AnError
	ran := false

	_, err := seeders.Run(t.Context(), pool, false,
		seeders.User(),
		seeders.Seeder{
			Name: "boom",
			Apply: func(context.Context, datastore.Querier, bool) ([]string, []string, error) {
				return nil, nil, failure
			},
		},
		seeders.Seeder{
			Name: "after",
			Apply: func(context.Context, datastore.Querier, bool) ([]string, []string, error) {
				ran = true
				return nil, nil, nil
			},
		},
	)

	require.ErrorIs(t, err, failure)
	assert.Contains(t, err.Error(), "seeders: boom", "the error must name the seeder")
	assert.False(t, ran, "a later seeder must not run after a failure")
}
