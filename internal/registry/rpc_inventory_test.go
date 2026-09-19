package registry

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/riipandi/tango/internal/transport"
)

// connectServices lists every Connect service prefix the refactor
// promises; the RPC mount must carry a wildcard registration for
// each.
var connectServices = []string{
	"/tango.system.v1.HealthService/",
	"/tango.system.v1.VersionService/",
	"/tango.identity.v1.AuthService/",
	"/tango.identity.v1.UserService/",
	"/tango.identity.v1.UserGroupService/",
	"/tango.identity.v1.AccountService/",
	"/tango.identity.v1.SignupService/",
	"/tango.identity.v1.MfaService/",
	"/tango.identity.v1.OneTimeAccessService/",
	"/tango.identity.v1.EmailVerificationService/",
	"/tango.identity.v1.DeviceApprovalService/",
	"/tango.identity.v1.CustomClaimService/",
	"/tango.admin.v1.ApiKeyService/",
	"/tango.admin.v1.ApiService/",
	"/tango.admin.v1.ApplicationConfigurationService/",
	"/tango.admin.v1.AuditLogService/",
	"/tango.federation.v1.OidcClientService/",
	"/tango.federation.v1.OidcConsentService/",
	"/tango.federation.v1.ScimProviderService/",
	"/tango.webhook.v1.WebhookService/",
}

// retainedREST is the retired-then-kept HTTP surface: protocol
// contracts, email links, the worker bridge, and bare documents.
var retainedREST = []string{
	"POST /api/auth/token",
	"POST /api/auth/sign-out",
	"POST /api/auth/forgot-password",
	"POST /api/auth/reset-password",
	"POST /api/one-time-access-token/{token}",
	"POST /api/users/me/verify-email",
	"GET /api/users/{id}/profile-picture.png",
	"POST /api/webauthn/register/begin",
	"POST /api/webauthn/register/finish",
	"POST /api/webauthn/login/begin",
	"POST /api/webauthn/login/finish",
	"POST /api/device-login/requests",
	"POST /api/device-login/requests/{id}/exchange",
	"GET /api/application-configuration",
	"GET /api/oidc/clients/{clientId}/logo",
	"POST /api/oidc/token",
	"POST /api/oidc/introspect",
	"POST /api/oidc/par",
	"POST /api/oidc/device/authorize",
	"GET /api/oidc/device/info",
	"POST /api/oidc/device/verify",
	"GET /api/oidc/userinfo",
	"POST /api/oidc/end-session",
	"GET /api/oidc/interaction/{id}",
	"POST /api/oidc/interaction/{id}/approve",
}

// retiredREST lists internal routes whose ConnectRPC replacement is
// verified; the inventory must not carry them anymore.
var retiredREST = []string{
	"/api/auth/sign-in",
	"/api/auth/session",
	"/api/account",
	"/api/account/password",
	"/api/account/sessions",
	"/api/users",
	"/api/users/me",
	"/api/user-groups",
	"/api/signup",
	"/api/signup-tokens",
	"/api/mfa/totp",
	"/api/one-time-access-email",
	"/api/users/me/send-email-verification",
	"/api/users/{id}/one-time-access-token",
	"/api/api-keys",
	"/api/apis",
	"/api/api-access",
	"/api/application-configuration/all",
	"/api/application-configuration/test-email",
	"/api/audit-logs",
	"/api/custom-claims",
	"/api/oidc/clients/{clientId}/meta",
	"/api/oidc/users/me/authorized-clients",
	"/api/oidc/authorized-clients",
	"/api/scim/service-provider",
	"/api/webhooks",
	"/api/webhook-deliveries",
	"/api/version/current",
	"/api/version/latest",
	"/api/device-login/verification",
}

// TestConnectServiceInventory pins the /rpc surface: every promised
// service has a mount registration and no legacy /api wildcard snuck
// back in.
func TestConnectServiceInventory(t *testing.T) {
	rt, err := New(testDeps(t))
	require.NoError(t, err)

	// The same tree serve.go builds: the transport smoke + version
	// services, then the module-owned registrations. The health smoke
	// service is transport-internal; the inventory walks what a real
	// mount carries, so a stub registration with the same prefix
	// keeps the walk honest.
	rpc := chi.NewRouter()
	versionPrefix, versionHandler := transport.VersionRPCService(nil, rt.SessionAuthenticator())
	rpc.Handle(versionPrefix+"*", versionHandler)
	rpc.Handle("/tango.system.v1.HealthService/*", http.NotFoundHandler())
	rt.MountRPC(rpc)

	routes := strings.Join(collectRoutes(t, rpc), "\n")
	for _, service := range connectServices {
		assert.Contains(t, routes, service, "service must be registered on /rpc")
	}
	assert.NotContains(t, routes, "/api/", "the RPC tree must not carry /api paths")
}

// TestRetainedRESTInventory pins the surviving /api routes and the
// absence of every retired internal route. MountAPI registers paths
// relative to the /api group — the walk runs over the full router.
func TestRetainedRESTInventory(t *testing.T) {
	rt, err := New(testDeps(t))
	require.NoError(t, err)

	router := chi.NewRouter()
	router.Route("/api", rt.MountAPI)

	routes := collectRoutes(t, router)
	joined := strings.Join(routes, "\n")
	for _, route := range retainedREST {
		method, pattern, _ := strings.Cut(route, " ")
		assert.Contains(t, joined, method+" "+pattern, "retained route must stay mounted")
	}
	for _, pattern := range retiredREST {
		for _, route := range routes {
			_, mounted, _ := strings.Cut(route, " ")
			assert.NotEqual(t, pattern, mounted, "retired route must be gone")
		}
	}
}

// TestProtectedRPCRequiresBearer pins the transport contract: a
// protected RPC answers unauthenticated even when an access-token
// cookie rides the request — cookies never authorize an RPC.
func TestProtectedRPCRequiresBearer(t *testing.T) {
	rt, err := New(testDeps(t))
	require.NoError(t, err)

	rpc := chi.NewRouter()
	rt.MountRPC(rpc)

	body := strings.NewReader("{}")
	// The mount registers procedure patterns without the /rpc prefix
	// (the transport strips it); the request mirrors that contract.
	req, _ := http.NewRequest(http.MethodPost, "/tango.admin.v1.ApplicationConfigurationService/GetAll", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Connect-Protocol-Version", "1")
	req.AddCookie(&http.Cookie{Name: "tango_access", Value: "whatever"})

	w := httptest.NewRecorder()
	rpc.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
}
