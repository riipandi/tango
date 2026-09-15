package auditlog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	jsonv2 "encoding/json/v2"

	"github.com/go-chi/chi/v5"
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

// fakeAuthenticator resolves any cookie token to the fixed
// principal — tests only exercise route protection, not tokens.
type fakeAuthenticator struct{ principal middleware.Principal }

func (f *fakeAuthenticator) ResolveSession(_ context.Context, _ string) (middleware.Principal, error) {
	return f.principal, nil
}

// adminTestUser mirrors the principal in the users table for the
// FK-bound audit rows.
var fixedPrincipal = middleware.Principal{
	SessionID: "test-session",
	UserID:    "01a0996e-9bc3-79b8-a151-d392733a2d58",
	IsAdmin:   true,
}

// mountWithGuards builds the router with both guards wired (self
// auth + admin header guard).
func mountWithGuards(t *testing.T, mod *Module) chi.Router {
	t.Helper()
	mod.selfAuth = &fakeAuthenticator{principal: fixedPrincipal}
	mod.cookie = "tango_session"
	mod.adminGuard = func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Admin") == "" {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}

	r := chi.NewRouter()
	r.Route("/api", func(api chi.Router) {
		api.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				next.ServeHTTP(w, req.WithContext(middleware.WithPrincipal(req.Context(), fixedPrincipal)))
			})
		})
		mod.APIRoutes(api)
	})
	return r
}

func TestListEndpointEnvelopeAndPagination(t *testing.T) {
	mod := New(newTestStore(t))
	for range 3 {
		require.NoError(t, mod.Record(t.Context(), &Entry{Event: "user.created"}))
	}
	router := mountWithGuards(t, mod)

	// Admin listing with pagination.
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/audit-logs/all?page=1&limit=3", nil)
	req.Header.Set("X-Admin", "1")
	router.ServeHTTP(w, req)
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
	assert.Equal(t, "user.created", body.Data[0].Event)
	assert.NotNil(t, body.Metadata.TotalItems)
	assert.GreaterOrEqual(t, *body.Metadata.TotalItems, 3)
	assert.Equal(t, 3, *body.Metadata.Limit)

	// Paged: limit=2 yields exactly two entries.
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/audit-logs/all?page=1&limit=2", nil)
	req.Header.Set("X-Admin", "1")
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, jsonv2.Unmarshal(w.Body.Bytes(), &body))
	require.Len(t, body.Data, 2)
}

func TestListInvalidPagination(t *testing.T) {
	mod := New(newTestStore(t))
	router := mountWithGuards(t, mod)

	req := httptest.NewRequest(http.MethodGet, "/api/audit-logs/all?page=nope", nil)
	req.Header.Set("X-Admin", "1")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSelfListingScopesToCurrentUser(t *testing.T) {
	ctx := t.Context()
	pg := testutils.StartPostgres(ctx, t)
	if _, err := database.MigrateUp(ctx, pg.DSN); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	ds, err := datastore.New(ctx, datastore.Options{DSN: pg.DSN})
	require.NoError(t, err)
	t.Cleanup(func() { ds.Close() })

	mod := New(NewPostgresStore(ds))
	users := user.NewPostgresStore(ds)
	u, err := users.Create(ctx, user.CreateParams{
		Username: "self_" + strconv.FormatInt(time.Now().UnixNano(), 10),
		Email:    "self-" + strconv.FormatInt(time.Now().UnixNano(), 10) + "@example.com",
	})
	require.NoError(t, err)

	principal := fixedPrincipal
	principal.UserID = u.ID.String()
	require.NoError(t, mod.Record(ctx, &Entry{Event: "user.signed_in", UserID: ptr(u.ID.UUID())}))
	require.NoError(t, mod.Record(ctx, &Entry{Event: "user.signed_out"}))

	mod.selfAuth = &fakeAuthenticator{principal: principal}
	mod.cookie = "tango_session"
	r := chi.NewRouter()
	r.Route("/api", mod.APIRoutes)

	req := httptest.NewRequest(http.MethodGet, "/api/audit-logs", nil)
	req.AddCookie(&http.Cookie{Name: "tango_session", Value: "any"})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var body struct {
		Data []struct {
			Event string `json:"event"`
		} `json:"data"`
	}
	require.NoError(t, jsonv2.Unmarshal(w.Body.Bytes(), &body))
	require.NotEmpty(t, body.Data)
	for _, item := range body.Data {
		assert.Equal(t, "user.signed_in", item.Event, "self listing must scope to the current user")
	}
}

func TestModuleContracts(t *testing.T) {
	mod := New(newTestStore(t))
	assert.Equal(t, ModuleName, mod.Name())

	// Without guards nothing is mounted — fail closed.
	r := chi.NewRouter()
	r.Route("/api", mod.APIRoutes)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/audit-logs", nil))
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestNewRejectsNilStore(t *testing.T) {
	require.Panics(t, func() { New(nil) })
}

func TestAdminGuardProtectsListing(t *testing.T) {
	mod := New(newTestStore(t))
	router := mountWithGuards(t, mod)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/audit-logs/all", nil))
	assert.Equal(t, http.StatusForbidden, w.Code)

	// With the admin header the listing and filter values open up.
	req := httptest.NewRequest(http.MethodGet, "/api/audit-logs/all", nil)
	req.Header.Set("X-Admin", "1")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	req = httptest.NewRequest(http.MethodGet, "/api/audit-logs/filters/users", nil)
	req.Header.Set("X-Admin", "1")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	req = httptest.NewRequest(http.MethodGet, "/api/audit-logs/filters/client-names", nil)
	req.Header.Set("X-Admin", "1")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
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
