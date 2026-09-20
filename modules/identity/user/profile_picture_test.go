package user

// profile_picture_test.go covers the picture surface after the
// ConnectRPC cutover: mutations run through the service (the RPC
// handlers call it directly), the bare .png read stays REST.

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/internal/storage"
	"github.com/riipandi/tango/internal/transport/middleware"
	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/pkg/testutils"
)

// identityRouteGroups keeps the route mount signature; no groups are
// needed for the retained bare-bytes read.
func identityRouteGroups() identity.RouteGroups {
	return identity.RouteGroups{}
}

// tinyPNG is a valid 1x1 PNG.
var tinyPNG = []byte{
	0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D,
	0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4, 0x89, 0x00, 0x00, 0x00,
	0x0A, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9C, 0x63, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00, 0x00, 0x00, 0x00, 0x49,
	0x45, 0x4E, 0x44, 0xAE, 0x42, 0x60, 0x82,
}

// nopReadCloser adapts a reader (test-only shim for the provider).
func nopReadCloser(r io.Reader) io.ReadCloser { return io.NopCloser(r) }

// newPictureStack builds the user service over a real FS blob store
// and a throwaway database; the router mounts only the retained
// bare-bytes read.
func newPictureStack(t *testing.T, withDefault bool) (chi.Router, *Service, *PostgresStore, UserID) {
	t.Helper()
	ctx := t.Context()

	pg := testutils.StartPostgres(ctx, t)
	if _, err := database.MigrateUp(ctx, pg.DSN); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	ds, err := datastore.New(ctx, datastore.Options{DSN: pg.DSN})
	require.NoError(t, err)
	t.Cleanup(func() { ds.Close() })

	blobs, err := storage.New(config.StorageConfig{DataDir: t.TempDir()})
	require.NoError(t, err)

	store := NewPostgresStore(ds)
	created, err := store.Create(ctx, CreateParams{
		Username: "pic_" + uniqueStamp(),
		Email:    "pic-" + uniqueStamp() + "@example.com",
	})
	require.NoError(t, err)

	defaults := DefaultPictureFunc(func(context.Context) (io.ReadCloser, int64, string, bool) {
		return nil, 0, "", false
	})
	if withDefault {
		defaults = DefaultPictureFunc(func(context.Context) (io.ReadCloser, int64, string, bool) {
			return nopReadCloser(strings.NewReader("default-picture")), int64(len("default-picture")), "image/png", true
		})
	}

	svc := NewService(store, nil, WithImages(blobs, defaults))
	r := chi.NewRouter()
	r.Route("/api", func(r chi.Router) {
		svc.APIRoutes(r, identityRouteGroups())
	})
	return r, svc, store, created.ID
}

// rpcCaller injects the principal the way the RPC guard would; the
// user id may point at another account for the admin paths.
func rpcCaller(ctx context.Context, userID string) context.Context {
	return middleware.WithPrincipal(ctx, middleware.Principal{UserID: userID, IsAdmin: true})
}

func TestProfilePictureSurface(t *testing.T) {
	r, svc, store, userID := newPictureStack(t, false)

	// No custom picture and no default → 404.
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/users/"+userID.String()+"/profile-picture.png", nil))
	require.Equal(t, http.StatusNotFound, w.Code)

	// The Connect surface stores the caller's own picture from raw
	// bytes.
	pictureUser, err := svc.savePicture(rpcCaller(t.Context(), userID.String()), userID, tinyPNG)
	require.NoError(t, err)
	require.NotNil(t, pictureUser.ProfilePicturePath)

	// The retained route serves the stored bytes.
	stored, err := store.GetByID(t.Context(), userID)
	require.NoError(t, err)
	require.NotNil(t, stored.ProfilePicturePath)

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/users/"+userID.String()+"/profile-picture.png", nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "image/png", w.Header().Get("Content-Type"))
	assert.NotEqual(t, "default-picture", w.Body.String())

	// Non-image bytes → invalid argument.
	_, err = svc.savePicture(rpcCaller(t.Context(), userID.String()), userID, []byte("nope"))
	assert.Error(t, err)

	// Clearing drops the picture; the read is a 404 again.
	_, err = svc.clearPicture(rpcCaller(t.Context(), userID.String()), userID)
	require.NoError(t, err)

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/users/"+userID.String()+"/profile-picture.png", nil))
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestProfilePictureDefaultFallback(t *testing.T) {
	r, _, _, userID := newPictureStack(t, true)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/users/"+userID.String()+"/profile-picture.png", nil))
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "default-picture", w.Body.String())
}
