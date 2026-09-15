package appconfig

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/riipandi/tango/internal/mailer"
	"github.com/riipandi/tango/internal/transport/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeSender records the queued messages and answers the principal
// lookups the handler needs.
type fakeSender struct {
	messages []mailer.Message
	err      error
}

func (s *fakeSender) EnqueueEmail(_ context.Context, msg mailer.Message) error {
	if s.err != nil {
		return s.err
	}
	s.messages = append(s.messages, msg)
	return nil
}

// testPrincipalContext injects the admin principal the handler reads,
// standing in for the session middleware.
func testPrincipalContext(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(middleware.WithPrincipal(r.Context(), middleware.Principal{
			UserID:   "user_01m2ffa7q5f3msq05b9x7z062z",
			Username: "root",
			Email:    "root@example.com",
			IsAdmin:  true,
		})))
	})
}

func withGuard(m *Module) *Module {
	m.guard = testPrincipalContext
	return m
}

func mount(t *testing.T, module *Module) chi.Router {
	t.Helper()
	r := chi.NewRouter()
	r.Route("/api", module.APIRoutes)
	return r
}

func post(t *testing.T, router chi.Router, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(http.MethodPost, path, nil)
	} else {
		req = httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestTestEmailQueuesToTheSignedInAdmin(t *testing.T) {
	sender := &fakeSender{}
	router := mount(t, withGuard(New(sender)))

	w := post(t, router, "/api/application-configuration/test-email", "")
	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())

	require.Len(t, sender.messages, 1)
	assert.Equal(t, "test-email", sender.messages[0].Template)
	assert.NotEmpty(t, sender.messages[0].To)
}

func TestTestEmailHonoursAnExplicitRecipient(t *testing.T) {
	sender := &fakeSender{}
	router := mount(t, withGuard(New(sender)))

	w := post(t, router, "/api/application-configuration/test-email",
		`{"email":"ops@example.com"}`)
	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
	require.Len(t, sender.messages, 1)
	assert.Equal(t, "ops@example.com", sender.messages[0].To)
}

func TestTestEmailRejectsABadAddress(t *testing.T) {
	router := mount(t, withGuard(New(&fakeSender{})))

	for _, body := range []string{`{"email":"not-an-email"}`, `{"email":`} {
		w := post(t, router, "/api/application-configuration/test-email", body)
		assert.Equal(t, http.StatusUnprocessableEntity, w.Code, body)
	}
}

func TestTestEmailSurfacesQueueFailure(t *testing.T) {
	sender := &fakeSender{err: assert.AnError}
	router := mount(t, withGuard(New(sender)))

	w := post(t, router, "/api/application-configuration/test-email", "")
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestModuleFailClosedWithoutGuardOrMailer(t *testing.T) {
	// No mailer: nothing to deliver, so nothing mounts.
	router := mount(t, withGuard(New(nil)))
	w := post(t, router, "/api/application-configuration/test-email", "")
	assert.Equal(t, http.StatusNotFound, w.Code)

	// No guard: administrative surface stays hidden.
	router = mount(t, New(&fakeSender{}))
	w = post(t, router, "/api/application-configuration/test-email", "")
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestModuleName(t *testing.T) {
	assert.Equal(t, ModuleName, New(&fakeSender{}).Name())
}
