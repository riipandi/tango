package totp

// handler_test.go drives the TOTP HTTP lifecycle over the real
// session service and guard: enrollment, confirmation, pending
// verification with cookie rotation, recovery codes, and
// disablement.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	jsonv2 "encoding/json/v2"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/internal/transport/middleware"
	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/modules/identity/password"
	"github.com/riipandi/tango/modules/identity/session"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/pkg/crypto"
	"github.com/riipandi/tango/pkg/testutils"
)

// recorderFunc adapts a function to the audit Recorder contract.
type recorderFunc func(ctx context.Context, e identity.AuditEvent, exec datastore.Executor)

func (f recorderFunc) Record(ctx context.Context, e identity.AuditEvent, exec datastore.Executor) {
	f(ctx, e, exec)
}

// sessionIssuer adapts the real session service to the Sessions
// contract.
type sessionIssuer struct{ sessions *session.Service }

func (s sessionIssuer) IssueForUser(ctx context.Context, userID user.UserID, provider string, meta session.Meta) (string, error) {
	return s.sessions.IssueForUser(ctx, userID, provider, meta)
}

func newTestRouter(t *testing.T, record func(context.Context, identity.AuditEvent, datastore.Executor)) (chi.Router, *Service, *password.Service, user.Store) {
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

	var recorder identity.Recorder
	if record != nil {
		recorder = recorderFunc(record)
	}
	cipher, err := crypto.NewCipher(make([]byte, 32))
	require.NoError(t, err)

	// The TOTP service binds the session issuer after construction;
	// the session service needs the MFA port, mirroring the registry.
	svc := NewService(NewPostgresStore(ds), users, nil, passwords, cipher, "Tango", recorder)
	sessions := session.NewService(session.NewPostgresStore(ds), passwords, users, nil, session.WithMFAPort(svc))
	svc.BindSessions(sessionIssuer{sessions})

	r := chi.NewRouter()
	r.Route("/api", func(r chi.Router) {
		sessions.APIRoutes(r, identity.RouteGroups{})
		NewFeature(svc).WithCookie(false).APIRoutes(r, identity.RouteGroups{
			Self: middlewareOf(sessions),
		})
	})
	return r, svc, passwords, users
}

// middlewareOf builds the real self guard from the session service.
func middlewareOf(sessions *session.Service) func(http.Handler) http.Handler {
	return middleware.RequireAuth(sessions, session.CookieName)
}

func provisionUser(t *testing.T, users user.Store, passwords *password.Service) user.User {
	t.Helper()
	stamp := strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	u, err := users.Create(t.Context(), user.CreateParams{
		Username: "totph_" + stamp,
		Email:    "totph-" + stamp + "@example.com",
	})
	require.NoError(t, err)
	require.NoError(t, passwords.SetPassword(t.Context(), u.ID, "s3cret-p@ss"))
	return u
}

func postJSON(t *testing.T, r chi.Router, method, path, cookie, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: session.CookieName, Value: cookie})
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestTOTPLifecycle(t *testing.T) {
	var events []identity.AuditEvent
	r, svc, passwords, users := newTestRouter(t, func(ctx context.Context, e identity.AuditEvent, exec datastore.Executor) {
		events = append(events, e)
	})
	u := provisionUser(t, users, passwords)
	cookie := cookieValue(t, signIn(t, r, u.Username))

	// Anonymous callers never reach the lifecycle.
	w := postJSON(t, r, http.MethodPost, "/api/mfa/totp/enroll", "", `{}`)
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// Enrollment returns the seed and provisioning URI once.
	w = postJSON(t, r, http.MethodPost, "/api/mfa/totp/enroll", cookie, `{}`)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var enrolled struct {
		Data struct {
			Secret          string `json:"secret"`
			ProvisioningURI string `json:"provisioning_uri"`
		} `json:"data"`
	}
	require.NoError(t, jsonv2.Unmarshal(w.Body.Bytes(), &enrolled))
	require.NotEmpty(t, enrolled.Data.Secret)
	assert.Contains(t, enrolled.Data.ProvisioningURI, "otpauth://totp/Tango:")

	// Status before confirmation.
	w = postJSON(t, r, http.MethodGet, "/api/mfa/totp/status", cookie, "")
	require.Equal(t, http.StatusOK, w.Code)
	var status struct {
		Data struct {
			Confirmed              bool `json:"confirmed"`
			RecoveryCodesRemaining int  `json:"recovery_codes_remaining"`
		} `json:"data"`
	}
	require.NoError(t, jsonv2.Unmarshal(w.Body.Bytes(), &status))
	assert.False(t, status.Data.Confirmed)

	// A wrong code does not confirm.
	w = postJSON(t, r, http.MethodPost, "/api/mfa/totp/confirm", cookie, `{"code":"000000"}`)
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// The live code confirms and issues the recovery codes once.
	live := codeAt(t, enrolled.Data.Secret, time.Now().UTC())
	w = postJSON(t, r, http.MethodPost, "/api/mfa/totp/confirm", cookie, `{"code":"`+live+`"}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var confirmed struct {
		Data struct {
			RecoveryCodes []string `json:"recovery_codes"`
		} `json:"data"`
	}
	require.NoError(t, jsonv2.Unmarshal(w.Body.Bytes(), &confirmed))
	require.Len(t, confirmed.Data.RecoveryCodes, recoveryCodeCount)

	// Status flips; the seed never appears again.
	w = postJSON(t, r, http.MethodGet, "/api/mfa/totp/status", cookie, "")
	require.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, jsonv2.Unmarshal(w.Body.Bytes(), &status))
	assert.True(t, status.Data.Confirmed)
	assert.Equal(t, recoveryCodeCount, status.Data.RecoveryCodesRemaining)
	assert.NotContains(t, w.Body.String(), enrolled.Data.Secret)

	// A second confirm conflicts.
	w = postJSON(t, r, http.MethodPost, "/api/mfa/totp/confirm", cookie, `{"code":"`+live+`"}`)
	assert.Equal(t, http.StatusConflict, w.Code)

	// Pending verification: sign-in now lands in the pending state.
	w = signIn(t, r, u.Username)
	require.Equal(t, http.StatusOK, w.Code)
	var pendingBody struct {
		Data struct {
			Pending bool `json:"pending"`
		} `json:"data"`
	}
	require.NoError(t, jsonv2.Unmarshal(w.Body.Bytes(), &pendingBody))
	require.True(t, pendingBody.Data.Pending)

	pendingCookies := w.Result().Cookies()
	require.Len(t, pendingCookies, 1)
	require.Equal(t, identity.PendingCookieName, pendingCookies[0].Name)

	// A wrong verification code fails without a session.
	wrongVerify := httptest.NewRequest(http.MethodPost, "/api/mfa/totp/verify", strings.NewReader(`{"code":"000000"}`))
	wrongVerify.Header.Set("Content-Type", "application/json")
	wrongVerify.AddCookie(&http.Cookie{Name: identity.PendingCookieName, Value: pendingCookies[0].Value})
	w = httptest.NewRecorder()
	r.ServeHTTP(w, wrongVerify)
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// The live code upgrades the pending auth into the full session:
	// the session cookie replaces the pending one.
	verifyReq := httptest.NewRequest(http.MethodPost, "/api/mfa/totp/verify",
		strings.NewReader(`{"code":"`+codeAt(t, enrolled.Data.Secret, time.Now().UTC())+`"}`))
	verifyReq.Header.Set("Content-Type", "application/json")
	verifyReq.AddCookie(&http.Cookie{Name: identity.PendingCookieName, Value: pendingCookies[0].Value})
	w = httptest.NewRecorder()
	r.ServeHTTP(w, verifyReq)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	sessionCookies := w.Result().Cookies()
	require.Len(t, sessionCookies, 2)
	names := []string{sessionCookies[0].Name, sessionCookies[1].Name}
	assert.Contains(t, names, session.CookieName)
	assert.Contains(t, names, identity.PendingCookieName)
	for _, c := range sessionCookies {
		if c.Name == session.CookieName {
			assert.NotEmpty(t, c.Value, "the fresh session cookie carries the token")
		} else {
			assert.Empty(t, c.Value, "the pending cookie is cleared")
		}
	}

	// Recovery code path: burn one code through the store to prove
	// the remaining count drops.
	spent, err := svc.store.ConsumeRecoveryCode(t.Context(), u.ID, recoveryHash(confirmed.Data.RecoveryCodes[0]))
	require.NoError(t, err)
	assert.True(t, spent)

	w = postJSON(t, r, http.MethodGet, "/api/mfa/totp/status", cookie, "")
	require.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, jsonv2.Unmarshal(w.Body.Bytes(), &status))
	assert.Equal(t, recoveryCodeCount-1, status.Data.RecoveryCodesRemaining)

	// Disable with the current password clears the enrollment.
	w = postJSON(t, r, http.MethodDelete, "/api/mfa/totp", cookie, `{"password":"s3cret-p@ss"}`)
	assert.Equal(t, http.StatusNoContent, w.Code)

	w = postJSON(t, r, http.MethodGet, "/api/mfa/totp/status", cookie, "")
	require.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, jsonv2.Unmarshal(w.Body.Bytes(), &status))
	assert.False(t, status.Data.Confirmed)

	// Audit events cover the lifecycle without secrets.
	assert.NotEmpty(t, events)
	for _, e := range events {
		assert.NotContains(t, e.Action, enrolled.Data.Secret)
	}
}

// cookieValue extracts the session cookie value from a sign-in
// response.
func cookieValue(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	cookies := w.Result().Cookies()
	require.NotEmpty(t, cookies)
	return cookies[0].Value
}

// signIn submits the sign-in form and returns the raw response.
func signIn(t *testing.T, r chi.Router, identityText string) *httptest.ResponseRecorder {
	t.Helper()
	body := `{"identity":"` + identityText + `","secret":"s3cret-p@ss"}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/sign-in", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}
