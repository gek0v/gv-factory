// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package telemetry_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/gv-factory/gv-factory/internal/gateway"
	"github.com/gv-factory/gv-factory/internal/telemetry"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

func setupTestTracer(t *testing.T) (trace.Tracer, func()) {
	t.Helper()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	tracer := tp.Tracer("propagation-test")
	return tracer, func() {
		_ = tp.Shutdown(context.Background())
		otel.SetTracerProvider(prev)
	}
}

func TestHTTPPropagationRoundtrip(t *testing.T) {
	tracer, cleanup := setupTestTracer(t)
	defer cleanup()

	ctx, span := tracer.Start(context.Background(), "parent-http-span")
	defer span.End()

	spanCtx := span.SpanContext()
	if !spanCtx.IsValid() {
		t.Fatal("expected valid span context")
	}

	header := make(http.Header)
	telemetry.InjectHTTP(ctx, header)

	traceparent := header.Get("traceparent")
	if traceparent == "" {
		t.Fatal("expected 'traceparent' header to be injected")
	}

	extractedCtx := telemetry.ExtractHTTP(context.Background(), header)
	extractedSpan := trace.SpanFromContext(extractedCtx)
	extractedSpanCtx := extractedSpan.SpanContext()

	if !extractedSpanCtx.IsValid() {
		t.Fatal("expected valid extracted span context")
	}
	if extractedSpanCtx.TraceID() != spanCtx.TraceID() {
		t.Errorf("expected TraceID %s, got %s", spanCtx.TraceID(), extractedSpanCtx.TraceID())
	}
	if extractedSpanCtx.SpanID() != spanCtx.SpanID() {
		t.Errorf("expected SpanID %s, got %s", spanCtx.SpanID(), extractedSpanCtx.SpanID())
	}
}

func TestEventPropagationRoundtrip(t *testing.T) {
	tracer, cleanup := setupTestTracer(t)
	defer cleanup()

	ctx, span := tracer.Start(context.Background(), "nats-parent-span")
	defer span.End()

	spanCtx := span.SpanContext()
	if !spanCtx.IsValid() {
		t.Fatal("expected valid span context")
	}

	ev := &gateway.Event{
		Type:    "task.pending",
		RunID:   "run-test-1",
		TaskID:  "task-tdd",
		Payload: map[string]any{"key": "value"},
	}

	telemetry.InjectEvent(ctx, ev)

	if ev.Payload["_trace"] == nil {
		t.Fatal("expected '_trace' to be injected into event payload")
	}

	extractedCtx := telemetry.ExtractEvent(context.Background(), *ev)
	extractedSpan := trace.SpanFromContext(extractedCtx)
	extractedSpanCtx := extractedSpan.SpanContext()

	if !extractedSpanCtx.IsValid() {
		t.Fatal("expected valid extracted span context from event")
	}
	if extractedSpanCtx.TraceID() != spanCtx.TraceID() {
		t.Errorf("expected TraceID %s, got %s", spanCtx.TraceID(), extractedSpanCtx.TraceID())
	}
	if extractedSpanCtx.SpanID() != spanCtx.SpanID() {
		t.Errorf("expected SpanID %s, got %s", spanCtx.SpanID(), extractedSpanCtx.SpanID())
	}
}

func TestPropagationResilienceToMalformedHeaders(t *testing.T) {
	// 1. Invalid traceparent in HTTP header
	badHeader := make(http.Header)
	badHeader.Set("traceparent", "invalid-malformed-string")

	ctx := telemetry.ExtractHTTP(context.Background(), badHeader)
	if ctx == nil {
		t.Fatal("ExtractHTTP returned nil context on malformed header")
	}

	// 2. Empty event payload
	emptyEvent := gateway.Event{
		Type: "task.test",
	}
	ctxFromEmpty := telemetry.ExtractEvent(context.Background(), emptyEvent)
	if ctxFromEmpty == nil {
		t.Fatal("ExtractEvent returned nil context on empty event payload")
	}

	// 3. InjectEvent with nil event or nil payload
	var nilEv *gateway.Event
	telemetry.InjectEvent(context.Background(), nilEv) // Must not panic

	evWithNilPayload := &gateway.Event{}
	telemetry.InjectEvent(context.Background(), evWithNilPayload)
	if evWithNilPayload.Payload == nil {
		t.Fatal("expected Payload to be initialized on InjectEvent")
	}
}
