package user

// profile_picture_test.go drives the phase 9C picture surface over a
// real Postgres + FS blob store: admin upload, the bare-bytes read
// with the bundled-default fallback, reset, and rejection of
// non-image uploads.

import (
	"context"
	"io"
	"mime/multipart"
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
	"github.com/riipandi/tango/pkg/testutils"
)

// tinyPNG is a valid 1x1 PNG.
var tinyPNG = []byte{
	0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D,
	0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4, 0x89, 0x00, 0x00, 0x00,
	0x0A, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9C, 0x63, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00, 0x00, 0x00, 0x00, 0x49,
	0x45, 0x4E, 0x44, 0xAE, 0x42, 0x60, 0x82,
}

// fakeSelf resolves one fixed session token to a principal.
type fakeSelf struct{ principal middleware.Principal }

func (f *fakeSelf) ResolveSession(_ context.Context, token string) (middleware.Principal, error) {
	if token == "self-token" {
		return f.principal, nil
	}
	return middleware.Principal{}, ErrNotFound
}

// upload builds a multipart body with one file part.
func upload(t *testing.T, field, filename string, data []byte) (contentType string, body io.Reader) {
	t.Helper()
	var buf strings.Builder
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile(field, filename)
	require.NoError(t, err)
	_, err = part.Write(data)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	return w.FormDataContentType(), strings.NewReader(buf.String())
}

// nopReadCloser adapts a reader (test-only shim for the provider).
func nopReadCloser(r io.Reader) io.ReadCloser { return io.NopCloser(r) }

// newPictureRouter mounts the user service with a real FS blob store
// (routes open — no guards — matching the other handler tests).
func newPictureRouter(t *testing.T, withDefault bool) (chi.Router, *PostgresStore, UserID) {
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

	svc := NewService(store, nil,
		WithSelfAuth(&fakeSelf{principal: middleware.Principal{UserID: created.ID.String()}}, "tango_session"),
		WithImages(blobs, defaults),
	)

	r := chi.NewRouter()
	r.Route("/api", svc.APIRoutes)
	return r, store, created.ID
}

func TestProfilePictureSurface(t *testing.T) {
	r, store, userID := newPictureRouter(t, false)

	// No custom picture and no default → 404.
	w := do(r, http.MethodGet, "/api/users/"+userID.String()+"/profile-picture.png", "")
	require.Equal(t, http.StatusNotFound, w.Code)

	// Self upload via the session cookie → 204 (the /users/me mount).
	contentType, body := upload(t, "file", "avatar.png", tinyPNG)
	req := httptest.NewRequest(http.MethodPut, "/api/users/me/profile-picture", body)
	req.Header.Set("Content-Type", contentType)
	req.AddCookie(&http.Cookie{Name: "tango_session", Value: "self-token"})
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())

	// The column lands and the .png route serves the bytes bare.
	stored, err := store.GetByID(t.Context(), userID)
	require.NoError(t, err)
	require.NotNil(t, stored.ProfilePicturePath)

	w = do(r, http.MethodGet, "/api/users/"+userID.String()+"/profile-picture.png", "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "image/png", w.Header().Get("Content-Type"))
	assert.NotEqual(t, "default-picture", w.Body.String())

	// Non-image upload → 422.
	contentType, body = upload(t, "file", "notes.txt", []byte("nope"))
	req = httptest.NewRequest(http.MethodPut, "/api/users/"+userID.String()+"/profile-picture", body)
	req.Header.Set("Content-Type", contentType)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)

	// Reset → 204, then the read is a 404 again.
	w = do(r, http.MethodDelete, "/api/users/"+userID.String()+"/profile-picture", "")
	require.Equal(t, http.StatusNoContent, w.Code)

	w = do(r, http.MethodGet, "/api/users/"+userID.String()+"/profile-picture.png", "")
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestProfilePictureDefaultFallback(t *testing.T) {
	r, _, userID := newPictureRouter(t, true)

	// No custom picture → the bundled default answers.
	w := do(r, http.MethodGet, "/api/users/"+userID.String()+"/profile-picture.png", "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "default-picture", w.Body.String())
}
