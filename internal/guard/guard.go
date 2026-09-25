// Package guard holds the authorization rules the transport enforces on a
// request, and the tables that declare which rule each procedure and route
// gets.
//
// It exists because a route table is the wrong place to answer "who may call
// this": the answer depends on the caller's claims, and a table that mixed
// routing with claims would be read by two audiences with nothing in common.
// The transport owns the mechanism — a refusal is written in the protocol the
// caller used — and this package owns the policy: which rule, and what a rule
// compares.
//
// The tables are the surface's contract with itself. A procedure or route
// absent from them is administrative, which is the safe default: forgetting
// to declare something refuses it rather than publishing it.
package guard

import (
	"errors"

	"github.com/riipandi/tango/pkg/jwtutils"
)

// The refusals a rule raises. They are sentinel errors because the transport
// maps them to wire codes once, so a rule never decides a status code.
var (
	// ErrUnauthenticated is a request with no authenticated caller.
	ErrUnauthenticated = errors.New("authentication required")

	// ErrAdminRequired is a caller who is authenticated but holds no
	// administrator role.
	ErrAdminRequired = errors.New("administrator role required")

	// ErrNotSelf is a self-service request whose target is another account.
	//
	// It is deliberately the same answer an unknown account produces — the
	// transport maps it to `not_found` — so a caller cannot use the refusal
	// to learn whether an account exists. The distinction survives in the
	// audit record and the server log, which an operator reads.
	ErrNotSelf = errors.New("the caller may only act on their own account")

	// ErrImpersonated is a request that belongs to the account itself,
	// refused because the caller is acting for another account. An
	// administrator's delegation is not a way to act as somebody else on a
	// surface that is theirs.
	ErrImpersonated = errors.New("an impersonated caller may not use a self-service request")
)

// Target is what a rule compares against the caller: the account a request
// names, wherever the protocol carries it.
//
// The two transports carry it differently — a procedure names it in the
// request message, a REST route in a path segment — so the rule reads a
// Target rather than a message, and one rule serves both. The alternative,
// a rule per transport, is the drift this type exists to prevent.
type Target struct {
	// Message is the decoded request message, for a procedure. It is nil on
	// the REST surface.
	Message any
	// PathParams are the route's path segments, for a REST route. It is nil
	// on the RPC surface.
	PathParams map[string]string
}

// ID reads the account identifier the request names.
//
// The path is consulted first, then the message field, both under the same
// name: a REST route calls its segment `id` and a procedure calls its field
// `id`, so one declaration covers both. The name is the proto field name —
// the snake_case key the contract declares — so a table entry reads like the
// request a client sends.
//
// A name the request does not carry reports false. The rule refuses on it
// rather than comparing against an empty string, because a guard that cannot
// find what it compares must not answer "yes".
func (t Target) ID(name string) (string, bool) {
	if name == "" {
		return "", false
	}
	if value, ok := t.PathParams[name]; ok && value != "" {
		return value, true
	}
	return stringField(t.Message, name)
}

// Rule decides whether a caller may run a request.
//
// The rule reads the caller the authenticator resolved and the target the
// request names, because a self-service rule has to compare the account the
// request names against the account the caller is. It answers nil to allow,
// and one of the sentinel errors above to refuse.
//
// A rule must not reach a database: it runs before the handler, on every
// call, and an authorization decision that needs a round trip belongs in the
// service where the transaction already is.
type Rule func(caller *jwtutils.Caller, target Target) error

// Public answers every request, caller or not.
func Public(*jwtutils.Caller, Target) error { return nil }

// Authenticated answers any verified caller, whatever their role. It is the
// rule for a request that is the caller's own by construction — one that
// reads the account from the claims and takes no target from the request.
func Authenticated(caller *jwtutils.Caller, _ Target) error {
	if caller == nil {
		return ErrUnauthenticated
	}
	return nil
}

// Admin answers a caller whose claims name an administrator. It is the
// default rule, so a request that forgets to declare one is administrative
// rather than open.
func Admin(caller *jwtutils.Caller, _ Target) error {
	if caller == nil {
		return ErrUnauthenticated
	}
	if !caller.IsAdmin {
		return ErrAdminRequired
	}
	return nil
}

// Self answers the account the request names, and nobody else.
//
// field is the name the identifier travels under: the proto field name of a
// procedure's request, or the path parameter of a REST route.
//
// An administrator does not pass by virtue of the role: a self-service
// request acts on the caller's own account, and an administrator who must act
// on another account has the administrative procedure for it. That is the
// rule upstream Pocket ID follows by construction — `/users/me` beside
// `/users/{id}` — expressed here as one rule instead of two routes.
//
// A caller that is impersonating is refused outright. The delegation exists
// so an administrator can act as another account through the administrative
// surface, not so it can use the requests that belong to the account.
//
// TODO(impersonation): the rule is enforced and tested, but no procedure
// issues a delegated token yet, so the refusal is currently unreachable in a
// running server — see pkg/jwtutils.AccessClaims.ActorID. It stays because
// the refusal has to exist before the surface that produces such a token
// does.
func Self(field string) Rule {
	return func(caller *jwtutils.Caller, target Target) error {
		if caller == nil {
			return ErrUnauthenticated
		}
		if caller.IsImpersonating() {
			return ErrImpersonated
		}
		named, ok := target.ID(field)
		if !ok {
			// The contract does not carry the name the table declares. That
			// is a wiring defect, and it is refused rather than allowed.
			return ErrNotSelf
		}
		if !caller.ActsFor(named) {
			return ErrNotSelf
		}
		return nil
	}
}
