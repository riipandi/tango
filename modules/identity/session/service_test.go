package session

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/huandu/go-sqlbuilder"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/internal/audit"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/pkg/crypto"
	"github.com/riipandi/tango/pkg/jwtutils"
	"github.com/riipandi/tango/pkg/testutils"
	"github.com/riipandi/tango/pkg/userid"

	"go.jetify.com/typeid"
	"uuid"
)

// migratedPool opens a database the migrations have built, so the session
// table exists.
func migratedPool(t *testing.T) *datastore.Postgres {
	t.Helper()

	dsn := testutils.StartPostgres(t.Context(), t).NewDatabase(t)

	migrationDB, err := datastore.OpenMigrationDB(t.Context(), datastore.PostgresOptions{DSN: dsn})
	require.NoError(t, err)
	migrator, err := database.NewMigrator(t.Context(), migrationDB, database.MigratorOptions{})
	require.NoError(t, err)
	_, err = migrator.Up(t.Context())
	require.NoError(t, err)
	require.NoError(t, migrationDB.Close())

	pool, err := datastore.NewPostgres(t.Context(), datastore.PostgresOptions{
		DSN:             dsn,
		ApplicationName: "session_test",
	})
	require.NoError(t, err)
	t.Cleanup(func() { pool.Shutdown(t.Context()) })
	return pool
}

// fakeIssuer is the signing half a test controls: the token is a constant, so
// a renewal's answer is assertable, and the windows are short enough that the
// clock a test moves puts a session past them.
type fakeIssuer struct{ lifetime time.Duration }

func (f *fakeIssuer) SignSessionToken(_ context.Context, subject string, _ jwtutils.AccessClaims, _ SessionID, _ time.Time) (string, error) {
	return "fake-token-for-" + subject, nil
}

func (f *fakeIssuer) SessionLifetime(bool) time.Duration { return f.lifetime }

func (f *fakeIssuer) AccessTokenTTL() time.Duration { return 15 * time.Minute }

// testService builds the service with a recorder that writes for real, and
// answers the clock knob a test moves to put a session past its window
// without writing an expiry the database's own check refuses.
// wireOf renders an account's row identifier in the wire form the claims
// carry, the shape the service procedures take.
func wireOf(t *testing.T, raw uuid.UUID) string {
	t.Helper()
	return userid.Wire(raw)
}

func testService(t *testing.T, pool *datastore.Postgres) (*Service, *time.Time) {
	t.Helper()

	users := user.NewService(pool, nil, nil, nil)
	now := time.Now()
	service := NewService(pool, &fakeIssuer{lifetime: 14 * 24 * time.Hour}, users,
		audit.NewRecorder(slog.New(slog.NewTextHandler(os.Stderr, nil))), slog.New(slog.DiscardHandler))
	service.now = func() time.Time { return now }
	return service, &now
}

// jump moves the test's clock and answers the instant it now reads.
func jump(t *testing.T, now *time.Time, d time.Duration) time.Time {
	t.Helper()
	*now = now.Add(d)
	return *now
}

// seedAccount inserts an account directly and answers its identifier. A
// session belongs to an account, so one must exist before the session does.
func seedAccount(t *testing.T, pool *datastore.Postgres, username string) uuid.UUID {
	t.Helper()

	_, err := pool.Exec(t.Context(), `
		INSERT INTO public.users (username, email, first_name, last_name, display_name)
		VALUES ($1::citext, $1::text || '@example.com', 'Hermione', 'Granger', 'Hermione Granger')`,
		username)
	require.NoError(t, err)

	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select("id")
	sb.From("public.users")
	sb.Where(sb.Equal("username", username))

	query, args := sb.Build()
	var id uuid.UUID
	require.NoError(t, pool.QueryRow(t.Context(), query, args...).Scan(&id))
	return id
}

// seedSession inserts a session row the way a sign-in would, under a known
// refresh token, and answers the row's identifier and the token. The id is
// the database's default, read back and typed the way every reader of the
// table types it.
func seedSession(t *testing.T, pool *datastore.Postgres, userID uuid.UUID, name, provider string, remember bool) (SessionID, string) {
	t.Helper()

	token := "refresh-token-" + name

	ib := sqlbuilder.PostgreSQL.NewInsertBuilder()
	ib.InsertInto(SessionTable)
	ib.Cols("user_id", "provider", "token_hash", "user_agent", "remember", "created_at", "expires_at")
	ib.Values(userID, provider, crypto.HashRefreshToken(token), "test-agent/1.0", remember,
		time.Now().Add(-time.Minute), time.Now().Add(24*time.Hour))

	query, args := ib.Build()
	_, err := pool.Exec(t.Context(), query, args...)
	require.NoError(t, err)

	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select("id")
	sb.From(SessionTable)
	sb.Where(sb.Equal("token_hash", crypto.HashRefreshToken(token)))

	query, args = sb.Build()
	var rawID string
	require.NoError(t, pool.QueryRow(t.Context(), query, args...).Scan(&rawID))
	sid, err := typeid.FromUUID[SessionID](rawID)
	require.NoError(t, err)
	return sid, token
}

// auditCount reads how many records the log holds of one event about one
// session.
func auditCount(t *testing.T, pool *datastore.Postgres, event, sessionID string) int {
	t.Helper()

	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select("count(*)")
	sb.From("public.audit_logs")
	sb.Where(sb.Equal("event", event), sb.Equal("resource_id", sessionID))

	query, args := sb.Build()
	var count int
	require.NoError(t, pool.QueryRow(t.Context(), query, args...).Scan(&count))
	return count
}

func TestSignOutStampsTheRowAndTheRefreshTokenDies(t *testing.T) {
	testutils.SkipWithoutDocker(t)

	pool := migratedPool(t)
	service, _ := testService(t, pool)
	userID := seedAccount(t, pool, "hermione")
	sid, token := seedSession(t, pool, userID, "one", "password", false)

	outcome, err := service.SignOut(t.Context(), sid.String(), wireOf(t, userID))
	require.NoError(t, err)
	assert.False(t, outcome.Already)
	assert.False(t, outcome.Expired)

	// The row survives with its stamp: the view carries the instant and the
	// ender, and the refresh token the row held opens nothing from now on.
	row, err := service.repo.GetSession(t.Context(), pool, sid)
	require.NoError(t, err)
	require.NotNil(t, row.RevokedAt)
	require.NotNil(t, row.RevokedBy)
	assert.Equal(t, userID, *row.RevokedBy)
	_, err = service.repo.FindActiveByTokenHash(t.Context(), pool,
		crypto.HashRefreshToken(token), time.Now())
	assert.ErrorIs(t, err, datastore.ErrNoRows)

	// A second sign-out is the success it is: the caller's intent is the
	// state the session is in, and the answer says so — nothing was
	// written, not even the audit record the first sign-out earned.
	again, err := service.SignOut(t.Context(), sid.String(), wireOf(t, userID))
	require.NoError(t, err)
	assert.True(t, again.Already)
	assert.Equal(t, 1, auditCount(t, pool, audit.EventSignOut, sid.UUID()))
}

func TestAnEndedSessionCannotManageSessions(t *testing.T) {
	testutils.SkipWithoutDocker(t)

	pool := migratedPool(t)
	service, now := testService(t, pool)
	userID := seedAccount(t, pool, "hermione")
	live, _ := seedSession(t, pool, userID, "live", "password", false)
	dead, _ := seedSession(t, pool, userID, "dead", "password", false)

	// The stamped row: the holder signed out from this client, and the
	// surface no longer honours the credential it left behind.
	_, err := service.SignOut(t.Context(), dead.String(), wireOf(t, userID))
	require.NoError(t, err)

	_, _, err = service.ListSessions(t.Context(), dead.String(), wireOf(t, userID), 1, 10)
	assert.ErrorIs(t, err, ErrSessionEnded)
	_, err = service.SignOutOtherSessions(t.Context(), dead.String(), wireOf(t, userID))
	assert.ErrorIs(t, err, ErrSessionEnded)
	_, err = service.SignOutAllSessions(t.Context(), dead.String(), wireOf(t, userID))
	assert.ErrorIs(t, err, ErrSessionEnded)
	assert.ErrorIs(t, service.RevokeSession(t.Context(), dead.String(), wireOf(t, userID), live.String()), ErrSessionEnded)

	// A live row still manages as before, until its own window closes.
	_, _, err = service.ListSessions(t.Context(), live.String(), wireOf(t, userID), 1, 10)
	require.NoError(t, err)

	// The window closing without a stamp is the same refusal: the row is
	// still unstamped, and no procedure of the surface answers for it.
	live2, _ := seedSession(t, pool, userID, "live-2", "password", false)
	jump(t, now, 48*time.Hour)
	_, _, err = service.ListSessions(t.Context(), live2.String(), wireOf(t, userID), 1, 10)
	assert.ErrorIs(t, err, ErrSessionEnded)
	_, err = service.SignOutOtherSessions(t.Context(), live2.String(), wireOf(t, userID))
	assert.ErrorIs(t, err, ErrSessionEnded)
}

func TestSignOutOtherSessionsSweepsEveryLiveRowButTheCallerOwn(t *testing.T) {
	testutils.SkipWithoutDocker(t)

	pool := migratedPool(t)
	service, _ := testService(t, pool)
	userID := seedAccount(t, pool, "hermione")
	current, currentToken := seedSession(t, pool, userID, "current", "password", false)
	otherA, _ := seedSession(t, pool, userID, "other-a", "password", true)
	otherB, _ := seedSession(t, pool, userID, "other-b", "one_time_access", false)
	stranger := seedAccount(t, pool, "vittoria")
	strangerSID, _ := seedSession(t, pool, stranger, "stranger", "password", false)

	// A row that was stamped before the sweep is outside it: the sweep ends
	// live rows, and an ended one is not its business.
	ended, _ := seedSession(t, pool, userID, "ended", "password", false)
	_, signOutErr := service.SignOut(t.Context(), ended.String(), wireOf(t, userID))
	require.NoError(t, signOutErr)

	count, err := service.SignOutOtherSessions(t.Context(), current.String(), wireOf(t, userID))
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	// The caller's own row stays live, the swept ones do not, and the
	// account the sweep never names is untouched.
	_, _, err = service.GetSession(t.Context(), current.String())
	require.NoError(t, err)
	_, _, err = service.GetSession(t.Context(), otherA.String())
	assert.ErrorIs(t, err, ErrSessionEnded)
	_, _, err = service.GetSession(t.Context(), otherB.String())
	assert.ErrorIs(t, err, ErrSessionEnded)
	_, _, err = service.GetSession(t.Context(), strangerSID.String())
	require.NoError(t, err)

	// Each swept row carries its own record, named with the reason the
	// sweep answered.
	assert.Equal(t, 1, auditCount(t, pool, audit.EventSessionRevoked, otherA.UUID()))
	assert.Equal(t, 1, auditCount(t, pool, audit.EventSessionRevoked, otherB.UUID()))
	assert.Equal(t, 0, auditCount(t, pool, audit.EventSessionRevoked, current.UUID()))

	// A second sweep finds nothing: the rows it ended are stamped, and the
	// success is the state the account is already in.
	count, err = service.SignOutOtherSessions(t.Context(), current.String(), wireOf(t, userID))
	require.NoError(t, err)
	assert.Equal(t, 0, count)

	// The swept refresh tokens open nothing; the kept one still does.
	_, err = service.Refresh(t.Context(), "refresh-token-other-a")
	assert.ErrorIs(t, err, ErrSessionEnded)
	_, err = service.Refresh(t.Context(), currentToken)
	require.NoError(t, err)
}

func TestSignOutAllSessionsEndsTheCallerOwnRowToo(t *testing.T) {
	testutils.SkipWithoutDocker(t)

	pool := migratedPool(t)
	service, _ := testService(t, pool)
	userID := seedAccount(t, pool, "vittoria")
	current, currentToken := seedSession(t, pool, userID, "current", "password", false)
	other, _ := seedSession(t, pool, userID, "other", "password", true)

	count, err := service.SignOutAllSessions(t.Context(), current.String(), wireOf(t, userID))
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	// The caller's own row is stamped like the rest: the session it named
	// answers the ended failure from here on, and its refresh token opens
	// nothing.
	_, _, err = service.GetSession(t.Context(), current.String())
	assert.ErrorIs(t, err, ErrSessionEnded)
	_, _, err = service.GetSession(t.Context(), other.String())
	assert.ErrorIs(t, err, ErrSessionEnded)
	_, err = service.Refresh(t.Context(), currentToken)
	assert.ErrorIs(t, err, ErrSessionEnded)

	// Each stamped row carries its own record under the all scope's reason.
	assert.Equal(t, 1, auditCount(t, pool, audit.EventSessionRevoked, current.UUID()))
	assert.Equal(t, 1, auditCount(t, pool, audit.EventSessionRevoked, other.UUID()))
}

func TestSignOutOfAnExpiredSessionStampsAndSaysSo(t *testing.T) {
	testutils.SkipWithoutDocker(t)

	pool := migratedPool(t)
	service, now := testService(t, pool)
	userID := seedAccount(t, pool, "hermione")
	sid, _ := seedSession(t, pool, userID, "one", "password", false)

	// The window closed a day ago; the row was never stamped, because the
	// expiry refusal is the write that rolls back. The sign-out closes the
	// book an expiry left open, and the answer says which one happened.
	jump(t, now, 48*time.Hour)
	outcome, err := service.SignOut(t.Context(), sid.String(), wireOf(t, userID))
	require.NoError(t, err)
	assert.True(t, outcome.Expired)
	assert.False(t, outcome.Already)

	// The stamp is the write it always was, and the record says the session
	// ended under its own event.
	row, err := service.repo.GetSession(t.Context(), pool, sid)
	require.NoError(t, err)
	require.NotNil(t, row.RevokedAt)
	assert.Equal(t, 1, auditCount(t, pool, audit.EventSignOut, sid.UUID()))
}

func TestGetSessionAnswersTheLiveRowAndRefusesAnEndedOne(t *testing.T) {
	testutils.SkipWithoutDocker(t)

	pool := migratedPool(t)
	service, now := testService(t, pool)
	userID := seedAccount(t, pool, "hermione")
	sid, _ := seedSession(t, pool, userID, "one", "password", true)

	row, view, err := service.GetSession(t.Context(), sid.String())
	require.NoError(t, err)
	assert.Equal(t, sid.String(), row.ID.String())
	assert.True(t, row.Remember)
	assert.Equal(t, "hermione", view.Username)

	// A session past its window is the ended failure: the token verified,
	// the session behind it did not survive.
	jump(t, now, 15*24*time.Hour)
	_, _, err = service.GetSession(t.Context(), sid.String())
	assert.ErrorIs(t, err, ErrSessionEnded)
}

func TestRevokeSessionEndsOneOfTheAccountsAndRefusesAnOthers(t *testing.T) {
	testutils.SkipWithoutDocker(t)

	pool := migratedPool(t)
	service, _ := testService(t, pool)
	userID := seedAccount(t, pool, "hermione")
	second := seedAccount(t, pool, "ron")
	current, _ := seedSession(t, pool, userID, "current", "password", false)
	other, _ := seedSession(t, pool, userID, "other", "password", true)
	theirs, _ := seedSession(t, pool, second, "theirs", "password", false)

	require.NoError(t, service.RevokeSession(t.Context(), current.String(), wireOf(t, userID), other.String()))
	row, err := service.repo.GetSession(t.Context(), pool, other)
	require.NoError(t, err)
	require.NotNil(t, row.RevokedAt)
	assert.Equal(t, 1, auditCount(t, pool, audit.EventSessionRevoked, other.UUID()))

	// A session another account holds is the not-found failure, the same
	// as an unknown identifier: the owner's list is the only way to learn
	// which sessions exist.
	assert.ErrorIs(t, service.RevokeSession(t.Context(), current.String(), wireOf(t, userID), theirs.String()), ErrSessionNotFound)
	assert.ErrorIs(t, service.RevokeSession(t.Context(), current.String(), wireOf(t, userID), "sess_000000000000000000000000a"), ErrSessionNotFound)

	// A caller whose own session has ended cannot manage sessions at all:
	// the gate is the caller's own row, not the target's.
	assert.ErrorIs(t, service.RevokeSession(t.Context(), other.String(), wireOf(t, userID), theirs.String()), ErrSessionEnded)

	// Ending the current session is what SignOut does; the event names the
	// happening so the log can tell the two apart.
	third, _ := seedSession(t, pool, userID, "third", "password", false)
	require.NoError(t, service.RevokeSession(t.Context(), third.String(), wireOf(t, userID), current.String()))
	assert.Equal(t, 1, auditCount(t, pool, audit.EventSessionRevoked, current.UUID()))
}

func TestListSessionsAnswersTheAccountsOwnNewestFirst(t *testing.T) {
	testutils.SkipWithoutDocker(t)

	pool := migratedPool(t)
	service, _ := testService(t, pool)
	userID := seedAccount(t, pool, "hermione")
	second := seedAccount(t, pool, "ron")
	first, _ := seedSession(t, pool, userID, "first", "password", false)
	time.Sleep(10 * time.Millisecond)
	secondOfFirst, _ := seedSession(t, pool, userID, "second", "one_time_access", true)
	seedSession(t, pool, second, "theirs", "password", false)

	rows, pagination, err := service.ListSessions(t.Context(), first.String(), wireOf(t, userID), 1, 10)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.Equal(t, 2, *pagination.TotalItems)
	assert.Equal(t, secondOfFirst.String(), rows[0].ID.String(),
		"the newest session answers first")
	assert.Equal(t, first.String(), rows[1].ID.String())
}

func TestRefreshRotatesTheTokenAndKeepsTheSession(t *testing.T) {
	testutils.SkipWithoutDocker(t)

	pool := migratedPool(t)
	service, _ := testService(t, pool)
	userID := seedAccount(t, pool, "hermione")
	sid, token := seedSession(t, pool, userID, "one", "password", false)

	refreshed, err := service.Refresh(t.Context(), token)
	require.NoError(t, err)

	// The session keeps its identity: the id is the row's, the answer names
	// the account, and the row now carries the renewal's secret and stamp.
	assert.Equal(t, sid.String(), refreshed.SessionID)
	assert.Equal(t, "fake-token-for-"+wireOf(t, userID), refreshed.AccessToken)
	assert.Equal(t, "hermione", refreshed.User.Username)
	row, err := service.repo.GetSession(t.Context(), pool, sid)
	require.NoError(t, err)
	require.NotNil(t, row.RefreshedAt)

	// The new credential works; the spent one opens nothing.
	_, err = service.repo.FindActiveByTokenHash(t.Context(), pool,
		crypto.HashRefreshToken(refreshed.RefreshToken), time.Now())
	assert.NoError(t, err)
	_, err = service.repo.FindActiveByTokenHash(t.Context(), pool,
		crypto.HashRefreshToken(token), time.Now())
	assert.ErrorIs(t, err, datastore.ErrNoRows)

	// A revoked session answers the same failure an unknown token does:
	// the rotation's write carries the gate, so a session ended between the
	// read and the write costs the new secret and nothing else.
	_, err = service.SignOut(t.Context(), sid.String(), wireOf(t, userID))
	require.NoError(t, err)
	_, err = service.Refresh(t.Context(), refreshed.RefreshToken)
	assert.ErrorIs(t, err, ErrSessionEnded)
	_, err = service.Refresh(t.Context(), "not-a-token")
	assert.ErrorIs(t, err, ErrSessionEnded)
	_, err = service.Refresh(t.Context(), "")
	assert.ErrorIs(t, err, ErrSessionEnded)
}
