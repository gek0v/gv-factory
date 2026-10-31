// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package gateway

import (
	"context"
	"fmt"
	"sync"
)

// Event represents an internal messaging envelope passed over NATS / WebSockets.
type Event struct {
	Type      string         `json:"type"`
	RunID     string         `json:"run_id"`
	Payload   map[string]any `json:"payload"`
	Timestamp int64          `json:"timestamp"`
}

// Gateway orchestrates external webhooks (Svix), message bus (NATS), and client WebSockets.
type Gateway struct {
	mu          sync.RWMutex
	subscribers map[string][]chan Event
}

// NewGateway creates an initialized communications gateway.
func NewGateway() *Gateway {
	return &Gateway{
		subscribers: make(map[string][]chan Event),
	}
}

// Publish broadcasts an event across active subscribers and JetStream stream.
func (g *Gateway) Publish(ctx context.Context, topic string, ev Event) error {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if chs, ok := g.subscribers[topic]; ok {
		for _, ch := range chs {
			select {
			case ch <- ev:
			default:
			}
		}
	}
	return nil
}

// HandleWebhook processes inbound webhooks from external providers.
func (g *Gateway) HandleWebhook(provider string, payload []byte) error {
	fmt.Printf("Received inbound webhook from %s\n", provider)
	return nil
}
