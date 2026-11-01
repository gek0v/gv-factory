// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package telemetry

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/gv-factory/gv-factory/internal/gateway"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

// InjectHTTP injects standard W3C TraceContext headers into an outgoing HTTP request.
func InjectHTTP(ctx context.Context, header http.Header) {
	if header == nil {
		return
	}
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(header))
}

// ExtractHTTP extracts standard W3C TraceContext headers from an incoming HTTP request.
func ExtractHTTP(ctx context.Context, header http.Header) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if header == nil {
		return ctx
	}
	return otel.GetTextMapPropagator().Extract(ctx, propagation.HeaderCarrier(header))
}

// EventCarrier adapts a gateway.Event payload to the propagation.TextMapCarrier interface.
type EventCarrier struct {
	payload map[string]any
}

// NewEventCarrier creates a carrier backed by the event payload.
func NewEventCarrier(payload map[string]any) *EventCarrier {
	return &EventCarrier{payload: payload}
}

// Get returns the value associated with the passed key.
func (c *EventCarrier) Get(key string) string {
	if c == nil || c.payload == nil {
		return ""
	}

	normKey := strings.ToLower(key)

	// 1. Check inside "_trace" map
	if rawTrace, ok := c.payload["_trace"]; ok && rawTrace != nil {
		switch m := rawTrace.(type) {
		case map[string]string:
			for k, v := range m {
				if strings.ToLower(k) == normKey {
					return v
				}
			}
		case map[string]any:
			for k, v := range m {
				if strings.ToLower(k) == normKey {
					return fmt.Sprint(v)
				}
			}
		}
	}

	// 2. Direct fallback in payload
	if v, ok := c.payload[key]; ok {
		return fmt.Sprint(v)
	}

	return ""
}

// Set stores the key-value pair in the carrier's "_trace" metadata dictionary.
func (c *EventCarrier) Set(key, value string) {
	if c == nil || c.payload == nil {
		return
	}

	var traceMap map[string]string
	if rawTrace, ok := c.payload["_trace"]; ok && rawTrace != nil {
		switch m := rawTrace.(type) {
		case map[string]string:
			traceMap = m
		case map[string]any:
			traceMap = make(map[string]string, len(m)+1)
			for k, v := range m {
				traceMap[k] = fmt.Sprint(v)
			}
		}
	}

	if traceMap == nil {
		traceMap = make(map[string]string)
	}

	traceMap[key] = value
	c.payload["_trace"] = traceMap
}

// Keys lists the keys stored in this carrier.
func (c *EventCarrier) Keys() []string {
	if c == nil || c.payload == nil {
		return nil
	}

	var keys []string
	if rawTrace, ok := c.payload["_trace"]; ok && rawTrace != nil {
		switch m := rawTrace.(type) {
		case map[string]string:
			for k := range m {
				keys = append(keys, k)
			}
		case map[string]any:
			for k := range m {
				keys = append(keys, k)
			}
		}
	}
	return keys
}

// InjectPayload serializes the active trace context into a payload map under "_trace".
func InjectPayload(ctx context.Context, payload map[string]any) {
	if payload == nil {
		return
	}
	carrier := NewEventCarrier(payload)
	otel.GetTextMapPropagator().Inject(ctx, carrier)
}

// ExtractPayload restores the active trace context from a payload map containing "_trace".
func ExtractPayload(ctx context.Context, payload map[string]any) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if payload == nil {
		return ctx
	}
	carrier := NewEventCarrier(payload)
	return otel.GetTextMapPropagator().Extract(ctx, carrier)
}

// InjectEvent serializes the active trace context into the event's payload metadata.
func InjectEvent(ctx context.Context, ev *gateway.Event) {
	if ev == nil {
		return
	}
	if ev.Payload == nil {
		ev.Payload = make(map[string]any)
	}
	InjectPayload(ctx, ev.Payload)
}

// ExtractEvent restores the active trace context from the event's payload metadata.
func ExtractEvent(ctx context.Context, ev gateway.Event) context.Context {
	return ExtractPayload(ctx, ev.Payload)
}
