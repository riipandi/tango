package totp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/internal/kernel"
	"github.com/riipandi/tango/internal/transport/middleware"
	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/pkg/crypto"
	"github.com/riipandi/tango/pkg/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubAccess resolves any bearer token to the seeded user's
// principal; "revoked-" fails.
type stubAccess struct{ userID string }

func (s stubAccess) ResolveAccess(_ context.Context, token string) (kernel.Principal, error) {
	if token == "" || strings.HasPrefix(token, "revoked-") {
		return kernel.Principal{}, errors.New("session: invalid or expired")
	}
	return kernel.Principal{SessionID: "sess_test", UserID: s.userID}, nil
}

// newRPCStack builds the MFA service over the throwaway database and
// mounts the Connect surface behind the real guard.
func newRPCStack(t *testing.T) (http.Handler, *Service, user.User, Store) {
	t.Helper()
	ctx := t.Context()
	pg := testutils.StartPostgres(ctx, t)
	if _, err := database.MigrateUp(ctx, pg.DSN); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	ds, err := datastore.New(ctx, datastore.Options{DSN: pg.DSN})
	require.NoError(t, err)
	t.Cleanup(func() { ds.Close() })

	cipher, err := crypto.NewCipher(make([]byte, 32))
	require.NoError(t, err)

	users := user.NewPostgresStore(ds)
	store := NewPostgresStore(ds)
	stamp := strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	created, err := users.Create(ctx, user.CreateParams{
		Username: "mfa_" + stamp,
		Email:    "mfa-" + stamp + "@example.com",
	})
	require.NoError(t, err)

	svc := NewService(store, users, &sessionStub{issued: make([]string, 0, 1)},
		&passwordStub{secret: "correct-horse"}, cipher, "Tango", nil)

	prefix, handler := svc.RPCService(false, stubAccess{userID: created.ID.String()})
	guarded := middleware.RPCSessionAuth(stubAccess{userID: created.ID.String()})(handler)
	mux := http.NewServeMux()
	mux.Handle(prefix, guarded)
	return mux, svc, created, store
}

// rpcPost posts an MFA procedure; the pending cookie rides when set.
func rpcPost(t *testing.T, h http.Handler, procedure, body, pendingCookie string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/tango.identity.v1.MfaService/"+procedure, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Connect-Protocol-Version", "1")
	req.Header.Set("Authorization", "Bearer mfa-1")
	if pendingCookie != "" {
		req.AddCookie(&http.Cookie{Name: identity.PendingCookieName, Value: pendingCookie})
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// TestMfaRPCLifecycle walks enroll → confirm → status → rotate →
// disable through the real transport, including the show-once
// branches.
func TestMfaRPCLifecycle(t *testing.T) {
	h, svc, created, store := newRPCStack(t)
	ctx := t.Context()

	// Enroll returns the secret and provisioning URI once.
	w := rpcPost(t, h, "EnrollTotp", "{}", "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "otpauth://totp/Tango:")

	// Status before confirm: proto JSON renders the empty message —
	// unconfirmed with zero codes left.
	w = rpcPost(t, h, "GetTotpStatus", "{}", "")
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "{}", strings.TrimSpace(w.Body.String()))

	// Confirm with a wrong code → unauthenticated (enumeration-safe).
	w = rpcPost(t, h, "ConfirmTotp", `{"code":"000000"}`, "")
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	secret, err := func() (string, error) {
		row, err := store.State(ctx, created.ID)
		if err != nil {
			return "", err
		}
		cipher, err := crypto.NewCipher(make([]byte, 32))
		if err != nil {
			return "", err
		}
		return cipher.Decrypt(row.SecretEnc)
	}()
	require.NoError(t, err)

	w = rpcPost(t, h, "ConfirmTotp", `{"code":"`+codeAt(t, secret, time.Now().UTC())+`"}`, "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"recovery_codes"`)

	// Re-confirm → already_exists.
	w = rpcPost(t, h, "ConfirmTotp", `{"code":"000000"}`, "")
	assert.Equal(t, http.StatusConflict, w.Code)

	// Status: confirmed with the full code count.
	w = rpcPost(t, h, "GetTotpStatus", "{}", "")
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"confirmed":true`)

	// Rotate with a wrong code → unauthenticated.
	w = rpcPost(t, h, "RotateRecoveryCodes", `{"code":"000000"}`, "")
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// Disable with the wrong password → unauthenticated.
	w = rpcPost(t, h, "DisableTotp", `{"password":"wrong"}`, "")
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// Disable with the right password clears the enrollment.
	w = rpcPost(t, h, "DisableTotp", `{"password":"correct-horse"}`, "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	confirmed, _, err := svc.Status(ctx, created)
	require.NoError(t, err)
	assert.False(t, confirmed)
}

// TestMfaRPCVerifyPending pins the anonymous verification: the
// pending cookie is the credential, the response rotates the session
// cookie and clears the bridge.
func TestMfaRPCVerifyPending(t *testing.T) {
	h, svc, created, store := newRPCStack(t)
	ctx := t.Context()

	// Enroll + confirm so the bridge can complete.
	secret, uri, err := svc.Enroll(ctx, created)
	require.NoError(t, err)
	assert.Contains(t, uri, "otpauth://")
	codes, err := svc.Confirm(ctx, created, codeAt(t, secret, time.Now().UTC()))
	require.NoError(t, err)
	require.NotEmpty(t, codes)

	// Seed a pending bridge row.
	pendingRaw := "pending-bridge-token-" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	require.NoError(t, store.PutPending(ctx, created.ID, hashToken(pendingRaw), identity.PendingCookieTTL, false))

	// No cookie → unauthenticated.
	w := rpcPost(t, h, "VerifyPending", `{"code":"000000"}`, "")
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// Wrong code with the cookie → unauthenticated, bridge survives.
	w = rpcPost(t, h, "VerifyPending", `{"code":"000000"}`, pendingRaw)
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// A recovery code completes the sign-in.
	w = rpcPost(t, h, "VerifyPending", `{"code":"`+codes[0]+`"}`, pendingRaw)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	cookies := w.Result().Cookies()
	require.Len(t, cookies, 2)
	assert.Equal(t, "tango_session", cookies[0].Name)
	assert.Equal(t, identity.PendingCookieName, cookies[1].Name)
	assert.Empty(t, cookies[1].Value)
}

// TestMfaPendingCarriesRemember pins that the duration requested
// before the second factor reaches the session issued after it. The
// bridge outlives the sign-in call, so dropping the flag here would
// silently shorten or lengthen the session.
func TestMfaPendingCarriesRemember(t *testing.T) {
	h, svc, created, store := newRPCStack(t)
	ctx := t.Context()

	secret, _, err := svc.Enroll(ctx, created)
	require.NoError(t, err)
	_, err = svc.Confirm(ctx, created, codeAt(t, secret, time.Now().UTC()))
	require.NoError(t, err)

	pendingRaw := "pending-remember-token-" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	require.NoError(t, store.PutPending(ctx, created.ID, hashToken(pendingRaw), identity.PendingCookieTTL, true))

	w := rpcPost(t, h, "VerifyPending", `{"code":"`+codeAt(t, secret, time.Now().UTC())+`"}`, pendingRaw)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"remember":true`,
		"the requested duration must survive the second factor")
}
