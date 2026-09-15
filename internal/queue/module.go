package queue

import (
	"context"
	"fmt"
)

// Module is the kernel lifecycle adapter: Start runs the dispatcher,
// Stop waits for workers to drain. Queue registration happens at build
// time via registry.Deps.
type Module struct {
	client *Client
}

// New wraps a client for kernel registration.
func New(client *Client) *Module {
	return &Module{client: client}
}

// Name implements kernel.Module.
func (m *Module) Name() string {
	return "queue"
}

// Start runs the queue dispatcher.
func (m *Module) Start(ctx context.Context) error {
	m.client.Start(ctx)
	return nil
}

// Stop drains running tasks. A false result means workers were still
// busy when the context deadline hit.
func (m *Module) Stop(ctx context.Context) error {
	if !m.client.Stop(ctx) {
		return fmt.Errorf("queue: workers still running after stop deadline")
	}
	return nil
}
