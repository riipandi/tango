package user

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/huandu/go-sqlbuilder"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"connectrpc.com/authn"
	"connectrpc.com/connect"

	identityv1 "github.com/riipandi/tango/codegen/proto/go/tango/identity/v1"
	"github.com/riipandi/tango/database"
	"log/slog"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	awshttp "github.com/aws/smithy-go/transport/http"
	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/internal/storage"
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

// readPicture opens the account's picture through the service's read, so a
// test reads the body the transport would stream.
func (s *Service) readPicture(ctx context.Context, id string) (io.ReadCloser, string, error) {
	view, err := s.ProfilePicture(ctx, id)
	if err != nil {
		return nil, "", err
	}
	return view.Body, view.ContentType, nil
}

func testService(t *testing.T, pool *datastore.Postgres) *Service {
	t.Helper()
	return NewService(pool, nil, nil)
}

// testPictureService builds the service over the real storage engine — the
// local driver in a throwaway directory — so a picture procedure runs the
// stage, sync, and read the production path runs. The engine and its
// directory travel with the test through the returned cleanup.
func testPictureService(t *testing.T, pool *datastore.Postgres) (*Service, *storage.Manager) {
	t.Helper()

	manager := storage.NewManager(storage.NewFS(t.TempDir()), pool,
		t.TempDir(), slog.New(slog.DiscardHandler))
	return NewService(pool, nil, manager), manager
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
		Username:      "hermione",
		Email:         "hermione@example.com",
		Password:      "expecto-patronum",
		FirstName:     "Hermione",
		LastName:      "Granger",
		IsAdmin:       true,
		EmailVerified: true,
	})
	require.NoError(t, err)

	assert.NotEmpty(t, created.ID)
	assert.Equal(t, "hermione", created.Username)
	assert.Equal(t, "hermione@example.com", created.Email)
	assert.Equal(t, "Hermione Granger", created.DisplayName)
	assert.True(t, created.IsAdmin)
	assert.True(t, created.EmailVerified)
	assert.False(t, created.CreatedAt.IsZero())
	assert.Equal(t, 1, passwordCount(t, pool, created.ID))

	read, err := service.GetUser(t.Context(), created.ID)
	require.NoError(t, err)
	assert.Equal(t, "Hermione Granger", read.DisplayName)
	require.NotNil(t, read.FirstName)
	assert.Equal(t, "Hermione", *read.FirstName)
}

func TestCreateUserWithoutAPasswordCarriesNoCredential(t *testing.T) {
	testutils.SkipWithoutDocker(t)

	pool := migratedPool(t)
	service := testService(t, pool)

	created, err := service.CreateUser(t.Context(), CreateParams{FirstName: "Robert", LastName: "Langdon",
		Username: "langdon",
		Email:    "langdon@example.com",
	})
	require.NoError(t, err)

	// The display name is composed from the mandatory names, and no
	// credential row exists to sign in with.
	assert.Equal(t, "Robert Langdon", created.DisplayName)
	assert.False(t, created.EmailVerified)
	assert.Equal(t, 0, passwordCount(t, pool, created.ID))
}

func TestCreateUserRefusesADuplicateAccount(t *testing.T) {
	testutils.SkipWithoutDocker(t)

	pool := migratedPool(t)
	service := testService(t, pool)

	_, err := service.CreateUser(t.Context(), CreateParams{FirstName: "Hermione", LastName: "Granger", Username: "hermione", Email: "hermione@example.com"})
	require.NoError(t, err)

	// The username matches case-insensitively, the way its unique index does.
	_, err = service.CreateUser(t.Context(), CreateParams{FirstName: "Hermione", LastName: "Granger", Username: "HERMIONE", Email: "other@example.com"})
	assert.ErrorIs(t, err, ErrAccountExists)

	_, err = service.CreateUser(t.Context(), CreateParams{FirstName: "Hermione", LastName: "Granger", Username: "other", Email: "hermione@example.com"})
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

	// The four accounts carry distinct names, so the search's one match is
	// observable: the Granger name is hers alone.
	for _, account := range []struct{ username, first, last string }{
		{"hermione", "Hermione", "Granger"},
		{"langdon", "Robert", "Langdon"},
		{"sophie", "Sophie", "Neveu"},
		{"vittoria", "Vittoria", "Vetra"},
	} {
		_, err := service.CreateUser(t.Context(), CreateParams{
			Username: account.username, Email: account.username + "@example.com",
			FirstName: account.first, LastName: account.last,
		})
		require.NoError(t, err)
	}

	users, pagination, err := service.ListUsers(t.Context(), "", 1, 2)
	require.NoError(t, err)
	assert.Len(t, users, 2)
	require.NotNil(t, pagination.TotalItems)
	assert.Equal(t, 4, *pagination.TotalItems)

	// The search matches the username and the name parts, case-insensitively,
	// and answers only the accounts it names.
	users, _, err = service.ListUsers(t.Context(), "GRA", 1, 10)
	require.NoError(t, err)
	require.Len(t, users, 1)
	assert.Equal(t, "hermione", users[0].Username)
}

func TestUpdateUserReplacesTheFields(t *testing.T) {
	testutils.SkipWithoutDocker(t)

	pool := migratedPool(t)
	service := testService(t, pool)

	created, err := service.CreateUser(t.Context(), CreateParams{
		Username:  "hermione",
		Email:     "hermione@example.com",
		FirstName: "Hermione",
	})
	require.NoError(t, err)

	// The full replace is the whole shape: every field is specified, and the
	// names are mandatory, so a name part left empty is a contract refusal
	// the transport answers before the service runs.
	updated, err := service.UpdateUser(t.Context(), created.ID, UpdateParams{
		Username:    "hermione",
		Email:       "grey.lady@example.com",
		FirstName:   "Hermione",
		LastName:    "King",
		DisplayName: "The Grey Lady",
		Locale:      "en-GB",
		Disabled:    true,
	})
	require.NoError(t, err)
	assert.Equal(t, "The Grey Lady", updated.DisplayName)
	assert.Equal(t, "grey.lady@example.com", updated.Email)
	assert.True(t, updated.Disabled)
	require.NotNil(t, updated.FirstName)
	assert.Equal(t, "Hermione", *updated.FirstName)
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

	created, err := service.CreateUser(t.Context(), CreateParams{FirstName: "Hermione", LastName: "Granger", Username: "hermione", Email: "hermione@example.com"})
	require.NoError(t, err)

	expires := service.now().Add(24 * time.Hour)
	banned, err := service.UpdateUser(t.Context(), created.ID, UpdateParams{
		Username:     "hermione",
		Email:        "hermione@example.com",
		FirstName:    "Hermione",
		LastName:     "Granger",
		DisplayName:  "hermione",
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
		Username:     "hermione",
		Email:        "hermione@example.com",
		FirstName:    "Hermione",
		LastName:     "Granger",
		DisplayName:  "hermione",
		BanExpiresAt: &later,
	})
	require.NoError(t, err)
	assert.Equal(t, service.now(), *rebanned.BannedAt)

	lifted, err := service.UpdateUser(t.Context(), created.ID, UpdateParams{
		Username:    "hermione",
		Email:       "hermione@example.com",
		DisplayName: "hermione",
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

	_, err := service.CreateUser(t.Context(), CreateParams{FirstName: "Hermione", LastName: "Granger", Username: "hermione", Email: "hermione@example.com"})
	require.NoError(t, err)

	_, err = service.UpdateUser(t.Context(), uuid.NewV7().String(), UpdateParams{
		Username: "hermione", Email: "ron@example.com", DisplayName: "x", FirstName: "Hermione", LastName: "Granger",
	})
	assert.ErrorIs(t, err, ErrUserNotFound)

	other, err := service.CreateUser(t.Context(), CreateParams{FirstName: "Hermione", LastName: "Granger", Username: "langdon", Email: "langdon@example.com"})
	require.NoError(t, err)

	_, err = service.UpdateUser(t.Context(), other.ID, UpdateParams{
		Username: "hermione", Email: "langdon@example.com", DisplayName: "langdon", FirstName: "Hermione", LastName: "Granger",
	})
	assert.ErrorIs(t, err, ErrAccountExists)
}

func TestDeleteUserRefusesTheSignedInAccount(t *testing.T) {
	testutils.SkipWithoutDocker(t)

	pool := migratedPool(t)
	service := testService(t, pool)

	created, err := service.CreateUser(t.Context(), CreateParams{FirstName: "Hermione", LastName: "Granger", Username: "hermione", Email: "hermione@example.com"})
	require.NoError(t, err)

	// The signed-in account is refused, whatever case the claims carry it in.
	err = service.DeleteUser(t.Context(), created.ID, "HERMIONE")
	assert.ErrorIs(t, err, ErrSelfDeletion)

	other, err := service.CreateUser(t.Context(), CreateParams{FirstName: "Hermione", LastName: "Granger", Username: "langdon", Email: "langdon@example.com"})
	require.NoError(t, err)
	require.NoError(t, service.DeleteUser(t.Context(), other.ID, "hermione"))

	_, err = service.GetUser(t.Context(), other.ID)
	assert.ErrorIs(t, err, ErrUserNotFound)

	err = service.DeleteUser(t.Context(), other.ID, "hermione")
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

// TestThePictureFlowStagesSyncsAndReadsBack runs the update through the real
// engine — stage, inline sync, row pointer — and reads the bytes back with
// the content type the update recorded.
func TestThePictureFlowStagesSyncsAndReadsBack(t *testing.T) {
	testutils.SkipWithoutDocker(t)

	pool := migratedPool(t)
	service, pictures := testPictureService(t, pool)
	created, err := service.CreateUser(t.Context(), CreateParams{
		Username: "hermione", Email: "hermione@example.com", Password: "expecto-patronum",
		FirstName: "Hermione", LastName: "Granger",
	})
	require.NoError(t, err)
	claims := &jwtutils.AccessClaims{Username: "hermione", IsAdmin: false}

	picture := bytes.Repeat([]byte("A"), 80) // PNG magic + filler
	picture[0], picture[3] = 0x89, 'N'
	copy(picture, []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
	require.NoError(t, service.UpdateProfilePicture(t.Context(), created.ID, claims, picture))

	body, mime, err := service.readPicture(t.Context(), created.ID)
	require.NoError(t, err)
	defer body.Close()
	assert.Equal(t, "image/png", mime)
	read, err := io.ReadAll(body)
	require.NoError(t, err)
	assert.Equal(t, picture, read)

	// The engine holds the file whole under the key the row names — the
	// same tree of keys both drivers keep.
	stored, err := pictures.Open(t.Context(), "avatars/"+created.ID+"/profile-picture")
	require.NoError(t, err)
	storedBody, err := io.ReadAll(stored)
	require.NoError(t, err)
	require.NoError(t, stored.Close())
	assert.Equal(t, picture, storedBody)

	// The row names the key the engine holds the file under.
	var storedPath *string
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select("avatar_url")
	sb.From(UserTable)
	sb.Where(sb.Equal("id", created.ID))
	query, args := sb.Build()
	require.NoError(t, pool.QueryRow(t.Context(), query, args...).Scan(&storedPath))
	require.NotNil(t, storedPath)
	assert.Equal(t, "avatars/"+created.ID+"/profile-picture", *storedPath)
}

// TestPictureUpdateSniffsTheBytesRatherThanTheDeclaration refuses a payload
// no accepted image kind claims, whatever its name says.
func TestPictureUpdateSniffsTheBytesRatherThanTheDeclaration(t *testing.T) {
	testutils.SkipWithoutDocker(t)

	pool := migratedPool(t)
	service, _ := testPictureService(t, pool)
	created, err := service.CreateUser(t.Context(), CreateParams{
		Username: "hermione", Email: "hermione@example.com", Password: "expecto-patronum",
		FirstName: "Hermione", LastName: "Granger",
	})
	require.NoError(t, err)
	claims := &jwtutils.AccessClaims{Username: "hermione", IsAdmin: false}

	err = service.UpdateProfilePicture(t.Context(), created.ID, claims, []byte("definitely not an image"))
	assert.ErrorIs(t, err, ErrUnsupportedPicture)

	// The WAV file shares the RIFF form with WebP; the format field tells
	// them apart.
	wav := append([]byte("RIFF"), make([]byte, 8)...)
	copy(wav[8:], "WAVE")
	err = service.UpdateProfilePicture(t.Context(), created.ID, claims, wav)
	assert.ErrorIs(t, err, ErrUnsupportedPicture)
}

// TestPictureEditBelongsToTheOwnerOrAnAdministrator keeps the two-gate rule:
// the owner under whatever case the claims carry, an administrator for any
// account, nobody else.
func TestPictureEditBelongsToTheOwnerOrAnAdministrator(t *testing.T) {
	testutils.SkipWithoutDocker(t)

	pool := migratedPool(t)
	service, _ := testPictureService(t, pool)
	created, err := service.CreateUser(t.Context(), CreateParams{
		Username: "hermione", Email: "hermione@example.com", Password: "expecto-patronum",
		FirstName: "Hermione", LastName: "Granger",
	})
	require.NoError(t, err)
	picture := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}

	for name, claims := range map[string]*jwtutils.AccessClaims{
		"owner":    {Username: "HERMIONE", IsAdmin: false},
		"admin":    {Username: "langdon", IsAdmin: true},
		"stranger": {Username: "langdon", IsAdmin: false},
	} {
		updateErr := service.UpdateProfilePicture(t.Context(), created.ID, claims, picture)
		if name == "stranger" {
			assert.ErrorIs(t, updateErr, ErrPictureForbidden, name)
			continue
		}
		assert.NoError(t, updateErr, name)
	}

	err = service.ResetProfilePicture(t.Context(), created.ID, &jwtutils.AccessClaims{Username: "langdon", IsAdmin: false})
	assert.ErrorIs(t, err, ErrPictureForbidden)
}

// TestPictureResetFallsBackToTheDefault clears the row and removes the file,
// so the read answers the bundled default the frontend ships.
func TestPictureResetFallsBackToTheDefault(t *testing.T) {
	testutils.SkipWithoutDocker(t)

	pool := migratedPool(t)
	service, pictures := testPictureService(t, pool)
	created, err := service.CreateUser(t.Context(), CreateParams{
		Username: "hermione", Email: "hermione@example.com", Password: "expecto-patronum",
		FirstName: "Hermione", LastName: "Granger",
	})
	require.NoError(t, err)
	claims := &jwtutils.AccessClaims{Username: "hermione", IsAdmin: false}
	picture := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 1, 2}
	require.NoError(t, service.UpdateProfilePicture(t.Context(), created.ID, claims, picture))

	// The read before the reset answers the stored bytes.
	body, _, readErr := service.readPicture(t.Context(), created.ID)
	require.NoError(t, readErr)
	body.Close()

	require.NoError(t, service.ResetProfilePicture(t.Context(), created.ID, claims))

	var storedPath *string
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select("avatar_url")
	sb.From(UserTable)
	sb.Where(sb.Equal("id", created.ID))
	query, args := sb.Build()
	require.NoError(t, pool.QueryRow(t.Context(), query, args...).Scan(&storedPath))
	assert.Nil(t, storedPath)

	// The file left the engine: no read answers the key anymore.
	_, err = pictures.Open(t.Context(), "avatars/"+created.ID+"/profile-picture")
	assert.ErrorIs(t, err, storage.ErrNotFound)

	view, err := service.ProfilePicture(t.Context(), created.ID)
	require.NoError(t, err)
	defer view.Body.Close()
	assert.True(t, view.Default)
	empty, err := io.ReadAll(view.Body)
	require.NoError(t, err)
	assert.Empty(t, empty)

	// The file left the engine: the sync the update ran holds no copy the
	// reset forgot.
	assert.True(t, view.Default, "the read after the reset answers the default")
}

// TestPictureRefusesAnUnknownAccount keeps the not-found boundary on every
// picture procedure.
func TestPictureRefusesAnUnknownAccount(t *testing.T) {
	testutils.SkipWithoutDocker(t)

	pool := migratedPool(t)
	service, _ := testPictureService(t, pool)
	claims := &jwtutils.AccessClaims{Username: "hermione", IsAdmin: true}

	id := "00000000-0000-0000-0000-000000000000"
	err := service.UpdateProfilePicture(t.Context(), id, claims, []byte("x"))
	assert.ErrorIs(t, err, ErrUserNotFound)
	err = service.ResetProfilePicture(t.Context(), id, claims)
	assert.ErrorIs(t, err, ErrUserNotFound)
	_, err = service.ProfilePicture(t.Context(), id)
	assert.ErrorIs(t, err, ErrUserNotFound)
}

// TestThePictureFlowLandsOnS3 runs the same update through the S3-compatible
// backend — the local driver's twin — and reads the bytes back through the
// engine, so the picture procedures are indifferent to the store they are
// given.
func TestThePictureFlowLandsOnS3(t *testing.T) {
	testutils.SkipWithoutDocker(t)

	pool := migratedPool(t)
	backend := testutils.StartMinIO(t.Context(), t)
	store, err := storage.NewS3(config.S3{
		AccessKeyID:     backend.AccessKey,
		AccessKeySecret: backend.Secret,
		BucketName:      "tango-user-test",
		EndpointURL:     backend.Endpoint,
		ForcePathStyle:  true,
		Region:          "us-east-1",
		PathPrefix:      "tango-user-test/",
	})
	require.NoError(t, err)
	// The shared container starts empty: the bucket is this test's to make,
	// and an existing one is fine. The client is built over the same
	// endpoint the store addresses — the store itself owns no bucket.
	awsCfg, err := awsconfig.LoadDefaultConfig(t.Context(), awsconfig.WithRegion("us-east-1"),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			backend.AccessKey, backend.Secret, "")),
		awsconfig.WithBaseEndpoint(backend.Endpoint))
	require.NoError(t, err)
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) { o.UsePathStyle = true })
	if _, bucketErr := client.CreateBucket(t.Context(), &s3.CreateBucketInput{
		Bucket: awssdk.String("tango-user-test"),
	}); bucketErr != nil {
		var apiErr *awshttp.ResponseError
		if !errors.As(bucketErr, &apiErr) || apiErr.HTTPStatusCode() != 409 {
			require.NoError(t, bucketErr)
		}
	}

	manager := storage.NewManager(store, pool, t.TempDir(), slog.New(slog.DiscardHandler))
	service := NewService(pool, nil, manager)

	created, err := service.CreateUser(t.Context(), CreateParams{
		Username: "hermione", Email: "hermione@example.com", Password: "expecto-patronum",
		FirstName: "Hermione", LastName: "Granger",
	})
	require.NoError(t, err)
	claims := &jwtutils.AccessClaims{Username: "hermione", IsAdmin: false}

	picture := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 9, 8, 7}
	require.NoError(t, service.UpdateProfilePicture(t.Context(), created.ID, claims, picture))

	// The read streams the chunks the backend holds — the same flow the
	// local driver answers, no S3-specific branch anywhere in the feature.
	view, mime, err := service.readPicture(t.Context(), created.ID)
	require.NoError(t, err)
	defer view.Close()
	assert.Equal(t, "image/png", mime)
	read, err := io.ReadAll(view)
	require.NoError(t, err)
	assert.Equal(t, picture, read)

	// The bucket holds the final file at the key — the same tree the local
	// driver keeps — and nothing else: no chunk-shaped object ever lands
	// beside it.
	listed, err := client.ListObjectsV2(t.Context(), &s3.ListObjectsV2Input{
		Bucket: awssdk.String("tango-user-test"),
		Prefix: awssdk.String("tango-user-test/"),
	})
	require.NoError(t, err)
	var keys []string
	for _, item := range listed.Contents {
		keys = append(keys, awssdk.ToString(item.Key))
	}
	assert.Equal(t,
		[]string{"tango-user-test/avatars/" + created.ID + "/profile-picture"}, keys)

	// The reset removes the object the backend holds, so the account falls
	// back to the bundled default the same way it does on the local driver.
	require.NoError(t, service.ResetProfilePicture(t.Context(), created.ID, claims))
	fallback, err := service.ProfilePicture(t.Context(), created.ID)
	require.NoError(t, err)
	defer fallback.Body.Close()
	assert.True(t, fallback.Default)

	// The reset emptied the bucket: the object left with the account's
	// key, the same state a local reset lands in.
	listed, err = client.ListObjectsV2(t.Context(), &s3.ListObjectsV2Input{
		Bucket: awssdk.String("tango-user-test"),
		Prefix: awssdk.String("tango-user-test/"),
	})
	require.NoError(t, err)
	assert.Empty(t, listed.Contents)
}

// TestPictureProceduresRefuseARunWithoutTheEngine covers the deployment that
// serves accounts without picture storage: the account procedures answer as
// usual, the picture procedures refuse.
func TestPictureProceduresRefuseARunWithoutTheEngine(t *testing.T) {
	testutils.SkipWithoutDocker(t)

	pool := migratedPool(t)
	service := testService(t, pool)
	created, err := service.CreateUser(t.Context(), CreateParams{
		Username: "hermione", Email: "hermione@example.com", Password: "expecto-patronum",
		FirstName: "Hermione", LastName: "Granger",
	})
	require.NoError(t, err)
	claims := &jwtutils.AccessClaims{Username: "hermione", IsAdmin: false}

	err = service.UpdateProfilePicture(t.Context(), created.ID, claims, []byte("x"))
	assert.ErrorIs(t, err, ErrPicturesUnavailable)
	err = service.ResetProfilePicture(t.Context(), created.ID, claims)
	assert.ErrorIs(t, err, ErrPicturesUnavailable)
}
