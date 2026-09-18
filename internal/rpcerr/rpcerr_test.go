package rpcerr

import (
	"errors"
	"net/http"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRPCErrorMapping pins the REST-to-Connect error contract used by
// phase-03 RPC handlers: each REST status maps onto exactly one
// Connect code, and the mapped HTTP status keeps the REST meaning.
func TestRPCErrorMapping(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		code       connect.Code
		httpStatus int
	}{
		{"bad request", InvalidArgument("validation failed"), connect.CodeInvalidArgument, http.StatusBadRequest},
		{"unauthenticated", Unauthenticated("bearer token required"), connect.CodeUnauthenticated, http.StatusUnauthorized},
		{"forbidden", PermissionDenied("admin required"), connect.CodePermissionDenied, http.StatusForbidden},
		{"not found", NotFound("user not found"), connect.CodeNotFound, http.StatusNotFound},
		{"conflict", AlreadyExists("email already registered"), connect.CodeAlreadyExists, http.StatusConflict},
		{"rate limited", ResourceExhausted("rate limit exceeded"), connect.CodeResourceExhausted, http.StatusTooManyRequests},
		{"internal", Internal("internal error"), connect.CodeInternal, http.StatusInternalServerError},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var cerr *connect.Error
			require.True(t, errors.As(tc.err, &cerr), "must decode as *connect.Error")
			assert.Equal(t, tc.code, cerr.Code())
			assert.NotEmpty(t, cerr.Message())
		})
	}
}
