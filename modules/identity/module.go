// Package identity is the identity area: the features that establish who a
// caller is and what they may do.
//
// The area is the unit the composition root loads. It mounts every feature it
// holds on one router, so the registry names one module per area rather than
// one per feature, and a new identity feature is added here without the
// transport or the registry learning about it.
//
// The area also owns the wiring of its own services: which service a feature
// is built from, and which of them must be validated before the listener
// opens. That knowledge lives here, not in the composition root, so an area can
// be added to a running server by naming it rather than by teaching the
// registry about its internals.
package identity

import (
	"log/slog"

	"connectrpc.com/connect"
	"github.com/go-chi/chi/v5"
	"github.com/samber/do/v2"

	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/internal/kernel"
	"github.com/riipandi/tango/modules/identity/jwks"
	"github.com/riipandi/tango/modules/identity/signin"
	"github.com/riipandi/tango/modules/identity/signup"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/pkg/jwtutils"
)

// ModuleName is the name the area reports under.
const ModuleName = "identity"

// Deps are the resolved services the area's features are built from. The
// registry resolves them; the area decides which feature takes which, so a
// feature's dependencies are named here rather than at the call site.
type Deps struct {
	// KeySet supplies the keys the JSON Web Key Set publishes. It is the
	// provider interface rather than the service, so the cache in front of
	// the service is invisible to the feature.
	KeySet jwtutils.KeyProvider

	// SignIn verifies the primary credential and issues the token pair.
	SignIn *signin.Service

	// Signup creates an account from a signup token.
	Signup *signup.Service

	// Users administers the accounts.
	Users *user.Service
}

// Module mounts every identity feature.
type Module struct {
	features []kernel.Module
}

// NewModule builds the area over its dependencies.
func NewModule(deps Deps) *Module {
	return &Module{features: features(deps)}
}

// Name reports the area in composition reports and logs.
func (m *Module) Name() string { return ModuleName }

// Mount registers every feature's endpoints on the router. It runs once, at
// startup, before the listener opens.
//
// The features mount in the order features returns. chi replaces the handler
// of a pattern registered twice, so the last one wins; two features claiming
// the same route is a defect to fix, not an ordering to rely on.
func (m *Module) Mount(r chi.Router) {
	kernel.Mount(r, m.features...)
}

// MountRPC registers the procedures of every feature that serves any. The
// area forwards because the composition root names the area alone: a
// feature's procedures reach the RPC router through the same seam its HTTP
// routes do, and the transport never learns a feature's name.
func (m *Module) MountRPC(r chi.Router, opts ...connect.HandlerOption) {
	kernel.MountRPC(r, opts, m.features...)
}

// Package registers the services this area owns.
//
// The composition root applies it while the container is built, so it only
// registers: each service is constructed when something resolves it, which is
// what keeps a run from dialling a database it never reads.
var Package = do.Package(
	do.Lazy(func(i do.Injector) (*jwks.Service, error) {
		c := do.MustInvoke[*config.Config](i)
		log := do.MustInvoke[*slog.Logger](i)
		pool := do.MustInvoke[*datastore.Postgres](i)
		return jwks.NewService(*c, jwks.NewRepository(pool), log), nil
	}),

	// The published key set is read behind a cache: a client that verifies
	// many tokens must not turn each verification into a query, and a
	// rotation is still picked up within the TTL. The cache is wired here
	// rather than inside the service, because how long a key set is reused
	// is a deployment decision, not a property of the set.
	do.Lazy(func(i do.Injector) (jwtutils.KeyProvider, error) {
		service := do.MustInvoke[*jwks.Service](i)
		return jwtutils.NewCachedKeyProvider(service, jwks.KeyCacheTTL), nil
	}),

	do.Lazy(func(i do.Injector) (*signin.Service, error) {
		c := do.MustInvoke[*config.Config](i)
		log := do.MustInvoke[*slog.Logger](i)
		pool := do.MustInvoke[*datastore.Postgres](i)
		keys := do.MustInvoke[*jwks.Service](i)
		return signin.NewService(*c, signin.NewRepository(pool), keys, log), nil
	}),

	do.Lazy(func(i do.Injector) (*signup.Service, error) {
		log := do.MustInvoke[*slog.Logger](i)
		pool := do.MustInvoke[*datastore.Postgres](i)
		return signup.NewService(pool, log), nil
	}),

	do.Lazy(func(i do.Injector) (*user.Service, error) {
		log := do.MustInvoke[*slog.Logger](i)
		pool := do.MustInvoke[*datastore.Postgres](i)
		return user.NewService(pool, log), nil
	}),
)

// Mount resolves what this area's features need and builds the module the
// router mounts. It is the other half of the seam the composition root uses,
// and it is where a configuration this area cannot work with becomes a failed
// run rather than a 500 on a client's first request.
func Mount(i do.Injector) (kernel.Module, error) {
	// A missing dependency panics here and is turned back into an error by the
	// invocation that reached this provider, so only the validation below is
	// returned by hand.
	keySet := do.MustInvoke[jwtutils.KeyProvider](i)

	// The service is resolved beside the provider it is wrapped in, so a
	// configuration whose key pair cannot be read is reported by Err() rather
	// than left for the first client that fetches the key set. The cache would
	// answer it with an error on the first request instead.
	service := do.MustInvoke[*jwks.Service](i)
	if err := service.Err(); err != nil {
		return nil, err
	}

	return NewModule(Deps{
		KeySet: keySet,
		SignIn: do.MustInvoke[*signin.Service](i),
		Signup: do.MustInvoke[*signup.Service](i),
		Users:  do.MustInvoke[*user.Service](i),
	}), nil
}

// features is the area's feature list, the one place an identity feature is
// named. A feature that serves a protocol endpoint — the key set, a discovery
// document — is mounted on the router's root; one that serves the application
// API mounts itself under /api.
func features(deps Deps) []kernel.Module {
	modules := []kernel.Module{
		jwks.NewModule(deps.KeySet),
	}
	if deps.SignIn != nil {
		modules = append(modules, signin.NewModule(deps.SignIn))
	}
	if deps.Signup != nil {
		modules = append(modules, signup.NewModule(deps.Signup))
	}
	if deps.Users != nil {
		modules = append(modules, user.NewModule(deps.Users))
	}
	return modules
}
