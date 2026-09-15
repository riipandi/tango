package webhook

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.jetify.com/typeid"
)

// mountRouter wires the module the way the registry does: inside the
// /api group, behind a guard that any request passes with X-Admin.
func mountRouter(t *testing.T, stack *testStack) chi.Router {
	t.Helper()

	module := New(stack.Service, WithGuard(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Admin") == "" {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}))

	r := chi.NewRouter()
	r.Route("/api", module.APIRoutes)
	return r
}

// do sends one request with the admin header set. An empty body sends
// no body at all, so the handler's decoder sees a clean EOF rather
// than a malformed document.
func do(t *testing.T, router chi.Router, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()

	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("X-Admin", "1")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// envelope pulls the status/message/data triple out of the response.
type envelope struct {
	Status   string         `json:"status"`
	Message  string         `json:"message"`
	Data     jsontext.Value `json:"data"`
	Error    jsontext.Value `json:"error"`
	Metadata struct {
		TotalItems *int `json:"total_items"`
		Page       *int `json:"page"`
	} `json:"metadata"`
}

func decodeEnvelope(t *testing.T, w *httptest.ResponseRecorder) envelope {
	t.Helper()
	var env envelope
	require.NoError(t, jsonv2.Unmarshal(w.Body.Bytes(), &env))
	return env
}

func TestRoutesRequireTheAdminGuard(t *testing.T) {
	stack := newTestStack(t, okSender())
	module := New(stack.Service, WithGuard(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		})
	}))

	r := chi.NewRouter()
	r.Route("/api", module.APIRoutes)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/webhooks", nil))
	assert.Equal(t, http.StatusForbidden, w.Code, "the guard must run before the handler")
}

func TestRoutesNotMountedWithoutAGuard(t *testing.T) {
	stack := newTestStack(t, okSender())
	module := New(stack.Service) // no guard: fail closed

	r := chi.NewRouter()
	r.Route("/api", module.APIRoutes)

	for _, path := range []string{"/api/webhooks", "/api/webhook-logs"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		assert.Equal(t, http.StatusNotFound, w.Code, path)
	}
}

func TestNewRejectsNilService(t *testing.T) {
	require.Panics(t, func() { New(nil) })
}

func TestCreateListGetUpdateDeleteLifecycle(t *testing.T) {
	stack := newTestStack(t, okSender())
	router := mountRouter(t, stack)

	// Create.
	w := do(t, router, http.MethodPost, "/api/webhooks",
		`{"name":"http-lifecycle","endpoint":"https://example.test/hook","event_types":["user.created"]}`)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())

	env := decodeEnvelope(t, w)
	assert.Equal(t, "success", env.Status)
	var created Webhook
	require.NoError(t, jsonv2.Unmarshal(env.Data, &created))
	assert.Equal(t, "http-lifecycle", created.Name)
	require.NotNil(t, created.Secret, "create must return the plaintext secret once")

	// List.
	w = do(t, router, http.MethodGet, "/api/webhooks?page=1&limit=10", "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	env = decodeEnvelope(t, w)
	var listed []Webhook
	require.NoError(t, jsonv2.Unmarshal(env.Data, &listed))
	require.Len(t, listed, 1)
	assert.Nil(t, listed[0].Secret, "listings must not carry the secret")
	require.NotNil(t, env.Metadata.TotalItems)
	assert.Equal(t, 1, *env.Metadata.TotalItems)

	// Get.
	w = do(t, router, http.MethodGet, "/api/webhooks/"+created.ID.String(), "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	env = decodeEnvelope(t, w)
	var fetched Webhook
	require.NoError(t, jsonv2.Unmarshal(env.Data, &fetched))
	assert.Equal(t, created.ID.String(), fetched.ID.String())
	assert.Nil(t, fetched.Secret)

	// Update.
	newName := "http-renamed"
	body := fmt.Sprintf(`{"name":%q,"enabled":false}`, newName)
	w = do(t, router, http.MethodPut, "/api/webhooks/"+created.ID.String(), body)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	env = decodeEnvelope(t, w)
	var updated Webhook
	require.NoError(t, jsonv2.Unmarshal(env.Data, &updated))
	assert.Equal(t, newName, updated.Name)
	assert.False(t, updated.Enabled)

	// Delete.
	w = do(t, router, http.MethodDelete, "/api/webhooks/"+created.ID.String(), "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	w = do(t, router, http.MethodGet, "/api/webhooks/"+created.ID.String(), "")
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestCreateRejectsInvalidPayload(t *testing.T) {
	stack := newTestStack(t, okSender())
	router := mountRouter(t, stack)

	cases := []struct {
		name string
		body string
	}{
		{"short name", `{"name":"ab","endpoint":"https://example.test"}`},
		{"missing endpoint", `{"name":"valid-name"}`},
		{"bad scheme", `{"name":"valid-name","endpoint":"ftp://example.test"}`},
		{"relative endpoint", `{"name":"valid-name","endpoint":"/hook"}`},
		{"malformed json", `{"name":`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := do(t, router, http.MethodPost, "/api/webhooks", tc.body)
			assert.Equal(t, http.StatusUnprocessableEntity, w.Code, w.Body.String())

			env := decodeEnvelope(t, w)
			assert.Equal(t, "error", env.Status)
			assert.NotEmpty(t, env.Error, "validation failures carry field errors")
		})
	}
}

func TestCreateDuplicateNameConflicts(t *testing.T) {
	stack := newTestStack(t, okSender())
	router := mountRouter(t, stack)

	body := `{"name":"dup-name","endpoint":"https://example.test/hook"}`
	require.Equal(t, http.StatusCreated, do(t, router, http.MethodPost, "/api/webhooks", body).Code)
	assert.Equal(t, http.StatusConflict, do(t, router, http.MethodPost, "/api/webhooks", body).Code)
}

func TestGetUnknownIDIs404(t *testing.T) {
	stack := newTestStack(t, okSender())
	router := mountRouter(t, stack)

	// A well-formed but unknown typed ID.
	missing := newWebhookID(t)

	w := do(t, router, http.MethodGet, "/api/webhooks/"+missing.String(), "")
	assert.Equal(t, http.StatusNotFound, w.Code)

	// A malformed ID never reaches the store.
	w = do(t, router, http.MethodGet, "/api/webhooks/not-a-typeid", "")
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestUpdateUnknownIDIs404(t *testing.T) {
	stack := newTestStack(t, okSender())
	router := mountRouter(t, stack)

	w := do(t, router, http.MethodPut, "/api/webhooks/not-a-typeid", `{"name":"whatever"}`)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestDeleteUnknownIDIs404(t *testing.T) {
	stack := newTestStack(t, okSender())
	router := mountRouter(t, stack)

	w := do(t, router, http.MethodDelete, "/api/webhooks/not-a-typeid", "")
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestRotateSecretEndpoint(t *testing.T) {
	stack := newTestStack(t, okSender())
	router := mountRouter(t, stack)

	created := mustCreateViaAPI(t, router, "rotate-http", "https://example.test/hook")

	w := do(t, router, http.MethodPost, "/api/webhooks/"+created.ID.String()+"/rotate-secret", "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	env := decodeEnvelope(t, w)
	var payload struct {
		ID     string `json:"id"`
		Secret string `json:"secret"`
	}
	require.NoError(t, jsonv2.Unmarshal(env.Data, &payload))
	assert.Equal(t, created.ID.String(), payload.ID)
	assert.NotEmpty(t, payload.Secret)
	assert.NotEqual(t, *created.Secret, payload.Secret, "rotation must issue a new secret")

	// Rotation of an unknown endpoint is a 404.
	w = do(t, router, http.MethodPost, "/api/webhooks/not-a-typeid/rotate-secret", "")
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestTestEndpointAcceptsAndQueues(t *testing.T) {
	stack := newTestStack(t, okSender())
	router := mountRouter(t, stack)

	created := mustCreateViaAPI(t, router, "test-http", "https://example.test/hook")

	w := do(t, router, http.MethodPost, "/api/webhooks/"+created.ID.String()+"/test", "")
	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())

	env := decodeEnvelope(t, w)
	var payload struct {
		LogID string `json:"log_id"`
		Event string `json:"event"`
	}
	require.NoError(t, jsonv2.Unmarshal(env.Data, &payload))
	assert.Equal(t, defaultTestEvent, payload.Event)
	assert.Equal(t, "webhook_log", mustLogIDFromString(t, payload.LogID).Prefix())

	// Unknown endpoint: 404.
	w = do(t, router, http.MethodPost, "/api/webhooks/not-a-typeid/test", "")
	assert.Equal(t, http.StatusNotFound, w.Code)

	// Malformed body: 422.
	w = do(t, router, http.MethodPost, "/api/webhooks/"+created.ID.String()+"/test", `{"payload":`)
	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
}

func TestLogsEndpointsListScopedAndGlobal(t *testing.T) {
	stack := newTestStack(t, okSender())
	router := mountRouter(t, stack)

	created := mustCreateViaAPI(t, router, "logs-http", "https://example.test/hook")
	_, err := stack.Service.DeliverTo(t.Context(), created.ID, "user.created", map[string]any{"event": "user.created"})
	require.NoError(t, err)

	// Per endpoint.
	w := do(t, router, http.MethodGet, "/api/webhooks/"+created.ID.String()+"/logs", "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	env := decodeEnvelope(t, w)
	var scoped []DeliveryLog
	require.NoError(t, jsonv2.Unmarshal(env.Data, &scoped))
	require.Len(t, scoped, 1)
	assert.Equal(t, created.ID.String(), scoped[0].WebhookID.String())

	// Global.
	w = do(t, router, http.MethodGet, "/api/webhook-logs", "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	env = decodeEnvelope(t, w)
	var all []DeliveryLog
	require.NoError(t, jsonv2.Unmarshal(env.Data, &all))
	assert.Len(t, all, 1)

	// Unknown endpoint for the scoped listing.
	w = do(t, router, http.MethodGet, "/api/webhooks/not-a-typeid/logs", "")
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestListRejectsMalformedQuery(t *testing.T) {
	stack := newTestStack(t, okSender())
	router := mountRouter(t, stack)

	w := do(t, router, http.MethodGet, "/api/webhooks?page=nope", "")
	assert.Equal(t, http.StatusBadRequest, w.Code)

	w = do(t, router, http.MethodGet, "/api/webhooks?enabled=perhaps", "")
	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)

	w = do(t, router, http.MethodGet, "/api/webhook-logs?limit=-5", "")
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestListFiltersThroughQueryParameters(t *testing.T) {
	stack := newTestStack(t, okSender())
	router := mountRouter(t, stack)

	mustCreateViaAPI(t, router, "filter-user", "https://one.test/hook")
	// A second endpoint that does NOT subscribe to user.created.
	w := do(t, router, http.MethodPost, "/api/webhooks",
		`{"name":"filter-api","endpoint":"https://two.test/hook","event_types":["api_key.created"]}`)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())

	w = do(t, router, http.MethodGet, "/api/webhooks?event=user.created", "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	env := decodeEnvelope(t, w)
	var listed []Webhook
	require.NoError(t, jsonv2.Unmarshal(env.Data, &listed))
	require.Len(t, listed, 1)
	assert.Equal(t, "filter-user", listed[0].Name)

	// Enabled filter.
	w = do(t, router, http.MethodGet, "/api/webhooks?enabled=true", "")
	require.Equal(t, http.StatusOK, w.Code)
	env = decodeEnvelope(t, w)
	require.NoError(t, jsonv2.Unmarshal(env.Data, &listed))
	assert.Len(t, listed, 2)
}

func TestStoreAccessorExposesPruner(t *testing.T) {
	stack := newTestStack(t, okSender())
	module := New(stack.Service)

	// The recurring cleanup job depends on this accessor.
	assert.NotNil(t, module.Store())
	assert.Equal(t, ModuleName, module.Name())
}

// TestEmitThroughModuleSink covers the module-level event sink the
// registry hands to other modules: the same outbox write, behind the
// public surface.
func TestEmitThroughModuleSink(t *testing.T) {
	stack := newTestStack(t, okSender())
	module := New(stack.Service)
	ctx := t.Context()

	// Without subscribers the sink is a no-op.
	require.NoError(t, module.Emit(ctx, "user.created", map[string]any{"event": "user.created"}))

	stack.create(t, "sink-target", "https://example.test/hook", AllEvents)
	require.NoError(t, module.Emit(ctx, "user.created", map[string]any{"event": "user.created"}))

	_, total, err := stack.Store.ListLogs(ctx, ListParams{}, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, total)
}

// mustCreateViaAPI creates an endpoint through the HTTP surface and
// returns the decoded entity.
func mustCreateViaAPI(t *testing.T, router chi.Router, name, endpoint string) Webhook {
	t.Helper()

	body := fmt.Sprintf(`{"name":%q,"endpoint":%q}`, name, endpoint)
	w := do(t, router, http.MethodPost, "/api/webhooks", body)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())

	env := decodeEnvelope(t, w)
	var created Webhook
	require.NoError(t, jsonv2.Unmarshal(env.Data, &created))
	return created
}

func mustLogIDFromString(t *testing.T, raw string) DeliveryLogID {
	t.Helper()
	id, err := parseDeliveryLogID(raw)
	require.NoError(t, err)
	return id
}

// newWebhookID mints a syntactically valid typed ID that no row owns,
// so a 404 test never trips the parser instead of the store.
func newWebhookID(t *testing.T) WebhookID {
	t.Helper()
	id, err := typeid.New[WebhookID]()
	require.NoError(t, err)
	return id
}
