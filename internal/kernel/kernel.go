// Package kernel holds the contract every feature module implements, so the
// transport can serve a route without knowing which module owns it.
package kernel

import (
	"connectrpc.com/connect"
	"github.com/go-chi/chi/v5"
)

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

// RPCModule is a module that also serves ConnectRPC procedures.
//
// It is a separate interface rather than a second method on Module, so a module
// that serves no procedure says so by not implementing it: the transport asks
// with a type assertion instead of calling a method that would have to do
// nothing.
type RPCModule interface {
	Module
	// MountRPC registers the module's procedures on the RPC router. The router
	// is mounted with the RPC prefix stripped, so the paths are the procedure
	// paths a generated Connect handler answers. MountRPC runs once, before the
	// listener opens.
	//
	// The handler options are the transport's, and a module passes them to every
	// generated handler it registers. They carry the shared JSON codec — the one
	// that serializes snake_case, so a procedure answers in the same field names
	// its REST twin writes — and the panic boundary. A module that registers a
	// handler without them answers in protobuf's default camelCase instead.
	MountRPC(r chi.Router, opts ...connect.HandlerOption)
}

// MountRPC registers the procedures of every module that serves any. A module
// without procedures is skipped, so one list can carry both kinds.
func MountRPC(r chi.Router, opts []connect.HandlerOption, modules ...Module) {
	for _, module := range modules {
		if rpc, ok := module.(RPCModule); ok {
			rpc.MountRPC(r, opts...)
		}
	}
}
