// Package kernel holds the contract every feature module implements, so the
// transport can serve a route without knowing which module owns it.
package kernel

import "github.com/go-chi/chi/v5"

// Module is one feature slice the server mounts. A module owns its package
// (schema, repository, service, handlers) and registers its endpoints through
// Mount, so no route list outside the module can drift from its handlers.
type Module interface {
	// Name reports the module in composition reports and logs. It is
	// diagnostic: routes are mounted in registration order, not by name.
	Name() string
	// Mount registers the module's endpoints on the router. Mount runs once at
	// startup, before the listener opens, so a registration failure is a
	// construction failure of the server, not a 500 on the first request.
	Mount(r chi.Router)
}

// Mount registers every module on the router in the order given. chi replaces
// the handler of a pattern registered twice, so a module that must shadow
// another's route registers last.
func Mount(r chi.Router, modules ...Module) {
	for _, module := range modules {
		module.Mount(r)
	}
}
