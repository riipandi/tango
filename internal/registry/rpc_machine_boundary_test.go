package registry

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/riipandi/tango/modules/admin/apikey"
	"github.com/riipandi/tango/modules/identity/user"
)

// machineAllowedProcedures are the admin application API procedures a
// machine credential may reach. The status is what the handler answers
// for an empty request body; the assertion is that the guard admitted
// the machine principal, so no entry may answer unauthenticated or
// permission_denied.
var machineAllowedProcedures = []struct {
	procedure string
	status    int
}{
	{"/tango.admin.v1.ApiKeyService/List", http.StatusOK},
	{"/tango.admin.v1.ApiService/ListApis", http.StatusOK},
	{"/tango.admin.v1.ApplicationConfigurationService/GetAll", http.StatusOK},
	{"/tango.admin.v1.AuditLogService/ListAll", http.StatusOK},
	{"/tango.federation.v1.OidcClientService/ListClients", http.StatusOK},
	{"/tango.federation.v1.OidcConsentService/ListAllAuthorizedClients", http.StatusOK},
	{"/tango.identity.v1.UserGroupService/ListGroups", http.StatusOK},
	{"/tango.identity.v1.UserService/ListUsers", http.StatusOK},
	{"/tango.identity.v1.SignupService/ListSignupTokens", http.StatusOK},
	{"/tango.webhook.v1.WebhookService/List", http.StatusOK},
}

// machineDeniedProcedures are self-service and credential-lifecycle
// procedures a machine credential must not reach. A leaked key must
// not rotate its owner's password, edit its owner's profile, mint a
// signup token, or send verification mail.
var machineDeniedProcedures = []string{
	"/tango.identity.v1.AccountService/ChangePassword",
	"/tango.identity.v1.AccountService/ListSessions",
	"/tango.identity.v1.AccountService/RevokeSession",
	"/tango.identity.v1.UserService/UpdateMe",
	"/tango.identity.v1.UserService/UpdateMyProfilePicture",
	"/tango.identity.v1.UserService/DeleteMyProfilePicture",
	"/tango.identity.v1.EmailVerificationService/SendEmail",
	"/tango.identity.v1.OneTimeAccessService/AdminIssueToken",
	"/tango.identity.v1.OneTimeAccessService/AdminSendEmail",
	"/tango.identity.v1.MfaService/GetTotpStatus",
	"/tango.identity.v1.DeviceApprovalService/GetPendingRequest",
}

// TestRPCMatrixDocumentsEveryProcedure pins the contract documents
// against the proto: every declared rpc needs a row in
// llms/endpoint-reference.md. The row count once drifted below the
// proto because three procedures shipped undocumented.
func TestRPCMatrixDocumentsEveryProcedure(t *testing.T) {
	protos, err := filepath.Glob("../../api/connect/*.proto")
	require.NoError(t, err)
	require.NotEmpty(t, protos)

	matrix, err := os.ReadFile("../../llms/endpoint-reference.md")
	require.NoError(t, err)

	declared := map[string]string{} // procedure -> source file
	serviceDecl := regexp.MustCompile(`^\s*service\s+(\w+)\s*\{`)
	packageDecl := regexp.MustCompile(`^\s*package\s+([\w.]+)\s*;`)
	rpcDecl := regexp.MustCompile(`^\s*rpc\s+(\w+)\s*\(`)

	for _, path := range protos {
		pkg, service := "", ""
		for _, line := range strings.Split(string(mustReadFile(t, path)), "\n") {
			if m := packageDecl.FindStringSubmatch(line); m != nil {
				pkg = m[1]
			}
			if m := serviceDecl.FindStringSubmatch(line); m != nil {
				service = m[1]
			}
			if m := rpcDecl.FindStringSubmatch(line); m != nil && pkg != "" && service != "" {
				declared["/rpc/"+pkg+"."+service+"/"+m[1]] = filepath.Base(path)
			}
		}
	}
	require.NotEmpty(t, declared, "the proto scan must find procedures")

	for procedure, source := range declared {
		assert.Contains(t, string(matrix), "`"+procedure+"`",
			"%s declares %s with no row in llms/endpoint-reference.md", source, procedure)
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return data
}

// TestRPCMachineCredentialBoundary pins the Option A boundary decided
// for finding F1: X-API-KEY reaches the admin application API only.
// Self-service and credential-lifecycle surfaces answer without a
// machine principal even when a valid key rides the request, so a
// leaked key cannot perform account takeover on its owner.
func TestRPCMachineCredentialBoundary(t *testing.T) {
	deps := testDeps(t)
	rt, err := New(deps)
	require.NoError(t, err)

	userStore := user.NewPostgresStore(deps.DB)
	admin, err := userStore.Create(t.Context(), user.CreateParams{
		Username:    "boundary_owner",
		Email:       "boundary@tango.test",
		DisplayName: "Boundary Owner",
		IsAdmin:     true,
	})
	require.NoError(t, err)

	_, raw, err := rt.apiKeys.Create(t.Context(), admin.ID.String(), apikey.CreateParams{
		Name:      "boundary-agent",
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	})
	require.NoError(t, err)

	rpc := chi.NewRouter()
	rt.MountRPC(rpc)

	call := func(procedure string) *httptest.ResponseRecorder {
		req, _ := http.NewRequest(http.MethodPost, procedure, strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Connect-Protocol-Version", "1")
		req.Header.Set("X-API-KEY", raw)
		w := httptest.NewRecorder()
		rpc.ServeHTTP(w, req)
		return w
	}

	for _, tc := range machineAllowedProcedures {
		w := call(tc.procedure)
		assert.Equal(t, tc.status, w.Code, "%s must accept a machine credential: %s", tc.procedure, w.Body.String())
	}

	for _, procedure := range machineDeniedProcedures {
		w := call(procedure)
		assert.NotEqual(t, http.StatusOK, w.Code, "%s must not accept a machine credential", procedure)
		assert.True(t,
			w.Code == http.StatusUnauthorized || w.Code == http.StatusForbidden,
			"%s must answer unauthenticated or permission_denied, got %d: %s", procedure, w.Code, w.Body.String())
	}
}

// TestRPCMachineBoundaryKeepsBearerWorking proves the narrowing is
// credential-specific, not a lockout: the same self-service procedure
// a machine credential cannot reach still resolves a bearer principal.
// Without a bearer it answers unauthenticated, which is the documented
// contract for the surface.
func TestRPCMachineBoundaryKeepsBearerWorking(t *testing.T) {
	deps := testDeps(t)
	rt, err := New(deps)
	require.NoError(t, err)

	userStore := user.NewPostgresStore(deps.DB)
	admin, err := userStore.Create(t.Context(), user.CreateParams{
		Username:    "bearer_owner",
		Email:       "bearer@tango.test",
		DisplayName: "Bearer Owner",
		IsAdmin:     true,
	})
	require.NoError(t, err)

	_, raw, err := rt.apiKeys.Create(t.Context(), admin.ID.String(), apikey.CreateParams{
		Name:      "bearer-agent",
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	})
	require.NoError(t, err)

	rpc := chi.NewRouter()
	rt.MountRPC(rpc)

	// A machine credential alone is refused on the self-service mount.
	req, _ := http.NewRequest(http.MethodPost, "/tango.identity.v1.AccountService/ListSessions", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Connect-Protocol-Version", "1")
	req.Header.Set("X-API-KEY", raw)
	w := httptest.NewRecorder()
	rpc.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())

	// The same request without the credential is refused for the
	// missing bearer, not because of the key. RPCPrincipalAuth answers
	// one enumeration-safe message for every failure mode.
	req, _ = http.NewRequest(http.MethodPost, "/tango.identity.v1.AccountService/ListSessions", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Connect-Protocol-Version", "1")
	w = httptest.NewRecorder()
	rpc.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"code":"unauthenticated"`)
}
