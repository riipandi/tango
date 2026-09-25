package user

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/riipandi/tango/pkg/jwtutils"
	"github.com/riipandi/tango/pkg/testutils"
)

// TestThePictureReadServesTheRESTRoute runs the one plain HTTP route the
// feature claims: the default answers by redirect, a stored picture answers
// its bytes, and an unknown identifier answers the envelope's not-found.
func TestThePictureReadServesTheRESTRoute(t *testing.T) {
	testutils.SkipWithoutDocker(t)

	pool := migratedPool(t)
	service := testPictureService(t, pool)
	created, err := service.CreateUser(t.Context(), CreateParams{
		Username: "ada", Email: "ada@example.com", Password: "correct horse",
		FirstName: "Ada", LastName: "Lovelace",
	})
	require.NoError(t, err)

	router := chi.NewRouter()
	NewModule(service).Mount(router)

	// An account without a picture answers the bundled default's URL — a
	// relative location, so the client's own host serves it.
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/users/"+created.ID+"/profile-picture.png", nil))
	require.Equal(t, http.StatusFound, rec.Code)
	assert.Equal(t, DefaultPicturePath, rec.Header().Get("Location"))

	// An update lands the bytes the next read carries.
	claims := &jwtutils.AccessClaims{Username: "ada", IsAdmin: false}
	picture := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 1, 2}
	require.NoError(t, service.UpdateProfilePicture(t.Context(), created.ID, claims, picture))

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/users/"+created.ID+"/profile-picture.png", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "image/png", rec.Header().Get("Content-Type"))
	assert.Equal(t, picture, rec.Body.Bytes())

	// An unknown identifier answers the REST envelope's not-found.
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/api/users/00000000-0000-0000-0000-000000000000/profile-picture.png", nil))
	require.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), "user not found")
}
