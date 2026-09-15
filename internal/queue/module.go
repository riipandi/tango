package queue

import (
	"context"
	"fmt"
)

// Module adapts the queue client to the kernel lifecycle.
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

// Stop drains running tasks.
func (m *Module) Stop(ctx context.Context) error {
	if !m.client.Stop(ctx) {
		return fmt.Errorf("queue: workers still running after stop deadline")
	}
	return nil
}
