package identity

import (
	"context"
	"net/http"

	"connectrpc.com/authn"

	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/modules/identity/jwks"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/pkg/jwtutils"
)

// Authenticate verifies the credential an RPC or a REST route carries and
// answers the caller's claims. It is the authenticator the transport-wide
// middleware runs on both surfaces.
//
// Two credentials are accepted. A Bearer token is verified through the area's
// key service — the key and algorithm resolve on every call, so a rotation is
// picked up without a restart, and the verifier is built with that exact pair,
// so the accepted algorithm is pinned by construction. An X-API-Key header is
// validated through the API-key service — the hash lookup answers the owner's
// account read live, so a role change or a disablement takes effect on the
// next request rather than at the next token mint — and the caller it answers
// carries the machine credential kind. The kind is what the guard's session
// rule reads, which is why the keys' own surface can refuse the credential
// that would otherwise manage it.
//
// A nil machine service leaves the header answering the refusal a bad token
// does: no key can authenticate, which is the state a build without the
// service is in.
//
// The failure text names nothing a caller could aim at: a missing credential
// and a bad one answer alike.
func Authenticate(keys *jwks.Service, cfg config.Config, machine KeyValidator) authn.AuthFunc {
	verifier := jwtutils.NewAccessVerifier(keys, cfg.Auth.Issuer)
	return func(ctx context.Context, req *http.Request) (any, error) {
		if token, ok := authn.BearerToken(req); ok {
			caller, err := verifier.VerifyCaller(ctx, token)
			if err != nil {
				return nil, authn.Errorf("invalid or expired token")
			}
			return caller, nil
		}
		if presented := req.Header.Get(apiKeyHeader); presented != "" && machine != nil {
			owner, err := machine.Validate(ctx, presented)
			if err != nil {
				return nil, authn.Errorf("invalid or expired token")
			}
			return machineCaller(owner), nil
		}
		return nil, authn.Errorf("authentication required")
	}
}

// KeyValidator is the machine-credential half of the authwall: the service
// that answers the account a presented X-API-Key value acts as. It is the
// seam the identity area imports the key area through — it names the answer
// it needs, the account schema the user feature owns, not the package that
// produces it — so the two keep their own wiring.
type KeyValidator interface {
	Validate(ctx context.Context, presented string) (user.UserSchema, error)
}

// apiKeyHeader is the header a machine credential travels under.
const apiKeyHeader = "X-API-Key"

// machineCaller builds the caller a validated key answers as. The claims are
// the owner's account read live, not a token's snapshot — a role change or a
// disablement takes effect on the next request — and the session identifier
// is empty, because no session exists behind the credential: the key is its
// own proof, and its row is the durable record.
func machineCaller(owner user.UserSchema) *jwtutils.Caller {
	return &jwtutils.Caller{
		AccessClaims: jwtutils.AccessClaims{
			Email:       owner.Email,
			Username:    owner.Username,
			DisplayName: owner.DisplayName,
			IsAdmin:     owner.IsAdmin,
		},
		UserID:     owner.ID.String(),
		Credential: jwtutils.CredentialAPIKey,
	}
}
