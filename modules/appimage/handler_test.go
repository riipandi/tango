package appimage

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/riipandi/tango/internal/storage"
)

// upload builds a multipart body with one file field.
func upload(t *testing.T, field, filename, content string) (body *bytes.Buffer, contentType string) {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile(field, filename)
	require.NoError(t, err)
	_, err = io.Copy(part, strings.NewReader(content))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	return &buf, writer.FormDataContentType()
}

// TestGuardFailsClosed runs before any WithGuard call: mutations must
// be denied while the package guard is unset.
func TestGuardFailsClosed(t *testing.T) {
	svc := NewService(mustStorage(t), nil)
	r := chi.NewRouter()
	r.Route("/api", svc.APIRoutes)

	body, contentType := upload(t, "file", "favicon.png", "data")
	req := httptest.NewRequest(http.MethodPut, "/api/application-images/favicon", body)
	req.Header.Set("Content-Type", contentType)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TestHandlerImageLifecycle mounts the routes with a pass-through
// guard and drives serve/update/delete.
func TestHandlerImageLifecycle(t *testing.T) {
	originalGuard := guard
	t.Cleanup(func() { guard = originalGuard })

	svc := NewService(mustStorage(t), nil).WithGuard(func(next http.Handler) http.Handler {
		return next // pass-through: tests exercise the handler body
	})
	r := chi.NewRouter()
	r.Route("/api", svc.APIRoutes)

	// Serve before upload: 404.
	get := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	assert.Equal(t, http.StatusNotFound, get("/api/application-images/favicon").Code)

	// Upload: 204, then serve with cache headers.
	body, contentType := upload(t, "file", "favicon.png", "png-data")
	put := httptest.NewRequest(http.MethodPut, "/api/application-images/favicon", body)
	put.Header.Set("Content-Type", contentType)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, put)
	require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())

	w = get("/api/application-images/favicon")
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "image/png", w.Header().Get("Content-Type"))
	assert.Equal(t, CacheControl(false), w.Header().Get("Cache-Control"))
	data, err := io.ReadAll(w.Body)
	require.NoError(t, err)
	assert.Equal(t, "png-data", string(data))

	// skipCache bypasses the cache policy.
	w = get("/api/application-images/favicon?skipCache=1")
	assert.Equal(t, CacheControl(true), w.Header().Get("Cache-Control"))

	// Logo light/dark variants route to separate objects.
	for _, path := range []string{"/api/application-images/logo", "/api/application-images/logo?light=true"} {
		logoBody, logoType := upload(t, "file", "logo.png", "logo-"+path)
		req := httptest.NewRequest(http.MethodPut, path, logoBody)
		req.Header.Set("Content-Type", logoType)
		w = httptest.NewRecorder()
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusNoContent, w.Code, path)

		w = get(path)
		require.Equal(t, http.StatusOK, w.Code, path)
		assert.Equal(t, "image/png", w.Header().Get("Content-Type"), path)
	}
	assert.Equal(t, "appimage", (&Service{}).Name())

	// Email logo and background go through the same machinery.
	for _, path := range []string{"/api/application-images/email", "/api/application-images/background"} {
		picBody, picType := upload(t, "file", "picture.png", "data-"+path)
		req := httptest.NewRequest(http.MethodPut, path, picBody)
		req.Header.Set("Content-Type", picType)
		w = httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNoContent, w.Code, path)

		req = httptest.NewRequest(http.MethodDelete, path, nil)
		w = httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNoContent, w.Code, path)
	}

	// Bad MIME type: 422. Missing field: 400. Oversize: 413.
	body, contentType = upload(t, "file", "background.txt", "no")
	req := httptest.NewRequest(http.MethodPut, "/api/application-images/background", body)
	req.Header.Set("Content-Type", contentType)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)

	req = httptest.NewRequest(http.MethodPut, "/api/application-images/background",
		strings.NewReader("not multipart"))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	huge := strings.Repeat("x", maxUploadSize+1)
	body, contentType = upload(t, "file", "background.png", huge)
	req = httptest.NewRequest(http.MethodPut, "/api/application-images/background", body)
	req.Header.Set("Content-Type", contentType)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code)

	// Delete tombstones: a repeat delete reports 404.
	req = httptest.NewRequest(http.MethodDelete, "/api/application-images/logo?light=true", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNoContent, w.Code)
	req = httptest.NewRequest(http.MethodDelete, "/api/application-images/logo?light=true", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestCacheControl(t *testing.T) {
	assert.Equal(t, "no-cache", CacheControl(true))
	assert.Equal(t, "public, max-age=900, stale-while-revalidate=86400", CacheControl(false))
}

func mustStorage(t *testing.T) storage.Store {
	t.Helper()
	store, err := storage.NewFilesystemStorage(t.TempDir())
	require.NoError(t, err)
	return store
}
