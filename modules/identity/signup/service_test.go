package signup

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
	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/modules/identity/jwks"
	"github.com/riipandi/tango/modules/identity/signin"
	"github.com/riipandi/tango/pkg/crypto"
	"github.com/riipandi/tango/pkg/jwtutils"
	"github.com/riipandi/tango/pkg/testutils"
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

func TestSignupComposesTheDisplayNameFromTheNames(t *testing.T) {
	testutils.SkipWithoutDocker(t)

	pool := migratedPool(t)
	service := testService(t, pool)
	insertToken(t, pool, "valid-token", 1, 0)

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
			_, err := tc.service.Signup(t.Context(), Params{FirstName: "Ada", LastName: "Lovelace",
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

	params := Params{FirstName: "Ada", LastName: "Lovelace", Username: "ada", Email: "ada@example.com", Password: "correct horse", Token: "single-use"}
	_, err := service.Signup(t.Context(), params)
	require.NoError(t, err)

	_, err = service.Signup(t.Context(), Params{FirstName: "Ada", LastName: "Lovelace",
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

	_, err := service.Signup(t.Context(), Params{FirstName: "Ada", LastName: "Lovelace",
		Username: "ada", Email: "ada@example.com", Password: "correct horse", Token: "first-token",
	})
	require.NoError(t, err)

	// The email is TEXT matched exactly, the way its unique index is, so a
	// cased variant of a live address is a different address and signs up.
	for name, params := range map[string]Params{
		"same username":  {Username: "ada", Email: "grace@example.com", Password: "correct horse", Token: "second-token", FirstName: "Ada", LastName: "Lovelace"},
		"cased username": {Username: "ADA", Email: "grace@example.com", Password: "correct horse", Token: "third-token", FirstName: "Ada", LastName: "Lovelace"},
		"same email":     {Username: "grace", Email: "ada@example.com", Password: "correct horse", Token: "fourth-token", FirstName: "Grace", LastName: "Hopper"},
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

func TestMapErrorCarriesTheConnectCodes(t *testing.T) {
	cases := []struct {
		err  error
		code connect.Code
	}{
		{ErrInvalidToken, connect.CodePermissionDenied},
		{ErrAccountExists, connect.CodeAlreadyExists},
		{ErrTokenNotFound, connect.CodeNotFound},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.code, connect.CodeOf(mapError(tc.err)), "%v", tc.err)
	}
	assert.Equal(t, connect.CodeInternal, connect.CodeOf(mapError(errors.New("boom"))))
}

func TestSignupTokenIssueStoresTheHashAlone(t *testing.T) {
	testutils.SkipWithoutDocker(t)

	pool := migratedPool(t)
	service := testService(t, pool)

	created, err := service.CreateSignupToken(t.Context(), CreateTokenParams{TTL: 24 * time.Hour})
	require.NoError(t, err)

	assert.Regexp(t, `^[A-Za-z0-9_-]{43}$`, created.RawToken, "the raw token is 256 bits of base64url")
	assert.Equal(t, int32(1), created.Token.UsageLimit, "an unset budget is the single invitation")

	// The row stores the hash alone: the raw value must not be recoverable
	// from anything the database holds.
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select("token_hash", "usage_limit", "usage_count", "expires_at")
	sb.From(SignupTokenTable)
	sb.Where(sb.Equal("id", created.Token.ID))
	query, args := sb.Build()
	var tokenHash string
	var usageLimit, usageCount int32
	var expiresAt time.Time
	require.NoError(t, pool.QueryRow(t.Context(), query, args...).Scan(&tokenHash, &usageLimit, &usageCount, &expiresAt))
	assert.Equal(t, tokenSHA256(created.RawToken), tokenHash)
	assert.NotEqual(t, created.RawToken, tokenHash)
	assert.Equal(t, int32(1), usageLimit)
	assert.Equal(t, int32(0), usageCount)
	assert.WithinDuration(t, time.Now().Add(24*time.Hour), expiresAt, time.Minute)

	// The raw value admits the sign-up it was issued for.
	user, err := service.Signup(t.Context(), Params{FirstName: "Ada", LastName: "Lovelace",
		Username: "ada",
		Email:    "ada@example.com",
		Password: "correct horse",
		Token:    created.RawToken,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, user.ID)
	assert.Equal(t, int32(1), tokenUsageCount(t, pool, created.RawToken))
}

func TestSignupTokenListAndDelete(t *testing.T) {
	testutils.SkipWithoutDocker(t)

	pool := migratedPool(t)
	service := testService(t, pool)

	first, err := service.CreateSignupToken(t.Context(), CreateTokenParams{TTL: 24 * time.Hour})
	require.NoError(t, err)
	second, err := service.CreateSignupToken(t.Context(), CreateTokenParams{TTL: 48 * time.Hour, UsageLimit: 5})
	require.NoError(t, err)

	tokens, pagination, err := service.ListSignupTokens(t.Context(), 1, 0)
	require.NoError(t, err)
	require.Len(t, tokens, 2)
	assert.Equal(t, second.Token.ID, tokens[0].ID, "the list is newest first")
	assert.Equal(t, first.Token.ID, tokens[1].ID)
	assert.Equal(t, int32(5), tokens[0].UsageLimit)
	require.NotNil(t, pagination.TotalItems)
	assert.Equal(t, 2, *pagination.TotalItems)

	// A page beyond the set answers no items and an unknown range.
	tokens, pagination, err = service.ListSignupTokens(t.Context(), 2, 10)
	require.NoError(t, err)
	assert.Empty(t, tokens)
	assert.Nil(t, pagination.FirstItemIndex)

	require.NoError(t, service.DeleteSignupToken(t.Context(), first.Token.ID))

	tokens, pagination, err = service.ListSignupTokens(t.Context(), 1, 0)
	require.NoError(t, err)
	assert.Len(t, tokens, 1)
	require.NotNil(t, pagination.TotalItems)
	assert.Equal(t, 1, *pagination.TotalItems)

	assert.ErrorIs(t, service.DeleteSignupToken(t.Context(), first.Token.ID), ErrTokenNotFound)
	assert.ErrorIs(t, service.DeleteSignupToken(t.Context(), "not-a-uuid"), ErrTokenNotFound)
}

func TestTokenProceduresRefuseACallerWithoutTheRole(t *testing.T) {
	handler := &rpcHandler{service: nil} // the gate runs before the service

	for name, ctx := range map[string]context.Context{
		"no identity": t.Context(),
		"non-admin":   authn.SetInfo(t.Context(), &jwtutils.AccessClaims{IsAdmin: false}),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := handler.CreateSignupToken(ctx, connect.NewRequest(&identityv1.CreateSignupTokenRequest{TtlSeconds: 86400}))
			assert.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))

			_, err = handler.ListSignupTokens(ctx, connect.NewRequest(&identityv1.ListSignupTokensRequest{}))
			assert.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))

			_, err = handler.DeleteSignupToken(ctx, connect.NewRequest(&identityv1.DeleteSignupTokenRequest{Id: "00000000-0000-0000-0000-000000000000"}))
			assert.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
		})
	}
}
