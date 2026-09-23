package static_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/riipandi/tango/internal/static"
)

// serve runs one request through the handler over dir.
func serve(t *testing.T, dir, path string) *httptest.ResponseRecorder {
	t.Helper()

	rec := httptest.NewRecorder()
	static.Handler(dir).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestAServedFileComesBack(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "avatar.png"), []byte("bytes"), 0o600))

	res := serve(t, dir, "/static/avatar.png")
	assert.Equal(t, http.StatusOK, res.Code)
	assert.Equal(t, "bytes", res.Body.String())
}

func TestANestedFileComesBack(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "users", "7"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "users", "7", "photo.jpg"), []byte("photo"), 0o600))

	res := serve(t, dir, "/static/users/7/photo.jpg")
	assert.Equal(t, http.StatusOK, res.Code)
	assert.Equal(t, "photo", res.Body.String())
}

// TestAMissingUploadIsNotFound is the boundary against the SPA: the SPA
// answers an unknown path with index.html, which for an image is a broken
// <img> rather than a visible error. A missing upload must be a plain 404.
func TestAMissingUploadIsNotFound(t *testing.T) {
	res := serve(t, t.TempDir(), "/static/nope.png")
	assert.Equal(t, http.StatusNotFound, res.Code)
	assert.NotContains(t, res.Body.String(), "<html")
}

// TestAnEmptyDataDirectoryIsNotAnError covers a run that has never stored an
// upload: the route exists and answers 404, rather than failing the run.
func TestAnEmptyDataDirectoryIsNotAnError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "never-created")
	res := serve(t, missing, "/static/x.png")

	assert.Equal(t, http.StatusNotFound, res.Code)
}

// TestARequestCannotLeaveTheUploadsDirectory is the security rule: a request
// path that escapes the served directory must be refused, not resolved.
func TestARequestCannotLeaveTheUploadsDirectory(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "uploads")
	require.NoError(t, os.MkdirAll(dir, 0o755))

	// A file beside the uploads directory: readable only by escaping it.
	require.NoError(t, os.WriteFile(filepath.Join(parent, "secret.txt"), []byte("secret"), 0o600))

	for _, path := range []string{
		"/static/../secret.txt",
		"/static/a/../../secret.txt",
	} {
		t.Run(path, func(t *testing.T) {
			res := serve(t, dir, path)
			assert.NotEqual(t, http.StatusOK, res.Code, "%s must not be served", path)
			assert.NotEqual(t, "secret", res.Body.String())
		})
	}
}

// TestAServedFileIsCacheableAndNotSniffed pins the two headers: the content is
// named by its path, so a client may keep it, and a browser must not guess a
// type from the bytes.
func TestAServedFileIsCacheableAndNotSniffed(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "f.bin"), []byte("x"), 0o600))

	res := serve(t, dir, "/static/f.bin")
	assert.Contains(t, res.Header().Get("Cache-Control"), "public")
	assert.Contains(t, res.Header().Get("Cache-Control"), "immutable")
	assert.Equal(t, "nosniff", res.Header().Get("X-Content-Type-Options"))
}

// TestDirIsUnderTheDataDirectory keeps the served tree and the chunk store
// apart: an upload is written whole under uploads/, never among the chunks.
func TestDirIsUnderTheDataDirectory(t *testing.T) {
	assert.Equal(t, filepath.Join("storage", "uploads"), static.Dir("storage"))
	assert.Equal(t, filepath.Join(".", "uploads"), static.Dir(""))
}
