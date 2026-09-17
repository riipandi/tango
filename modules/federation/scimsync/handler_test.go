package scimsync

import (
	"encoding/json" // json.RawMessage
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	jsonv2 "encoding/json/v2"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/riipandi/tango/internal/logger"
	"github.com/riipandi/tango/modules/federation"
)

// mountRouter mounts the feature the way the federation module does.
func mountRouter(svc *Service) chi.Router {
	r := chi.NewRouter()
	r.Route("/api", func(r chi.Router) { New(svc).APIRoutes(r, federation.RouteGroups{}) })
	return r
}

// do sends one JSON request and returns the recorder.
func do(r chi.Router, method, path, body string) *httptest.ResponseRecorder {
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// envelope pulls the status/message/data triple out of the response.
type envelope struct {
	Status string          `json:"status"`
	Data   json.RawMessage `json:"data"`
	Error  json.RawMessage `json:"error"`
}

func decodeEnvelope(t *testing.T, w *httptest.ResponseRecorder) envelope {
	t.Helper()
	var env envelope
	require.NoError(t, jsonv2.Unmarshal(w.Body.Bytes(), &env))
	return env
}

// TestHandlerProviderLifecycle drives the CRUD surface end to end.
func TestHandlerProviderLifecycle(t *testing.T) {
	store, _ := newStore(t)
	svc := NewService(store, &fakeSource{}, httpPoster{client: &http.Client{Timeout: 30 * time.Second}}, logger.Slog(logger.NewMock()))
	router := mountRouter(svc)
	ctx := t.Context()

	// Create: 201 with the token shown exactly once.
	body := fmt.Sprintf(`{"endpoint":"https://scim.example.com","token":"hdl-tok-%d","oidc_client_id":%q}`,
		time.Now().UnixNano(), clientFixtureID)
	w := do(router, http.MethodPost, "/api/scim/service-provider", body)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	created := decodeEnvelope(t, w)
	assert.Contains(t, string(created.Data), `"token":"hdl-tok-`)
	var view struct {
		ID string `json:"id"`
	}
	require.NoError(t, jsonv2.Unmarshal(created.Data, &view))

	// getByClient hides the token.
	w = do(router, http.MethodGet, "/api/oidc/clients/"+clientFixtureID+"/scim-service-provider", "")
	require.Equal(t, http.StatusOK, w.Code)
	assert.NotContains(t, w.Body.String(), "hdl-tok-")

	// Unknown client: 422. Duplicate: 409.
	w = do(router, http.MethodPost, "/api/scim/service-provider",
		`{"endpoint":"https://x.example.com","token":"t","oidc_client_id":"nope"}`)
	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
	w = do(router, http.MethodPost, "/api/scim/service-provider", body)
	assert.Equal(t, http.StatusConflict, w.Code)

	// Invalid body: 422.
	w = do(router, http.MethodPost, "/api/scim/service-provider",
		`{"endpoint":"/relative","token":"t","oidc_client_id":"c"}`)
	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)

	// Update: 200, token shown again.
	w = do(router, http.MethodPut, "/api/scim/service-provider/"+view.ID,
		`{"endpoint":"https://scim2.example.com","token":"rotated","oidc_client_id":"`+clientFixtureID+`"}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"token":"rotated"`)

	// Update with an unparseable id: 404.
	w = do(router, http.MethodPut, "/api/scim/service-provider/not-an-id", body)
	assert.Equal(t, http.StatusNotFound, w.Code)

	// Sync against an unreachable remote: 502.
	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadURL := dead.URL
	dead.Close()
	deadProvider, err := store.Create(ctx, UpsertParams{
		Endpoint: deadURL, Token: "t", OIDCClientID: newClientFixture(t, fixtureDS, "scim-dead"),
	})
	require.NoError(t, err)
	w = do(router, http.MethodPost, "/api/scim/service-provider/"+deadProvider.ID.String()+"/sync", "")
	assert.Equal(t, http.StatusBadGateway, w.Code)

	// Sync against the stub: 200. Bad id: 404.
	stub := newSCIMStub(t)
	_, err = store.Update(ctx, deadProvider.ID, UpsertParams{
		Endpoint: stub.srv.URL, Token: "t", OIDCClientID: deadProvider.OIDCClientID,
	})
	require.NoError(t, err)
	w = do(router, http.MethodPost, "/api/scim/service-provider/"+deadProvider.ID.String()+"/sync", "")
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
	w = do(router, http.MethodPost, "/api/scim/service-provider/zzz/sync", "")
	assert.Equal(t, http.StatusNotFound, w.Code)

	// Delete: 204, then the provider is gone (404 on repeat).
	w = do(router, http.MethodDelete, "/api/scim/service-provider/"+view.ID, "")
	assert.Equal(t, http.StatusNoContent, w.Code)
	w = do(router, http.MethodDelete, "/api/scim/service-provider/"+view.ID, "")
	assert.Equal(t, http.StatusNotFound, w.Code)

	// getByClient without a provider: 404.
	w = do(router, http.MethodGet, "/api/oidc/clients/"+clientFixtureID+"/scim-service-provider", "")
	assert.Equal(t, http.StatusNotFound, w.Code)
}
