package registry

import (
	"log/slog"

	"github.com/samber/do/v2"

	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/internal/kernel"
	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/modules/identity/jwks"
	"github.com/riipandi/tango/pkg/jwtutils"
)

// modules registers the services a module owns and the list the router mounts.
// This is the module half of the composition root: it names every area the
// application serves, and it reaches infrastructure only by invoking it from
// the container.
//
// An area owns its own features, so its `features` list is the one place a
// feature is named. What is named here is the area and the services that area
// needs from outside itself.
func modules() func(do.Injector) {
	return do.Package(
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
		//
		// The service is still resolved on its own by mountedModules, which is
		// what turns a broken configuration into a failed run: the cache would
		// answer it with an error on the first request instead.
		do.Lazy(func(i do.Injector) (jwtutils.KeyProvider, error) {
			service := do.MustInvoke[*jwks.Service](i)
			return jwtutils.NewCachedKeyProvider(service, jwks.KeyCacheTTL), nil
		}),
	)
}

// mountedModules builds the module list the router mounts, one entry per area.
//
// It resolves what each area needs before handing it over, so a configuration
// an area cannot work with fails the run here, before the listener opens,
// rather than as a 500 on the first request that reaches it.
func mountedModules(i do.Injector) ([]kernel.Module, error) {
	// A missing dependency panics here and is turned back into an error by the
	// invocation that reached this provider, so only the validation below is
	// returned by hand.
	keySet := do.MustInvoke[jwtutils.KeyProvider](i)

	// The service is resolved beside the provider it is wrapped in, so a
	// configuration whose key pair cannot be read is reported by Err() rather
	// than left for the first client that fetches the key set.
	service := do.MustInvoke[*jwks.Service](i)
	if err := service.Err(); err != nil {
		return nil, err
	}

	return []kernel.Module{
		identity.NewModule(identity.Deps{KeySet: keySet}),
	}, nil
}
