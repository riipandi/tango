package onetimeaccess

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/internal/kernel"
	"github.com/riipandi/tango/modules/identity/password"
	"github.com/riipandi/tango/modules/identity/session"
	"github.com/riipandi/tango/modules/identity/token"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/pkg/crypto"
	"github.com/riipandi/tango/pkg/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubAccess resolves any bearer token to a principal; "revoked-"
// fails and other tokens are admin.
type stubAccess struct{ userID string }

func (s stubAccess) ResolveAccess(_ context.Context, token string) (kernel.Principal, error) {
	if token == "" || strings.HasPrefix(token, "revoked-") {
		return kernel.Principal{}, errors.New("session: invalid or expired")
	}
	return kernel.Principal{SessionID: "sess_test", UserID: s.userID, IsAdmin: true}, nil
}

// newRPCStack builds the one-time access service over the throwaway
// database and mounts the Connect surface with the stub
// authenticator; the retained email-link exchange stays REST.
func newRPCStack(t *testing.T) (http.Handler, *Service, user.Store) {
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

	svc := NewService(token.NewStore(ds, token.PurposeOneTimeAccess), users, sessions, nil)
	prefix, handler := svc.RPCService(stubAccess{})
	mux := http.NewServeMux()
	mux.Handle(prefix, handler)
	return mux, svc, users
}

// stamp yields a per-call unique suffix.
func stamp() string {
	return strconv.FormatInt(time.Now().UnixNano(), 10)
}

// rpcPost posts a one-time access procedure with an optional bearer.
func rpcPost(t *testing.T, h http.Handler, procedure, bearer, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/tango.identity.v1.OneTimeAccessService/"+procedure, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Connect-Protocol-Version", "1")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// TestOneTimeAccessRPCBranches pins the mixed visibility contract:
// the anonymous request answers the enumeration-safe empty response,
// admin procedures deny anonymous callers, and the admin mint
// returns the raw token exactly once.
func TestOneTimeAccessRPCBranches(t *testing.T) {
	h, svc, users := newRPCStack(t)

	// Anonymous request → the empty response document (proto JSON of
	// the empty message), never an enumeration hint.
	w := rpcPost(t, h, "RequestEmail", "", `{"identity":"victim@example.com"}`)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "{}", strings.TrimSpace(w.Body.String()))

	// Empty identity → invalid_argument (the old contract only
	// enforced presence, never format).
	w = rpcPost(t, h, "RequestEmail", "", `{"identity":""}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Admin procedures deny anonymous callers.
	target, err := users.Create(t.Context(), user.CreateParams{
		Username: "member_" + stamp(),
		Email:    "member-" + stamp() + "@example.com",
	})
	require.NoError(t, err)

	w = rpcPost(t, h, "AdminIssueToken", "", `{"user_id":"`+target.ID.String()+`"}`)
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// Admin mint → show-once token.
	w = rpcPost(t, h, "AdminIssueToken", "admin-1", `{"user_id":"`+target.ID.String()+`"}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"token":"`)

	// Admin send-email → empty response.
	w = rpcPost(t, h, "AdminSendEmail", "admin-1", `{"user_id":"`+target.ID.String()+`"}`)
	assert.Equal(t, http.StatusOK, w.Code)

	// Unknown TypeID → not_found.
	w = rpcPost(t, h, "AdminIssueToken", "admin-1", `{"user_id":"user_00000000000000000000000000"}`)
	assert.Equal(t, http.StatusNotFound, w.Code)
	_ = svc
}
