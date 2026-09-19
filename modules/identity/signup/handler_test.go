package signup

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/internal/kernel"
	"github.com/riipandi/tango/modules/identity/password"
	"github.com/riipandi/tango/modules/identity/session"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/modules/identity/usergroup"
	"github.com/riipandi/tango/pkg/crypto"
	"github.com/riipandi/tango/pkg/jwtutils"
	"github.com/riipandi/tango/pkg/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubAccess resolves any bearer token to a principal; "revoked-"
// fails and other tokens are admin.
type stubAccess struct{ userID string }

func (s stubAccess) ResolveAccess(_ context.Context, token string) (kernel.Principal, error) {
	if token == "" || strings.HasPrefix(token, "revoked-") {
		return kernel.Principal{}, errors.New("session: invalid or expired")
	}
	return kernel.Principal{SessionID: "sess_test", UserID: s.userID, IsAdmin: true}, nil
}

// newRPCStack builds the signup service over the throwaway database
// and mounts the Connect surface with the stub authenticator.
func newRPCStack(t *testing.T) (http.Handler, *Service) {
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
		crypto.NewPasswordHasher().WithAlgorithm(crypto.AlgorithmArgon2id), nil)
	sessions := session.NewService(session.NewPostgresStore(ds), passwords, users, nil,
		session.WithAccessTokens(session.NewAccessTokenSigner(stubKeyProvider(t))))

	svc := NewService(NewPostgresStore(ds), users, usergroup.NewPostgresStore(ds), sessions, nil)
	prefix, handler := svc.RPCService(false, stubAccess{})
	mux := http.NewServeMux()
	mux.Handle(prefix, handler)
	return mux, svc
}

// stubKeyProvider serves one in-memory RSA key so access tokens sign
// without the JWKS store.
func stubKeyProvider(t *testing.T) jwtutils.KeyProvider {
	t.Helper()
	raw, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	key, err := jwk.Import(raw)
	require.NoError(t, err)
	return stubProvider{key: key}
}

type stubProvider struct{ key jwk.Key }

func (p stubProvider) SignKey(context.Context) (jwk.Key, error) { return p.key, nil }

func (p stubProvider) VerifyKeySet(context.Context) (jwk.Set, error) {
	set := jwk.NewSet()
	_ = set.AddKey(p.key)
	return set, nil
}

// stamp yields a per-call unique suffix.
func stamp() string {
	return strconv.FormatInt(time.Now().UnixNano(), 10)
}

// rpcPost posts a signup procedure with an optional bearer.
func rpcPost(t *testing.T, h http.Handler, procedure, bearer, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/tango.identity.v1.SignupService/"+procedure, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Connect-Protocol-Version", "1")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// TestSetupAvailabilityAndInitialAdmin pins the setup lifecycle: the
// fresh database accepts the first admin (with cookies), a second
// setup conflicts, and availability flips false.
func TestSetupAvailabilityAndInitialAdmin(t *testing.T) {
	h, svc := newRPCStack(t)

	w := rpcPost(t, h, "GetSetupAvailability", "", "{}")
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"available":true`)

	w = rpcPost(t, h, "SetupInitialAdmin", "", `{"username":"root_admin_`+stamp()+`","email":"root-`+stamp()+`@example.com"}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"isAdmin":true`)

	// The response rides the refresh and access cookies.
	cookies := w.Result().Cookies()
	require.Len(t, cookies, 2)
	assert.Equal(t, session.CookieName, cookies[0].Name)
	assert.Equal(t, session.AccessTokenCookieName, cookies[1].Name)

	w = rpcPost(t, h, "GetSetupAvailability", "", "{}")
	require.Equal(t, http.StatusOK, w.Code)
	// Proto JSON omits default scalars — an unavailable setup renders
	// as an empty message.
	assert.Equal(t, "{}", strings.TrimSpace(w.Body.String()))

	// A second setup → already_exists.
	w = rpcPost(t, h, "SetupInitialAdmin", "", `{"username":"root_admin_`+stamp()+`","email":"root2-`+stamp()+`@example.com"}`)
	assert.Equal(t, http.StatusConflict, w.Code)
	_ = svc
}

// TestSignupRequiresValidToken pins the token-gated signup: an
// unknown token answers one enumeration-safe rejection, a minted one
// creates the account.
func TestSignupRequiresValidToken(t *testing.T) {
	h, svc := newRPCStack(t)

	w := rpcPost(t, h, "Signup", "", `{"username":"signup_`+stamp()+`","email":"signup-`+stamp()+`@example.com","token":"bogus"}`)
	require.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "signup token is invalid or expired")

	token, raw, err := svc.CreateToken(t.Context(), CreateParams{UsageLimit: 1})
	require.NoError(t, err)
	require.NotEmpty(t, raw)
	_ = token

	payload := `{"username":"signup_` + stamp() + `","email":"signup-` + stamp() + `@example.com","token":"` + raw + `"}`
	w = rpcPost(t, h, "Signup", "", payload)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"user"`)

	// The token row recorded one use.
	spent, err := svc.ListTokens(t.Context())
	require.NoError(t, err)
	require.Len(t, spent, 1)
	assert.Equal(t, 1, spent[0].UsageCount)
}

// TestSignupTokenAdminCRUD walks the admin token surface: anonymous
// denied, admin lists/creates/deletes, show-once secret rides the
// create response only.
func TestSignupTokenAdminCRUD(t *testing.T) {
	h, svc := newRPCStack(t)

	// Anonymous admin procedure → unauthenticated.
	w := rpcPost(t, h, "ListSignupTokens", "", `{"page":1,"limit":10}`)
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	w = rpcPost(t, h, "ListSignupTokens", "admin-1", `{"page":1,"limit":10}`)
	require.Equal(t, http.StatusOK, w.Code)

	w = rpcPost(t, h, "CreateSignupToken", "admin-1", `{"ttl":"24h","usage_limit":5}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"token":"`)

	// Malformed TTL → invalid_argument.
	w = rpcPost(t, h, "CreateSignupToken", "admin-1", `{"ttl":"soon"}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	tokens, err := svc.ListTokens(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, tokens)

	w = rpcPost(t, h, "DeleteSignupToken", "admin-1", `{"token_id":"`+tokens[0].ID.String()+`"}`)
	assert.Equal(t, http.StatusOK, w.Code)

	// Unknown TypeID → not_found.
	w = rpcPost(t, h, "DeleteSignupToken", "admin-1", `{"token_id":"sgtok_00000000000000000000000000"}`)
	assert.Equal(t, http.StatusNotFound, w.Code)
}
