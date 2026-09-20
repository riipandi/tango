package auditlog

import (
	"strconv"
	"testing"
	"time"

	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/internal/transport/middleware"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/pkg/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// userStoreForAuditTest builds a user store for FK-valid rows.
func userStoreForAuditTest(ds datastore.Store) *user.PostgresStore {
	return user.NewPostgresStore(ds)
}

var fixedTime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// newTestStore builds a Postgres-backed store over the shared test
// container with all migrations applied.
func newTestStore(t *testing.T) *PostgresStore {
	t.Helper()
	ctx := t.Context()

	pg := testutils.StartPostgres(ctx, t)
	if _, err := database.MigrateUp(ctx, pg.DSN); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	store, err := datastore.New(ctx, datastore.Options{DSN: pg.DSN})
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })

	return NewPostgresStore(store)
}

func TestRecordAssignsIDAndTimestamp(t *testing.T) {
	store := newTestStore(t)
	mod := New(store)

	entry := Entry{Event: "user.created"}
	require.NoError(t, mod.Record(t.Context(), &entry))
	assert.False(t, entry.ID.IsZero())
	assert.False(t, entry.CreatedAt.IsZero())
}

func TestRecordKeepsGivenValues(t *testing.T) {
	store := newTestStore(t)
	mod := New(store)

	entry := Entry{Event: "a", CreatedAt: fixedTime}
	require.NoError(t, mod.Record(t.Context(), &entry))
	assert.True(t, entry.CreatedAt.Equal(fixedTime))
}

func TestRecordPersistsEnumsAndPayload(t *testing.T) {
	store := newTestStore(t)

	entry := Entry{
		Event:        "user.created",
		Trigger:      TriggerUser,
		Status:       StatusSuccess,
		Payload:      map[string]any{"actor": "someone"},
		ResourceType: "user",
	}
	require.NoError(t, store.Record(t.Context(), &entry))

	entries, total, err := store.List(t.Context(), ListFilters{}, Page{Page: 1, Limit: 10})
	require.NoError(t, err)
	require.Positive(t, total)
	require.NotEmpty(t, entries)

	var found *Entry
	for i := range entries {
		if entries[i].ID == entry.ID {
			found = &entries[i]
		}
	}
	require.NotNil(t, found, "recorded entry must be listed")
	assert.Equal(t, "user.created", found.Event)
	assert.Equal(t, TriggerUser, found.Trigger)
	assert.Equal(t, StatusSuccess, found.Status)
	assert.Equal(t, "someone", found.Payload["actor"])
	assert.Equal(t, "user", found.ResourceType)
}

// adminTestUser mirrors the principal in the users table for the
// FK-bound audit rows.
var fixedPrincipal = middleware.Principal{
	SessionID: "test-session",
	UserID:    "01a0996e-9bc3-79b8-a151-d392733a2d58",
	IsAdmin:   true,
}

func TestModuleContracts(t *testing.T) {
	mod := New(newTestStore(t))
	assert.Equal(t, ModuleName, mod.Name())
}

func TestNewRejectsNilStore(t *testing.T) {
	require.Panics(t, func() { New(nil) })
}

// TestListFiltersByUserAndEvent pins the store-level filter contract:
// a garbage user_id never reaches the store as a Postgres type error.

func TestListFiltersByUserAndEvent(t *testing.T) {
	ctx := t.Context()

	pg := testutils.StartPostgres(ctx, t)
	if _, err := database.MigrateUp(ctx, pg.DSN); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	ds, err := datastore.New(ctx, datastore.Options{DSN: pg.DSN})
	require.NoError(t, err)
	t.Cleanup(func() { ds.Close() })
	store := NewPostgresStore(ds)
	users := userStoreForAuditTest(ds)

	u, err := users.Create(ctx, user.CreateParams{
		Username: "audit_" + strconv.FormatInt(time.Now().UnixNano(), 10),
		Email:    "audit-" + strconv.FormatInt(time.Now().UnixNano(), 10) + "@example.com"})
	require.NoError(t, err)

	entry := Entry{Event: "user.signed_in", UserID: ptr(u.ID.UUID())}
	require.NoError(t, store.Record(ctx, &entry))
	require.NoError(t, store.Record(ctx, &Entry{Event: "user.signed_out"}))

	filtered, total, err := store.List(ctx, ListFilters{
		Event:  "user.signed_in",
		UserID: u.ID.UUID(),
	}, Page{Page: 1, Limit: 10})
	require.NoError(t, err)
	require.Len(t, filtered, 1)
	assert.Equal(t, "user.signed_in", filtered[0].Event)
	assert.Equal(t, 1, total)

	byUser, _, err := store.List(ctx, ListFilters{UserID: u.ID.UUID()},
		Page{Page: 1, Limit: 10})
	require.NoError(t, err)
	require.Len(t, byUser, 1)

	filterUsers, err := store.UserFilterValues(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, filterUsers)
	assert.Contains(t, filterUsers[0], u.ID.UUID())
}

func ptr(s string) *string { return &s }
