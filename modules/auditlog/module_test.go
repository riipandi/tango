package auditlog

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	jsonv2 "encoding/json/v2"

	"github.com/go-chi/chi/v5"
	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/internal/kernel"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/pkg/responder"
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

	entries, total, err := store.List(t.Context(), ListFilters{}, responder.PaginationParams{Page: 1, Limit: 10})
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

func TestListEndpointEnvelopeAndPagination(t *testing.T) {
	mod := New(newTestStore(t))
	for range 3 {
		require.NoError(t, mod.Record(t.Context(), &Entry{Event: "user.created"}))
	}

	reg := kernel.NewRegistry()
	reg.Register(mod)
	r := chi.NewRouter()
	r.Route("/api", reg.ApplyAPI)

	// Limited: limit=3 yields exactly the three newest entries.
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/audit-logs?page=1&limit=3", nil))
	require.Equal(t, http.StatusOK, w.Code)

	var body struct {
		Status   string `json:"status"`
		Metadata struct {
			Page       *int `json:"page"`
			Limit      *int `json:"limit"`
			TotalItems *int `json:"total_items"`
		} `json:"metadata"`
		Data []struct {
			Event string `json:"event"`
		} `json:"data"`
	}
	require.NoError(t, jsonv2.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "success", body.Status)
	require.Len(t, body.Data, 3)
	// The three just-recorded entries are the newest.
	assert.Equal(t, "user.created", body.Data[0].Event)
	assert.NotNil(t, body.Metadata.TotalItems)
	assert.GreaterOrEqual(t, *body.Metadata.TotalItems, 3)
	assert.Equal(t, 3, *body.Metadata.Limit)

	// Paged: limit=2 yields exactly two entries.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/audit-logs?page=1&limit=2", nil))
	require.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, jsonv2.Unmarshal(w.Body.Bytes(), &body))
	require.Len(t, body.Data, 2)
}

func TestListInvalidPagination(t *testing.T) {
	mod := New(newTestStore(t))

	reg := kernel.NewRegistry()
	reg.Register(mod)
	r := chi.NewRouter()
	r.Route("/api", reg.ApplyAPI)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/audit-logs?page=nope", nil))
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestModuleContracts(t *testing.T) {
	mod := New(newTestStore(t))
	assert.Equal(t, ModuleName, mod.Name())

	reg := kernel.NewRegistry()
	reg.Register(mod) // must not panic: APIRoutable, unique name

	r := chi.NewRouter()
	r.Route("/api", reg.ApplyAPI)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/audit-logs", nil))
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestNewRejectsNilStore(t *testing.T) {
	require.Panics(t, func() { New(nil) })
}

func TestAdminGuardProtectsListing(t *testing.T) {
	mod := New(newTestStore(t))
	mod.MountAdminAPI(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Admin") == "" {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	})

	reg := kernel.NewRegistry()
	reg.Register(mod)
	r := chi.NewRouter()
	r.Route("/api", reg.ApplyAPI)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/audit-logs", nil))
	assert.Equal(t, http.StatusForbidden, w.Code)

	w = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/audit-logs", nil)
	req.Header.Set("X-Admin", "1")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Filter values live behind the same guard.
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/audit-logs/filters/users", nil)
	req.Header.Set("X-Admin", "1")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

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

	filtered, total, err := store.List(ctx, ListFilters{Event: "user.signed_in"}, responder.PaginationParams{Page: 1, Limit: 10})
	require.NoError(t, err)
	require.Len(t, filtered, 1)
	assert.Equal(t, "user.signed_in", filtered[0].Event)
	assert.Equal(t, 1, total)

	byUser, _, err := store.List(ctx, ListFilters{UserID: u.ID.UUID()},
		responder.PaginationParams{Page: 1, Limit: 10})
	require.NoError(t, err)
	require.Len(t, byUser, 1)

	filterUsers, err := store.UserFilterValues(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, filterUsers)
	assert.Contains(t, filterUsers[0], u.ID.UUID())
}

func ptr(s string) *string { return &s }
