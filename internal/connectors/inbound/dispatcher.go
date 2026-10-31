// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package inbound

import (
	"context"
	"fmt"

	"github.com/gv-factory/gv-factory/pkg/schema"
)

// Dispatcher defines the sink for normalized inbound trigger events.
type Dispatcher interface {
	Dispatch(ctx context.Context, ev schema.TriggerEvent) error
}

// DispatcherFunc is an adapter to allow the use of ordinary functions as Dispatchers.
type DispatcherFunc func(ctx context.Context, ev schema.TriggerEvent) error

// Dispatch calls f(ctx, ev).
func (f DispatcherFunc) Dispatch(ctx context.Context, ev schema.TriggerEvent) error {
	return f(ctx, ev)
}

// ChannelDispatcher routes incoming events into an in-memory Go channel.
type ChannelDispatcher struct {
	ch chan schema.TriggerEvent
}

// NewChannelDispatcher constructs a ChannelDispatcher over the given channel.
func NewChannelDispatcher(ch chan schema.TriggerEvent) *ChannelDispatcher {
	return &ChannelDispatcher{ch: ch}
}

// Dispatch sends the event to the configured channel.
func (c *ChannelDispatcher) Dispatch(ctx context.Context, ev schema.TriggerEvent) error {
	select {
	case c.ch <- ev:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// MultiDispatcher broadcasts events across multiple destination dispatchers.
type MultiDispatcher struct {
	dispatchers []Dispatcher
}

// NewMultiDispatcher constructs a MultiDispatcher wrapping one or more dispatchers.
func NewMultiDispatcher(dispatchers ...Dispatcher) *MultiDispatcher {
	return &MultiDispatcher{dispatchers: dispatchers}
}

// Dispatch fans out the event to all registered dispatchers.
func (m *MultiDispatcher) Dispatch(ctx context.Context, ev schema.TriggerEvent) error {
	var firstErr error
	for _, d := range m.dispatchers {
		if d == nil {
			continue
		}
		if err := d.Dispatch(ctx, ev); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// GatewayPublisher captures the contract fulfilled by gateway.Gateway.
type GatewayPublisher interface {
	PublishTrigger(ctx context.Context, ev schema.TriggerEvent) error
}

// GatewayDispatcher adapts a GatewayPublisher to the Dispatcher interface.
type GatewayDispatcher struct {
	pub GatewayPublisher
}

// NewGatewayDispatcher constructs a dispatcher that forwards events to Gateway.
func NewGatewayDispatcher(pub GatewayPublisher) *GatewayDispatcher {
	return &GatewayDispatcher{pub: pub}
}

// Dispatch forwards the event to the gateway publisher.
func (g *GatewayDispatcher) Dispatch(ctx context.Context, ev schema.TriggerEvent) error {
	if g.pub == nil {
		return fmt.Errorf("gateway publisher is nil")
	}
	return g.pub.PublishTrigger(ctx, ev)
}
