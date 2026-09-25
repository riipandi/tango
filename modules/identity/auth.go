package identity

import (
	"context"
	"net/http"

	"connectrpc.com/authn"

	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/modules/identity/jwks"
	"github.com/riipandi/tango/pkg/jwtutils"
)

// Authenticate verifies the Bearer token an RPC carries and answers the
// caller's claims. It is the authenticator the transport-wide bearer
// middleware runs: the key and algorithm resolve on every call through the
// area's key service, so a rotation is picked up without a restart, and the
// verifier is built with that exact pair, so the accepted algorithm is pinned
// by construction.
//
// The failure text names nothing a caller could aim at: a missing token and a
// bad one answer alike.
func Authenticate(keys *jwks.Service, cfg config.Config) authn.AuthFunc {
	verifier := jwtutils.NewAccessVerifier(keys, cfg.Auth.Issuer)
	return func(ctx context.Context, req *http.Request) (any, error) {
		token, ok := authn.BearerToken(req)
		if !ok {
			return nil, authn.Errorf("authentication required")
		}
		caller, err := verifier.VerifyCaller(ctx, token)
		if err != nil {
			return nil, authn.Errorf("invalid or expired token")
		}
		return caller, nil
	}
}
