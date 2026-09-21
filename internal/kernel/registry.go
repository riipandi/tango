package kernel

import (
	"slices"
	"strings"

	"github.com/go-chi/chi/v5"
)

// Registry collects the modules a server serves, in the order the composition
// root registers them. It keeps the transport free of module imports: the
// router mounts what the registry holds, and a new module needs no change
// outside the composition root that registered it.
type Registry struct {
	modules []Module
}

// Register adds a module. Two modules under one name are a panic, because a
// duplicate is a composition mistake the operator cannot resolve at runtime.
func (reg *Registry) Register(module Module) {
	if slices.ContainsFunc(reg.modules, func(m Module) bool { return m.Name() == module.Name() }) {
		panic("kernel: module registered twice: " + module.Name())
	}
	reg.modules = append(reg.modules, module)
}

// Modules reports the registered modules, in registration order.
func (reg *Registry) Modules() []Module {
	return slices.Clone(reg.modules)
}

// Names reports the registered module names, for a startup report.
func (reg *Registry) Names() []string {
	names := make([]string, 0, len(reg.modules))
	for _, module := range reg.modules {
		names = append(names, module.Name())
	}
	return names
}

// Mount registers every module's routes on the router, in registration order.
func (reg *Registry) Mount(r chi.Router) {
	for _, module := range reg.modules {
		module.Mount(r)
	}
}

// String renders the registry for a log line.
func (reg *Registry) String() string {
	return strings.Join(reg.Names(), ",")
}
