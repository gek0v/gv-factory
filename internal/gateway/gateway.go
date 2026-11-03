// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gv-factory/gv-factory/pkg/schema"
	"github.com/nats-io/nats.go"
)

// Config defines connection settings for the gateway.
type Config struct {
	NATSURL        string
	StreamName     string
	ConnectTimeout time.Duration
	DisableNATS    bool
}

// Gateway orchestrates external webhooks (Svix), message bus (NATS), and client WebSockets.
type Gateway struct {
	mu          sync.RWMutex
	subscribers map[string][]chan Event
	nc          *nats.Conn
	js          nats.JetStreamContext
	hub         *Hub
	isInMemory  bool
	closed      bool
	closeOnce   sync.Once
}

// NewGateway creates an initialized communications gateway with default configuration.
func NewGateway() *Gateway {
	return NewGatewayWithConfig(Config{})
}

// NewGatewayWithConfig creates an initialized communications gateway using custom config.
func NewGatewayWithConfig(cfg Config) *Gateway {
	g := &Gateway{
		subscribers: make(map[string][]chan Event),
		hub:         NewHub(),
		isInMemory:  true,
	}

	if cfg.DisableNATS {
		return g
	}

	natsURL := strings.TrimSpace(cfg.NATSURL)
	if natsURL == "" {
		natsURL = strings.TrimSpace(os.Getenv("NATS_URL"))
	}
	if natsURL == "" {
		natsURL = "nats://localhost:4222"
	}

	connectTimeout := cfg.ConnectTimeout
	if connectTimeout <= 0 {
		connectTimeout = 250 * time.Millisecond
	}

	nc, err := nats.Connect(
		natsURL,
		nats.Timeout(connectTimeout),
		nats.MaxReconnects(2),
		nats.ReconnectWait(100*time.Millisecond),
	)
	if err != nil {
		g.isInMemory = true
		return g
	}

	js, err := nc.JetStream()
	if err != nil {
		nc.Close()
		g.isInMemory = true
		return g
	}

	g.nc = nc
	g.js = js
	g.isInMemory = false
	return g
}

// IsInMemory returns true if the gateway is running in local in-memory fallback mode.
func (g *Gateway) IsInMemory() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.isInMemory
}

// Hub returns the attached WebSocket / stream broadcast hub.
func (g *Gateway) Hub() *Hub {
	return g.hub
}

// Publish broadcasts an event across active subscribers and JetStream stream.
func (g *Gateway) Publish(ctx context.Context, topic string, ev Event) error {
	if g == nil {
		return nil
	}
	if ev.Type == "" {
		ev.Type = topic
	}
	return g.PublishTaskEvent(ctx, ev)
}

// PublishTaskEvent serializes the task event, stamps timestamp, and broadcasts
// across in-memory subscribers, the WebSocket hub, and NATS JetStream (if active).
func (g *Gateway) PublishTaskEvent(ctx context.Context, ev Event) error {
	if g == nil {
		return nil
	}
	g.mu.Lock()
	if g.closed {
		g.mu.Unlock()
		return fmt.Errorf("gateway is closed")
	}
	if ev.Timestamp == 0 {
		ev.Timestamp = time.Now().UnixMilli()
	}

	for pattern, chs := range g.subscribers {
		if matchesTopic(pattern, ev.Type) {
			for _, ch := range chs {
				select {
				case ch <- ev:
				default:
				}
			}
		}
	}
	g.mu.Unlock()

	data, err := json.Marshal(ev)
	if err == nil && g.hub != nil {
		g.hub.BroadcastRun(ev.RunID, data)
	}

	g.mu.RLock()
	js := g.js
	g.mu.RUnlock()

	if js != nil {
		_, pubErr := js.Publish(ev.Type, data)
		if pubErr != nil {
			return fmt.Errorf("jetstream publish error: %w", pubErr)
		}
	}

	return nil
}

// SubscribeTaskEvents registers a receiver channel for events matching the given topic pattern.
func (g *Gateway) SubscribeTaskEvents(ctx context.Context, topic string, ch chan Event) (func(), error) {
	if ch == nil {
		return nil, fmt.Errorf("subscriber channel cannot be nil")
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	if g.closed {
		return nil, fmt.Errorf("gateway is closed")
	}

	g.subscribers[topic] = append(g.subscribers[topic], ch)

	unsub := func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		chs := g.subscribers[topic]
		for i, c := range chs {
			if c == ch {
				g.subscribers[topic] = append(chs[:i], chs[i+1:]...)
				break
			}
		}
	}

	return unsub, nil
}

// Close gracefully releases connections, cleans up subscribers, and closes the WebSocket hub.
func (g *Gateway) Close() error {
	g.closeOnce.Do(func() {
		g.mu.Lock()
		g.closed = true
		g.subscribers = make(map[string][]chan Event)
		nc := g.nc
		g.nc = nil
		g.js = nil
		g.mu.Unlock()

		if g.hub != nil {
			g.hub.Close()
		}

		if nc != nil {
			nc.Drain()
			nc.Close()
		}
	})
	return nil
}

// PublishTrigger normalizes and broadcasts a trigger event across active subscribers,
// the WebSocket hub, and NATS JetStream (topic: factory.triggers.<source>).
func (g *Gateway) PublishTrigger(ctx context.Context, ev schema.TriggerEvent) error {
	topic := TriggerTopicForSource(ev.Source)
	payload := ev.Payload
	if payload == nil {
		payload = make(map[string]any)
	}
	payload["source"] = ev.Source
	payload["event_type"] = ev.EventType
	if ev.Repository != "" {
		payload["repository"] = ev.Repository
	}
	if ev.Reference != "" {
		payload["reference"] = ev.Reference
	}
	if ev.IssueNumber > 0 {
		payload["issue_number"] = ev.IssueNumber
	}
	if ev.PRNumber > 0 {
		payload["pr_number"] = ev.PRNumber
	}
	if ev.Sender != "" {
		payload["sender"] = ev.Sender
	}

	ts := ev.UnixMilli()
	if ts == 0 {
		ts = time.Now().UnixMilli()
	}

	return g.PublishTaskEvent(ctx, Event{
		Type:      topic,
		RunID:     ev.ID,
		Payload:   payload,
		Timestamp: ts,
	})
}

// HandleWebhook processes inbound webhooks from external providers.
func (g *Gateway) HandleWebhook(provider string, payload []byte) error {
	fmt.Printf("Received inbound webhook from %s\n", provider)
	return nil
}
