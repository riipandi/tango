package transport_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/internal/health"
	"github.com/riipandi/tango/internal/kernel"
	"github.com/riipandi/tango/internal/transport"
	"github.com/riipandi/tango/modules/identity/signup"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/modules/identity/verification"
)

// The declarative constraints live in the contracts, and the validate
// interceptor the handler options carry is what enforces them — before a
// handler runs, so a feature with no pool behind it still answers the
// refusal. A valid request would reach the service (and panic over the nil
// pool these tests carry), so every case below is one the contract itself
// refuses; the accepted-shape cases live in the feature's own tests.
func TestProtovalidateRefusesTheContractViolations(t *testing.T) {
	router := transport.NewRouter(transport.Options{
		Config:  config.Default(),
		Checker: health.NewChecker(),
		Modules: []kernel.Module{
			signup.NewModule(signup.NewService(nil, nil)),
			user.NewModule(user.NewService(nil, nil, nil)),
			verification.NewModule(verification.NewService(nil, nil, nil, "", nil)),
		},
	})

	for name, tc := range map[string]struct {
		procedure string
		body      string
	}{
		"bad username": {
			"/tango.identity.v1.SignupService/Signup",
			`{"username":"a","email":"ada@example.com","password":"correct horse","token":"tok"}`,
		},
		"missing password": {
			"/tango.identity.v1.SignupService/Signup",
			`{"username":"ada","email":"ada@example.com","token":"tok"}`,
		},
		"bad email": {
			"/tango.identity.v1.SignupService/Signup",
			`{"username":"ada","email":"ada@example","password":"correct horse","token":"tok"}`,
		},
		"ttl below the window": {
			"/tango.identity.v1.SignupService/CreateSignupToken",
			`{"ttl_seconds":60}`,
		},
		"ttl above the window": {
			"/tango.identity.v1.SignupService/CreateSignupToken",
			`{"ttl_seconds":2592001}`,
		},
		"usage limit over budget": {
			"/tango.identity.v1.SignupService/CreateSignupToken",
			`{"ttl_seconds":86400,"usage_limit":1001}`,
		},
		"bad user id": {
			"/tango.identity.v1.UserService/GetUser",
			`{"id":"not-a-uuid"}`,
		},
		"empty display name": {
			"/tango.identity.v1.UserService/UpdateUser",
			`{"id":"018f0000-0000-7000-8000-000000000000","username":"ada","email":"ada@example.com","display_name":""}`,
		},
		"bad username on update": {
			"/tango.identity.v1.UserService/UpdateUser",
			`{"id":"018f0000-0000-7000-8000-000000000000","username":"a b","email":"ada@example.com","display_name":"Ada"}`,
		},
		"empty verification token": {
			"/tango.identity.v1.EmailVerificationService/VerifyEmail",
			`{"token":""}`,
		},
		"empty first name on create": {
			"/tango.identity.v1.UserService/CreateUser",
			`{"username":"ada","email":"ada@example.com","first_name":"","last_name":"Lovelace"}`,
		},
		"empty names on signup": {
			"/tango.identity.v1.SignupService/Signup",
			`{"username":"ada","email":"ada@example.com","password":"correct horse","token":"tok"}`,
		},
		"empty picture bytes on update": {
			"/tango.identity.v1.UserService/UpdateProfilePicture",
			`{"userId":"018f0000-0000-7000-8000-000000000000","data":""}`,
		},
		"picture bytes past the contract's bound": {
			"/tango.identity.v1.UserService/UpdateProfilePicture",
			`{"userId":"018f0000-0000-7000-8000-000000000000","data":"` + strings.Repeat("A", 2*1024*1024+1) + `}`,
		},
		"bad user id on reset": {
			"/tango.identity.v1.UserService/ResetProfilePicture",
			`{"userId":"not-a-uuid"}`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, rpcRequest(t, tc.procedure, tc.body))

			require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())

			var body struct {
				Code string `json:"code"`
			}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			assert.Equal(t, "invalid_argument", body.Code)
		})
	}
}
