package ldapsync

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mountRouter mounts the feature the way the registry does, with a
// guard that any request passes (or rejects with X-Block).
func mountRouter(feature *APIFeature) chi.Router {
	r := chi.NewRouter()
	module := feature.WithGuard(func(next chi.Router) chi.Router {
		next.Use(func(h http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("X-Block") != "" {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				h.ServeHTTP(w, r)
			})
		})
		return next
	})
	r.Route("/api", module.APIRoutes)
	return r
}

// TestSyncEndpoint drives POST /application-configuration/sync-ldap
// through every outcome branch.
func TestSyncEndpoint(t *testing.T) {
	fake, _, adminName := directory(t)
	svc, _ := newSyncService(t, fake)

	current := settings()
	current.AdminGroupName = adminName
	feature := New(svc, func(context.Context) (LDAPSettings, error) { return current, nil })
	router := mountRouter(feature)

	post := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/application-configuration/sync-ldap", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}

	// The guard rejects blocked requests before the handler runs.
	blocked := httptest.NewRequest(http.MethodPost, "/api/application-configuration/sync-ldap", nil)
	blocked.Header.Set("X-Block", "1")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, blocked)
	assert.Equal(t, http.StatusForbidden, w.Code)

	// Success: 200 with the sync stats.
	w = post()
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var env struct {
		Status string `json:"status"`
		Data   struct {
			UsersCreated int `json:"users_created"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &env))
	assert.Equal(t, "success", env.Status)
	assert.Equal(t, 2, env.Data.UsersCreated)

	// Disabled: 409.
	current.Enabled = false
	assert.Equal(t, http.StatusConflict, post().Code)

	// Unconfigured: 422.
	current = settings()
	current.Base = ""
	assert.Equal(t, http.StatusUnprocessableEntity, post().Code)

	// Settings source failure: 500.
	erroring := New(svc, func(context.Context) (LDAPSettings, error) {
		return LDAPSettings{}, errors.New("no settings")
	})
	erroringRouter := mountRouter(erroring)
	req := httptest.NewRequest(http.MethodPost, "/api/application-configuration/sync-ldap", nil)
	w = httptest.NewRecorder()
	erroringRouter.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)

	// LDAP failure mid-sync: 502.
	fake2, _, _ := directory(t)
	fake2.searchErr = errors.New("ldap down")
	svc2, _ := newSyncService(t, fake2)
	failing := New(svc2, func(context.Context) (LDAPSettings, error) { return settings(), nil })
	failingRouter := mountRouter(failing)
	req2 := httptest.NewRequest(http.MethodPost, "/api/application-configuration/sync-ldap", nil)
	w = httptest.NewRecorder()
	failingRouter.ServeHTTP(w, req2)
	assert.Equal(t, http.StatusBadGateway, w.Code)
}

func TestFeatureLifecycle(t *testing.T) {
	fake, _, _ := directory(t)
	svc, _ := newSyncService(t, fake)

	feature := New(svc, func(context.Context) (LDAPSettings, error) { return settings(), nil })
	assert.Equal(t, "ldapsync", feature.Name())

	// Start arms the ticker loop; canceling the context ends it. The
	// hourly cadence never fires inside the test window.
	ctx, cancel := context.WithCancel(context.Background())
	require.NoError(t, feature.Start(ctx))
	require.NoError(t, feature.Stop(ctx))
	cancel()
	time.Sleep(50 * time.Millisecond) // let the goroutine observe the cancel
}

// The loop must survive a failing settings source without dying.
func TestLoopSurvivesSettingsErrors(t *testing.T) {
	fake, _, _ := directory(t)
	svc, _ := newSyncService(t, fake)

	feature := New(svc, func(context.Context) (LDAPSettings, error) {
		return LDAPSettings{}, errors.New("no settings")
	})
	ctx, cancel := context.WithCancel(context.Background())
	require.NoError(t, feature.Start(ctx))
	cancel()
	time.Sleep(50 * time.Millisecond)
}
