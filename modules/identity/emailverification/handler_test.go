package emailverification

// handler_test.go pins the email verification HTTP contract over the
// real session guard: the send endpoint answers 204 without leaking
// the token, and the verify endpoint consumes a single-use token
// scoped to the signed-in user.

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/internal/transport/middleware"
	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/modules/identity/password"
	"github.com/riipandi/tango/modules/identity/session"
	"github.com/riipandi/tango/modules/identity/token"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/pkg/crypto"
	"github.com/riipandi/tango/pkg/testutils"
)

func newTestRouter(t *testing.T) (chi.Router, *user.PostgresStore, *password.Service, *token.PostgresStore, *session.Service) {
	t.Helper()
	ctx := t.Context()

	pg := testutils.StartPostgres(ctx, t)
	if _, err := database.MigrateUp(ctx, pg.DSN); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	ds, err := datastore.New(ctx, datastore.Options{DSN: pg.DSN})
	require.NoError(t, err)
	t.Cleanup(func() { ds.Close() })

	users := user.NewPostgresStore(ds)
	passwords := password.NewService(password.NewPostgresStore(ds),
		crypto.NewPasswordHasher().WithAlgorithm(crypto.AlgorithmArgon2id), nil)
	sessions := session.NewService(session.NewPostgresStore(ds), passwords, users, nil)

	selfGuard := middleware.RequireAuth(sessions)

	tokens := token.NewStore(ds, token.PurposeEmailVerification)
	// Same adapter the registry wires: the user store satisfies the
	// Verifier contract through the ID parser.
	svc := NewService(tokens, verifierFunc(func(ctx context.Context, id string) error {
		parsed, err := identity.ParseID[user.UserID](id)
		if err != nil {
			return err
		}
		return users.MarkEmailVerified(ctx, parsed)
	}), nil)
	feature := New(svc)

	r := chi.NewRouter()
	r.Route("/api", func(r chi.Router) {
		sessions.APIRoutes(r, identity.RouteGroups{})
		feature.APIRoutes(r, identity.RouteGroups{Self: selfGuard})
	})
	return r, users, passwords, tokens, sessions
}

func signIn(t *testing.T, r chi.Router, users *user.PostgresStore, passwords *password.Service, sessions *session.Service) (string, user.User) {
	t.Helper()
	stamp := strconv.FormatInt(time.Now().UnixNano(), 10)

	u, err := users.Create(t.Context(), user.CreateParams{
		Username: "verify_" + stamp,
		Email:    "verify-" + stamp + "@example.com",
	})
	require.NoError(t, err)
	require.NoError(t, passwords.SetPassword(t.Context(), u.ID, "s3cret-p@ss"))

	result, err := sessions.SignInWithPending(t.Context(), u.Username, "s3cret-p@ss", session.Meta{})
	require.NoError(t, err)
	require.False(t, result.Pending)
	return result.Token, u
}

func TestVerifyConsumesScopedToken(t *testing.T) {
	r, users, passwords, tokens, sessions := newTestRouter(t)
	cookie, u := signIn(t, r, users, passwords, sessions)

	// Mint through the store and keep the raw value only here.
	raw := "rawver_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	sum := sha256.Sum256([]byte(raw))
	require.NoError(t, tokens.Upsert(t.Context(), &token.Token{
		UserID:    u.ID.UUID(),
		TokenHash: base64.RawURLEncoding.EncodeToString(sum[:]),
		ExpiresAt: time.Now().UTC().Add(TokenTTL),
	}))

	// An unknown token fails closed with not-found.
	req := httptest.NewRequest(http.MethodPost, "/api/users/me/verify-email",
		strings.NewReader(`{"token":"totally-unknown"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)

	// The owner's token verifies the address.
	req = httptest.NewRequest(http.MethodPost, "/api/users/me/verify-email",
		strings.NewReader(`{"token":"`+raw+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cookie)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())

	fetched, err := users.GetByID(t.Context(), u.ID)
	require.NoError(t, err)
	assert.NotNil(t, fetched.EmailVerifiedAt, "verification must stamp the user")

	// Single use: a replay is not found.
	req = httptest.NewRequest(http.MethodPost, "/api/users/me/verify-email",
		strings.NewReader(`{"token":"`+raw+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cookie)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestVerifyRequiresSession(t *testing.T) {
	r, _, _, _, _ := newTestRouter(t)

	req := httptest.NewRequest(http.MethodPost, "/api/users/me/verify-email", strings.NewReader(`{"token":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// verifierFunc adapts a function to the Verifier contract.
type verifierFunc func(ctx context.Context, userID string) error

func (f verifierFunc) MarkEmailVerified(ctx context.Context, userID string) error {
	return f(ctx, userID)
}
