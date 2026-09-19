package webhook

import (
	"context"
)

// ModuleName identifies the webhook module in the registry.
const ModuleName = "webhook"

// Module is the webhook feature: endpoint CRUD, delivery logs, and
// the outbox event sink. The whole surface serves ConnectRPC
// exclusively (see handler_rpc.go) — the composition root wraps the
// mount with the admin guard.
type Module struct {
	service *Service
}

// New builds the module on top of the service.
func New(service *Service, _opts ...Option) *Module {
	if service == nil {
		panic("webhook: nil service")
	}
	return &Module{service: service}
}

// Option configures the webhook module at construction.
type Option func(*Module)

// Store exposes the persistence layer for the recurring log-pruning
// job; the module itself never needs a wider surface.
func (m *Module) Store() Store { return m.service.store }

// Emit fans an application event out to its subscribers. It is the
// event sink the composition root hands to other modules, so a domain
// package never imports this one directly.
func (m *Module) Emit(ctx context.Context, event string, payload map[string]any) error {
	return m.service.Emit(ctx, event, payload)
}

// Name identifies the module.
func (*Module) Name() string { return ModuleName }
