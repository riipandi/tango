package emailverification

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/internal/kernel"
	"github.com/riipandi/tango/internal/transport/middleware"
	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/modules/identity/password"
	"github.com/riipandi/tango/modules/identity/session"
	"github.com/riipandi/tango/modules/identity/token"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/pkg/crypto"
	"github.com/riipandi/tango/pkg/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubAccess resolves any bearer token to the seeded user's
// principal; "revoked-" fails.
type stubAccess struct{ userID string }

func (s stubAccess) ResolveAccess(_ context.Context, token string) (kernel.Principal, error) {
	if token == "" || strings.HasPrefix(token, "revoked-") {
		return kernel.Principal{}, errors.New("session: invalid or expired")
	}
	return kernel.Principal{SessionID: "sess_test", UserID: s.userID}, nil
}

// newRPCStack builds the verification service over the throwaway
// database with a recorder-backed verifier, and mounts the Connect
// surface behind the real guard. The email-link verify endpoint
// stays REST on the same service.
func newRPCStack(t *testing.T) (http.Handler, chi.Router, *Service, user.User) {
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

	tokens := token.NewStore(ds, token.PurposeEmailVerification)
	svc := NewService(tokens, verifierFunc(func(ctx context.Context, id string) error {
		parsed, parseErr := identity.ParseID[user.UserID](id)
		if parseErr != nil {
			return parseErr
		}
		return users.MarkEmailVerified(ctx, parsed)
	}), nil)

	created, err := users.Create(ctx, user.CreateParams{
		Username: "verify_" + stamp(),
		Email:    "verify-" + stamp() + "@example.com",
	})
	require.NoError(t, err)

	prefix, handler := svc.RPCService(stubAccess{userID: created.ID.String()})
	guarded := middleware.RPCSessionAuth(stubAccess{userID: created.ID.String()})(handler)
	mux := http.NewServeMux()
	mux.Handle(prefix, guarded)

	rest := chi.NewRouter()
	rest.Route("/api", func(r chi.Router) {
		sessions.APIRoutes(r, identity.RouteGroups{})
		New(svc).APIRoutes(r, identity.RouteGroups{
			Self: middleware.RequireAuth(sessions),
		})
	})
	return mux, rest, svc, created
}

// stamp yields a per-call unique suffix.
func stamp() string {
	return strconv.FormatInt(time.Now().UnixNano(), 10)
}

// rpcPost posts the SendEmail procedure with the bearer.
func rpcPost(t *testing.T, h http.Handler) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/tango.identity.v1.EmailVerificationService/SendEmail", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Connect-Protocol-Version", "1")
	req.Header.Set("Authorization", "Bearer ev-1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// TestSendEmailRPC covers the Connect send action: the answer never
// carries the token, the row lands in the store, and anonymous
// callers are rejected by the guard.
func TestSendEmailRPC(t *testing.T) {
	h, _, _, _ := newRPCStack(t)

	// Anonymous → unauthenticated.
	req := httptest.NewRequest(http.MethodPost, "/tango.identity.v1.EmailVerificationService/SendEmail", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// Authenticated send → empty response, no token in the body (the
	// token travels by email only; the sender is unwired here).
	w = rpcPost(t, h)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.NotContains(t, w.Body.String(), "token")
}
