package signup

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/huandu/go-sqlbuilder"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"connectrpc.com/connect"

	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/modules/identity/jwks"
	"github.com/riipandi/tango/modules/identity/signin"
	"github.com/riipandi/tango/pkg/crypto"
	"github.com/riipandi/tango/pkg/testutils"
	"github.com/riipandi/tango/pkg/validate"
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
		ApplicationName: "signup_test",
	})
	require.NoError(t, err)
	t.Cleanup(func() { pool.Shutdown(context.Background()) })
	return pool
}

func testService(t *testing.T, pool *datastore.Postgres) *Service {
	t.Helper()
	return NewService(pool, nil)
}

// insertToken writes the signup token row a raw value hashes to. The expires
// window must sit ahead of the database clock, the way the table's check
// demands, so a test that needs an expired token moves the service's clock
// instead.
func insertToken(t *testing.T, pool *datastore.Postgres, raw string, usageLimit, usageCount int32) {
	t.Helper()

	ib := sqlbuilder.PostgreSQL.NewInsertBuilder()
	ib.InsertInto(SignupTokenTable)
	ib.Cols("token_hash", "usage_limit", "usage_count", "expires_at")
	ib.Values(tokenSHA256(raw), usageLimit, usageCount, time.Now().Add(time.Hour))

	query, args := ib.Build()
	_, err := pool.Exec(t.Context(), query, args...)
	require.NoError(t, err)
}

func tokenUsageCount(t *testing.T, pool *datastore.Postgres, raw string) int32 {
	t.Helper()

	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select("usage_count")
	sb.From(SignupTokenTable)
	sb.Where(sb.Equal("token_hash", tokenSHA256(raw)))

	query, args := sb.Build()
	var count int32
	require.NoError(t, pool.QueryRow(t.Context(), query, args...).Scan(&count))
	return count
}

func userCount(t *testing.T, pool *datastore.Postgres) int {
	t.Helper()

	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select("count(*)")
	sb.From("public.users")

	query, args := sb.Build()
	var count int
	require.NoError(t, pool.QueryRow(t.Context(), query, args...).Scan(&count))
	return count
}

func TestSignupCreatesTheAccount(t *testing.T) {
	testutils.SkipWithoutDocker(t)

	pool := migratedPool(t)
	service := testService(t, pool)
	insertToken(t, pool, "valid-token", 3, 0)

	user, err := service.Signup(t.Context(), Params{
		Username:  "ada",
		Email:     "ada@example.com",
		Password:  "correct horse",
		Token:     "valid-token",
		FirstName: "Ada",
		LastName:  "Lovelace",
	})
	require.NoError(t, err)
	assert.Equal(t, "Ada Lovelace", user.DisplayName)

	// The account row carries the composed display name and stays
	// unverified: email verification is a later procedure.
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select("email", "display_name", "email_verified_at", "is_admin")
	sb.From("public.users")
	sb.Where(sb.Equal("username", "ada"))
	query, args := sb.Build()
	var email, displayName string
	var verifiedAt *time.Time
	var isAdmin bool
	require.NoError(t, pool.QueryRow(t.Context(), query, args...).Scan(&email, &displayName, &verifiedAt, &isAdmin))
	assert.Equal(t, "ada@example.com", email)
	assert.Nil(t, verifiedAt)
	assert.False(t, isAdmin)

	// The credential is stored as the hash the verifier accepts, never in
	// the clear.
	pb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	pb.Select("password_hash")
	pb.From("public.user_passwords")
	pb.Where(pb.Equal("user_id", user.ID))
	query, args = pb.Build()
	var passwordHash string
	require.NoError(t, pool.QueryRow(t.Context(), query, args...).Scan(&passwordHash))
	match, err := crypto.NewPasswordHasher().Verify("correct horse", passwordHash)
	require.NoError(t, err)
	assert.True(t, match)

	assert.Equal(t, int32(1), tokenUsageCount(t, pool, "valid-token"))

	// The password the caller chose signs in immediately.
	signinService := signin.NewService(testConfig(), signin.NewRepository(pool), jwks.NewService(testConfig(), nil, nil), nil)
	result, err := signinService.SignIn(t.Context(), signin.Params{
		Identity: "ada",
		Password: "correct horse",
	})
	require.NoError(t, err)
	assert.Equal(t, user.ID, result.User.ID)
}

func testConfig() config.Config {
	cfg := config.Default()
	cfg.Auth.PrivateKey = ""
	cfg.Auth.PublicKey = ""
	cfg.Auth.SecretKey = "0123456789abcdeffedcba98765432100123456789abcdeffedcba9876543210"
	return cfg
}

func TestSignupDisplayNameFallsBackToUsername(t *testing.T) {
	testutils.SkipWithoutDocker(t)

	pool := migratedPool(t)
	service := testService(t, pool)
	insertToken(t, pool, "valid-token", 1, 0)

	user, err := service.Signup(t.Context(), Params{
		Username: "ada",
		Email:    "ada@example.com",
		Password: "correct horse",
		Token:    "valid-token",
	})
	require.NoError(t, err)
	assert.Equal(t, "ada", user.DisplayName)
}

func TestSignupRequiresValidToken(t *testing.T) {
	testutils.SkipWithoutDocker(t)

	pool := migratedPool(t)
	service := testService(t, pool)

	// The unknown token shares the failure with the expired and the spent
	// one, so the endpoint does not disclose which half was wrong.
	insertToken(t, pool, "spent-token", 1, 1)
	insertToken(t, pool, "exhausted-token", 2, 2)

	expired := testService(t, pool)
	expired.now = func() time.Time { return time.Now().Add(2 * time.Hour) }

	for name, tc := range map[string]struct {
		service *Service
		token   string
	}{
		"unknown":   {service, "nobody-token"},
		"spent":     {service, "spent-token"},
		"exhausted": {service, "exhausted-token"},
		"expired":   {expired, "exhausted-token"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := tc.service.Signup(t.Context(), Params{
				Username: "ada",
				Email:    "ada@example.com",
				Password: "correct horse",
				Token:    tc.token,
			})
			require.ErrorIs(t, err, ErrInvalidToken)
		})
	}
	assert.Equal(t, 0, userCount(t, pool))
}

func TestSignupConsumesASingleUseTokenOnce(t *testing.T) {
	testutils.SkipWithoutDocker(t)

	pool := migratedPool(t)
	service := testService(t, pool)
	insertToken(t, pool, "single-use", 1, 0)

	params := Params{Username: "ada", Email: "ada@example.com", Password: "correct horse", Token: "single-use"}
	_, err := service.Signup(t.Context(), params)
	require.NoError(t, err)

	_, err = service.Signup(t.Context(), Params{
		Username: "grace",
		Email:    "grace@example.com",
		Password: "correct horse",
		Token:    "single-use",
	})
	require.ErrorIs(t, err, ErrInvalidToken)

	// The refused attempt wrote nothing: one account, one use.
	assert.Equal(t, 1, userCount(t, pool))
	assert.Equal(t, int32(1), tokenUsageCount(t, pool, "single-use"))
}

func TestSignupRejectsDuplicateAccount(t *testing.T) {
	testutils.SkipWithoutDocker(t)

	pool := migratedPool(t)
	service := testService(t, pool)
	insertToken(t, pool, "first-token", 1, 0)
	for _, token := range []string{"second-token", "third-token", "fourth-token"} {
		insertToken(t, pool, token, 1, 0)
	}

	_, err := service.Signup(t.Context(), Params{
		Username: "ada", Email: "ada@example.com", Password: "correct horse", Token: "first-token",
	})
	require.NoError(t, err)

	// The email is TEXT matched exactly, the way its unique index is, so a
	// cased variant of a live address is a different address and signs up.
	for name, params := range map[string]Params{
		"same username":  {Username: "ada", Email: "grace@example.com", Password: "correct horse", Token: "second-token"},
		"cased username": {Username: "ADA", Email: "grace@example.com", Password: "correct horse", Token: "third-token"},
		"same email":     {Username: "grace", Email: "ada@example.com", Password: "correct horse", Token: "fourth-token"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := service.Signup(t.Context(), params)
			require.ErrorIs(t, err, ErrAccountExists)
		})
	}

	// The refused attempts wrote nothing: one account, no use spent.
	assert.Equal(t, 1, userCount(t, pool))
	assert.Equal(t, int32(0), tokenUsageCount(t, pool, "second-token"))
}

func TestSignupValidatesTheInput(t *testing.T) {
	// The rules run before any database work, so the service runs without
	// a pool here.
	service := NewService(nil, nil)

	for name, field := range map[string]string{
		"short username":  "username",
		"bad username":    "username",
		"missing domain":  "email",
		"missing address": "email",
		"empty request":   "username",
	} {
		t.Run(name, func(t *testing.T) {
			params := Params{Username: "ada", Email: "ada@example.com", Password: "correct horse", Token: "valid-token"}
			switch name {
			case "short username":
				params.Username = "ab"
			case "bad username":
				params.Username = "ada lovelace"
			case "missing domain":
				params.Email = "ada@example"
			case "missing address":
				params.Email = "@example.com"
			case "empty request":
				params = Params{}
			}

			_, err := service.Signup(t.Context(), params)
			require.True(t, validate.IsValidationError(err))

			fields := make([]string, 0, 2)
			for _, fe := range validate.FieldErrors(err) {
				fields = append(fields, fe.Field)
			}
			assert.Contains(t, fields, field)
		})
	}
}

func TestSignupRefusesAnIncompleteRequest(t *testing.T) {
	service := NewService(nil, nil) // validation returns before the pool

	_, err := service.Signup(t.Context(), Params{})
	require.True(t, validate.IsValidationError(err))

	fields := make([]string, 0, 4)
	for _, fe := range validate.FieldErrors(err) {
		fields = append(fields, fe.Field)
	}
	assert.ElementsMatch(t, []string{"username", "email", "password", "token"}, fields)
}

func TestMapErrorCarriesTheConnectCodes(t *testing.T) {
	cases := []struct {
		err  error
		code connect.Code
	}{
		{ErrInvalidToken, connect.CodePermissionDenied},
		{ErrAccountExists, connect.CodeAlreadyExists},
		{Params{}.Validate(), connect.CodeInvalidArgument},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.code, connect.CodeOf(mapError(tc.err)), "%v", tc.err)
	}
	assert.Equal(t, connect.CodeInternal, connect.CodeOf(mapError(errors.New("boom"))))
}
