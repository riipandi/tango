package recovery

// recovery_test.go drives the forgot/reset lifecycle over the real
// session service: generic responses, single-use consumption, and
// session invalidation after reset.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/internal/mailer"
	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/modules/identity/password"
	"github.com/riipandi/tango/modules/identity/session"
	"github.com/riipandi/tango/modules/identity/token"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/pkg/crypto"
	"github.com/riipandi/tango/pkg/testutils"
)

// recorderFunc adapts a function to the audit Recorder contract.
type recorderFunc func(ctx context.Context, e identity.AuditEvent, exec datastore.Executor)

func (f recorderFunc) Record(ctx context.Context, e identity.AuditEvent, exec datastore.Executor) {
	f(ctx, e, exec)
}

// stubMail captures the queued recovery emails.
type stubMail struct {
	messages []mailer.Message
}

func (s *stubMail) EnqueueEmail(ctx context.Context, message mailer.Message) error {
	s.messages = append(s.messages, message)
	return nil
}

// linkToken extracts the raw token from a queued reset link.
func linkToken(t *testing.T, message mailer.Message) string {
	t.Helper()
	link, _ := message.Data["ResetLink"].(string)
	require.NotEmpty(t, link)
	parsed, err := url.Parse(link)
	require.NoError(t, err)
	raw := parsed.Query().Get("token")
	require.NotEmpty(t, raw)
	return raw
}

func newTestRouter(t *testing.T, record func(context.Context, identity.AuditEvent, datastore.Executor)) (chi.Router, *password.Service, user.Store, *stubMail) {
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
	sessions := session.NewService(session.NewPostgresStore(ds), passwords, users, nil)

	var recorder identity.Recorder
	if record != nil {
		recorder = recorderFunc(record)
	}
	mail := &stubMail{}
	recoverySvc := New(
		token.NewStore(ds, token.PurposePasswordReset),
		users,
		password.NewPostgresStore(ds),
		sessions,
		crypto.NewPasswordHasher().WithAlgorithm(crypto.AlgorithmScrypt),
		recorder,
		WithMail(mail, "http://localhost:3000"),
	)

	r := chi.NewRouter()
	r.Route("/api", func(r chi.Router) {
		sessions.APIRoutes(r, identity.RouteGroups{})
		NewFeature(recoverySvc).WithCookie(session.CookieName, false).APIRoutes(r, identity.RouteGroups{})
	})
	return r, passwords, users, mail
}

func provisionUser(t *testing.T, users user.Store, passwords *password.Service) user.User {
	t.Helper()
	stamp := strconv.FormatInt(time.Now().UnixNano(), 10)
	u, err := users.Create(t.Context(), user.CreateParams{
		Username: "reco_" + stamp,
		Email:    "reco-" + stamp + "@example.com",
	})
	require.NoError(t, err)
	require.NoError(t, passwords.SetPassword(t.Context(), u.ID, "old-s3cret-p@ss"))
	return u
}

func postJSON(t *testing.T, r chi.Router, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// getWithCookie issues a GET with the session cookie.
func getWithCookie(t *testing.T, r chi.Router, path, cookie string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.AddCookie(&http.Cookie{Name: session.CookieName, Value: cookie})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// signIn submits the sign-in form and returns the raw cookie value.
func signIn(t *testing.T, r chi.Router, identity, secret string) string {
	t.Helper()
	body := `{"identity":"` + identity + `","secret":"` + secret + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/sign-in", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	cookies := w.Result().Cookies()
	require.NotEmpty(t, cookies)
	return cookies[0].Value
}

func TestForgotIsAlwaysGeneric(t *testing.T) {
	r, passwords, users, mail := newTestRouter(t, nil)

	// An unknown address and a malformed body behave like anything
	// else: no enumeration, no mail.
	w := postJSON(t, r, "/api/auth/forgot-password", `{"identity":"ghost@example.com"}`)
	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Empty(t, mail.messages)

	w = postJSON(t, r, "/api/auth/forgot-password", `{}`)
	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
	assert.Empty(t, mail.messages)

	// A known address queues exactly one recovery email carrying the
	// reset link.
	u := provisionUser(t, users, passwords)
	w = postJSON(t, r, "/api/auth/forgot-password", `{"identity":"`+u.Email+`"}`)
	require.Equal(t, http.StatusNoContent, w.Code)
	require.Len(t, mail.messages, 1)
	assert.Contains(t, mail.messages[0].Data["ResetLink"], "/reset-password?token=")
}

func TestResetLifecycle(t *testing.T) {
	var events []identity.AuditEvent
	r, passwords, users, mail := newTestRouter(t, func(ctx context.Context, e identity.AuditEvent, exec datastore.Executor) {
		events = append(events, e)
	})
	u := provisionUser(t, users, passwords)

	// The requester holds an old session.
	oldSession := signIn(t, r, u.Username, "old-s3cret-p@ss")

	// Mint the token through the forgot flow; the raw value travels
	// by email only.
	require.Equal(t, http.StatusNoContent,
		postJSON(t, r, "/api/auth/forgot-password", `{"identity":"`+u.Email+`"}`).Code)
	require.Len(t, mail.messages, 1)
	resetToken := linkToken(t, mail.messages[0])

	// A weak replacement fails the shared policy without consuming
	// the token.
	w := postJSON(t, r, "/api/auth/reset-password", `{"token":"`+resetToken+`","new_password":"short"}`)
	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)

	// The happy path: 200 + a fresh session cookie.
	w = postJSON(t, r, "/api/auth/reset-password", `{"token":"`+resetToken+`","new_password":"new-s3cret-p@ss"}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	cookies := w.Result().Cookies()
	require.NotEmpty(t, cookies)
	newSession := cookies[0].Value

	// The old session died with the old secret; the fresh one lives.
	assert.Equal(t, http.StatusUnauthorized, getWithCookie(t, r, "/api/auth/session", oldSession).Code)
	assert.Equal(t, http.StatusOK, getWithCookie(t, r, "/api/auth/session", newSession).Code)

	// Audit: request + completion, no secrets in events.
	require.Len(t, events, 2)
	assert.Equal(t, "password.reset_requested", events[0].Action)
	assert.Equal(t, "password.reset_completed", events[1].Action)
	assert.NotContains(t, events[1].Actor, resetToken)

	// The token is burned: a replay is not found.
	w = postJSON(t, r, "/api/auth/reset-password", `{"token":"`+resetToken+`","new_password":"another-p@ss"}`)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestResetUnknownToken(t *testing.T) {
	r, _, _, _ := newTestRouter(t, nil)

	w := postJSON(t, r, "/api/auth/reset-password", `{"token":"deadbeef","new_password":"whatever123"}`)
	assert.Equal(t, http.StatusNotFound, w.Code)
}
