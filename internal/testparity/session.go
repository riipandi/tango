// Package testparity builds the shared endpoint-parity stack: real
// Postgres, a user with a credential, and signed-in sessions. It
// lives outside pkg/testutils because it imports the module packages
// — test binaries import it, module code never does.
package testparity

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/modules/identity/password"
	"github.com/riipandi/tango/modules/identity/session"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/pkg/crypto"
	"github.com/riipandi/tango/pkg/testutils"
	"github.com/stretchr/testify/require"
)

// SessionStack is the wired sign-in surface shared by parity tests.
type SessionStack struct {
	DB        datastore.Store
	Users     *user.PostgresStore
	Passwords *password.Service
	Sessions  *session.Service
}

// NewSessionStack wires the session surface over the shared test
// Postgres with migrations applied.
func NewSessionStack(t *testing.T) *SessionStack {
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
		crypto.NewPasswordHasher().WithAlgorithm(crypto.AlgorithmScrypt), nil)
	sessions := session.NewService(session.NewPostgresStore(ds), passwords, users, nil)

	return &SessionStack{DB: ds, Users: users, Passwords: passwords, Sessions: sessions}
}

// NewUser provisions a user with a known credential and returns it
// plus the plaintext secret.
func (s *SessionStack) NewUser(t *testing.T, name string) (user.User, string) {
	t.Helper()
	stamp := fmt.Sprintf("%d", time.Now().UnixNano())
	u, err := s.Users.Create(t.Context(), user.CreateParams{
		Username: name + "_" + stamp,
		Email:    name + "-" + stamp + "@example.com",
	})
	require.NoError(t, err)
	const secret = "s3cret-p@ss"
	require.NoError(t, s.Passwords.SetPassword(t.Context(), u.ID, secret))
	return u, secret
}

// SignIn submits the sign-in request and returns the session cookie
// value.
func (s *SessionStack) SignIn(t *testing.T, r http.Handler, username, secret string) string {
	t.Helper()
	w := testutils.Do(t, r, http.MethodPost, "/api/auth/sign-in",
		`{"identity":"`+username+`","secret":"`+secret+`"}`)
	require.Equal(t, 200, w.Code, w.Body.String())
	cookies := w.Result().Cookies()
	require.NotEmpty(t, cookies)
	return cookies[0].Value
}
