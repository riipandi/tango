package devicelogin

import (
	"context"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/internal/transport/middleware"
	"github.com/riipandi/tango/modules/identity/session"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/pkg/testutils"
)

// testStack builds the feature over a real Postgres container.
func testStack(t *testing.T) (*Service, *session.Service, user.Store) {
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
	passwords := newNopPasswordVerifier()
	sessions := session.NewService(session.NewPostgresStore(ds), passwords, users, nil)

	return NewService(NewPostgresStore(ds), sessions, users, nil,
		WithCookie("tango_session", false)), sessions, users
}

// newNopPasswordVerifier satisfies the session Verifier contract.
func newNopPasswordVerifier() session.Verifier {
	return nil // SignIn isn't exercised here; IssueForUser skips it
}

// TestCreateApproveExchange drives the full happy path: create
// (anonymous) → approve (session) → exchange (device token).
func TestCreateApproveExchange(t *testing.T) {
	service, _, users := testStack(t)
	ctx := t.Context()

	created, deviceToken, err := service.Create(ctx, "https://sso.test", "10.0.0.1", "test-agent")
	require.NoError(t, err)
	require.Equal(t, PollingInterval, created.Interval)
	require.True(t, created.ExpiresAt.After(time.Now().UTC()))

	// Approving user + session cookie wiring.
	u, err := users.Create(ctx, user.CreateParams{
		Username: "approver_" + strconv.FormatInt(time.Now().UnixNano(), 36),
		Email:    "approver-" + strconv.FormatInt(time.Now().UnixNano(), 36) + "@example.com",
		IsAdmin:  true,
	})
	require.NoError(t, err)

	principal := middleware.Principal{UserID: u.ID.String(), IsAdmin: true}
	require.NoError(t, service.Decide(ctx, created.UserCode, "approve", principal))

	// The approve must be idempotent-safe: a second decision on the
	// consumed request fails.
	require.ErrorIs(t, service.Decide(ctx, created.UserCode, "deny", principal), ErrInvalidRequest)

	// Long-poll exchange returns the user + session token.
	exCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	exchanged, token, err := service.Exchange(exCtx, created.ID, deviceToken)
	require.NoError(t, err)
	require.NotEmpty(t, token)
	assert.Equal(t, u.ID.String(), exchanged.ID.String(), "")
}

// Exchange again with the SAME consumed request must fail.
func TestExchangeConsumedOnce(t *testing.T) {
	service, _, users := testStack(t)
	ctx := t.Context()

	created, deviceToken, err := service.Create(ctx, "https://sso.test", "10.0.0.1", "test-agent")
	require.NoError(t, err)

	u, err := users.Create(ctx, user.CreateParams{
		Username: "consumed_" + strconv.FormatInt(time.Now().UnixNano(), 36),
		Email:    "consumed-" + strconv.FormatInt(time.Now().UnixNano(), 36) + "@example.com",
	})
	require.NoError(t, err)
	require.NoError(t, service.Decide(ctx, created.UserCode, "approve",
		middleware.Principal{UserID: u.ID.String()}))

	_, _, err = service.Exchange(ctx, created.ID, deviceToken)
	require.NoError(t, err)

	// Second exchange: the request was consumed by the first one.
	_ = httptest.NewRequest("POST", "/x", nil)
	_, _, err = service.Exchange(ctx, created.ID, deviceToken)
	require.ErrorIs(t, err, ErrInvalidRequest)
}
