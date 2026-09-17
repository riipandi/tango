package recovery

// service_test.go drives the use case directly, without the HTTP
// transport: enumeration-safe forgot behavior, the session
// invalidation contract, and the typed error mapping.

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/modules/identity/password"
	"github.com/riipandi/tango/modules/identity/session"
	"github.com/riipandi/tango/modules/identity/token"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/pkg/crypto"
	"github.com/riipandi/tango/pkg/testutils"
)

// sessionsSpy records the session contract calls.
type sessionsSpy struct {
	revoked []string
	issued  int
}

func (s *sessionsSpy) IssueForUser(ctx context.Context, userID user.UserID, _ string, _ session.Meta) (string, error) {
	s.issued++
	return "fresh-session-" + strconv.Itoa(s.issued), nil
}

func (s *sessionsSpy) RevokeAllForUser(_ context.Context, userID user.UserID, _ string) error {
	s.revoked = append(s.revoked, userID.String())
	return nil
}

func newServiceStack(t *testing.T) (*Recovery, *sessionsSpy, *stubMail, *[]identity.AuditEvent, user.Store, user.User) {
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

	mail := &stubMail{}
	events := []identity.AuditEvent{}
	spy := &sessionsSpy{}
	svc := New(
		token.NewStore(ds, token.PurposePasswordReset),
		users,
		password.NewPostgresStore(ds),
		spy,
		crypto.NewPasswordHasher().WithAlgorithm(crypto.AlgorithmScrypt),
		recorderFunc(func(_ context.Context, e identity.AuditEvent, _ datastore.Executor) {
			events = append(events, e)
		}),
		WithMail(mail, "http://localhost:3000"),
	)

	stamp := strconv.FormatInt(time.Now().UnixNano(), 10)
	u, err := users.Create(ctx, user.CreateParams{
		Username: "recosvc_" + stamp,
		Email:    "recosvc-" + stamp + "@example.com",
	})
	require.NoError(t, err)
	require.NoError(t, passwords.SetPassword(ctx, u.ID, "old-s3cret-p@ss"))

	return svc, spy, mail, &events, users, u
}

func TestForgotPasswordServiceQueuesOneEmail(t *testing.T) {
	svc, _, mail, events, _, u := newServiceStack(t)
	ctx := t.Context()

	// Unknown and credential-less identities stay silent.
	assert.NoError(t, svc.ForgotPassword(ctx, "ghost-"+u.Email))
	assert.Empty(t, mail.messages)
	assert.Empty(t, *events)

	// A known address mints the token and queues exactly one email.
	require.NoError(t, svc.ForgotPassword(ctx, u.Email))
	require.Len(t, mail.messages, 1)
	assert.Equal(t, u.Email, mail.messages[0].To)
	assert.Contains(t, mail.messages[0].Data["ResetLink"], "/reset-password?token=")
	require.Len(t, *events, 1)
	assert.Equal(t, "password.reset_requested", (*events)[0].Action)
}

func TestForgotPasswordSkipsDisabledAccounts(t *testing.T) {
	svc, _, mail, _, users, u := newServiceStack(t)
	ctx := t.Context()

	banned := true
	_, err := users.UpdateAdmin(ctx, u.ID, user.AdminUpdateParams{Disabled: &banned})
	require.NoError(t, err)

	require.NoError(t, svc.ForgotPassword(ctx, u.Email))
	assert.Empty(t, mail.messages)
}

func TestResetPasswordServiceLifecycle(t *testing.T) {
	svc, spy, mail, events, _, u := newServiceStack(t)
	ctx := t.Context()

	require.NoError(t, svc.ForgotPassword(ctx, u.Email))
	require.Len(t, mail.messages, 1)
	raw := linkToken(t, mail.messages[0])

	// A weak replacement is a typed policy error, token unburned.
	_, _, err := svc.ResetPassword(ctx, raw, "short")
	assert.True(t, errors.Is(err, password.ErrWeakPassword), "got %v", err)

	updated, sessionToken, err := svc.ResetPassword(ctx, raw, "new-s3cret-p@ss")
	require.NoError(t, err)
	assert.Equal(t, u.ID, updated.ID)
	assert.NotEmpty(t, sessionToken)
	assert.Equal(t, []string{u.ID.String()}, spy.revoked, "every existing session dies")
	assert.Equal(t, 1, spy.issued)

	require.Len(t, *events, 2)
	assert.Equal(t, "password.reset_completed", (*events)[1].Action)

	// The token burned: the replay is the generic not-found.
	_, _, err = svc.ResetPassword(ctx, raw, "another-p@ss")
	assert.ErrorIs(t, err, ErrNotFound)

	// An unknown token maps to the same not-found.
	_, _, err = svc.ResetPassword(ctx, "no-such-token", "another-p@ss")
	assert.ErrorIs(t, err, ErrNotFound)
}
