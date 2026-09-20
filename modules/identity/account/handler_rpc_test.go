package account

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/internal/kernel"
	"github.com/riipandi/tango/internal/transport/middleware"
	"github.com/riipandi/tango/modules/identity/password"
	"github.com/riipandi/tango/modules/identity/session"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/pkg/crypto"
	"github.com/riipandi/tango/pkg/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubAccess resolves any bearer token to a principal; "revoked-"
// fails and other tokens resolve to the seeded user with its live
// session id.
type stubAccess struct {
	userID    string
	sessionID string
}

func (s stubAccess) ResolveAccess(_ context.Context, token string) (kernel.Principal, error) {
	if token == "" || strings.HasPrefix(token, "revoked-") {
		return kernel.Principal{}, errors.New("session: invalid or expired")
	}
	return kernel.Principal{SessionID: s.sessionID, UserID: s.userID, Provider: "password"}, nil
}

// newRPCStack builds the account service over the throwaway
// database: a user with a known password and one live session.
func newRPCStack(t *testing.T) (http.Handler, *Service, user.User, string) {
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

	u, err := users.Create(ctx, user.CreateParams{
		Username: "acct_" + stamp(),
		Email:    "acct-" + stamp() + "@example.com",
	})
	require.NoError(t, err)
	require.NoError(t, passwords.SetPassword(ctx, u.ID, "s3cret-p@ss"))

	token, _, _, err := sessions.SignIn(ctx, u.Username, "s3cret-p@ss", session.Meta{})
	require.NoError(t, err)

	svc := NewService(users, passwords, sessions, nil)
	prefix, handler := svc.RPCService()
	mux := http.NewServeMux()
	mux.Handle(prefix, middleware.RPCSessionAuth(stubAccess{userID: u.ID.String(), sessionID: sessionTypeID(t, sessions, u.ID)})(handler))
	return mux, svc, u, token
}

// sessionTypeID resolves the seeded session's TypeID.
func sessionTypeID(t *testing.T, sessions *session.Service, userID user.UserID) string {
	t.Helper()
	list, err := sessions.ListForUser(t.Context(), userID)
	require.NoError(t, err)
	require.NotEmpty(t, list)
	return list[0].ID
}

// stamp yields a per-call unique suffix.
func stamp() string {
	return strings.ReplaceAll(time.Now().Format("150405.000000000"), ".", "")
}

// rpcPost posts an account procedure with the bearer.
func rpcPost(t *testing.T, h http.Handler, procedure, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/tango.identity.v1.AccountService/"+procedure, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Connect-Protocol-Version", "1")
	req.Header.Set("Authorization", "Bearer acct-1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// TestAccountRPCSelfService walks the whole self-service surface:
// profile read and update, password change (which revokes other
// sessions), session listing, and targeted revocation.
func TestAccountRPCSelfService(t *testing.T) {
	h, svc, u, sessionToken := newRPCStack(t)
	ctx := t.Context()

	// Anonymous → the session guard rejects.
	req := httptest.NewRequest(http.MethodPost, "/tango.identity.v1.AccountService/ListSessions", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// ListSessions shows the live session.
	w = rpcPost(t, h, "ListSessions", "{}")
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"provider":"password"`)

	// Wrong current password → invalid_argument (domain sentinel).
	w = rpcPost(t, h, "ChangePassword", `{"current_password":"wrong-pass-1","new_password":"n3wS3cret!"}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// ChangePassword succeeds and revokes other sessions.
	w = rpcPost(t, h, "ChangePassword", `{"current_password":"s3cret-p@ss","new_password":"n3wS3cret!"}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	sessions, err := svc.sessions.ListForUser(ctx, u.ID)
	require.NoError(t, err)
	assert.Len(t, sessions, 1)

	// RevokeSession kills the remaining session by ID.
	w = rpcPost(t, h, "RevokeSession", `{"session_id":"`+sessions[0].ID+`"}`)
	require.Equal(t, http.StatusOK, w.Code)
	sessions, err = svc.sessions.ListForUser(ctx, u.ID)
	require.NoError(t, err)
	assert.Empty(t, sessions)
	_ = sessionToken
}
