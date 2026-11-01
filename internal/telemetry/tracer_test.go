// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package telemetry_test

import (
	"context"
	"testing"
	"time"

	"github.com/gv-factory/gv-factory/internal/telemetry"
)

func TestTracerProviderNoopFallback(t *testing.T) {
	ctx := context.Background()

	// Default config has empty OTLPEndpoint -> must fallback to noop safely
	cfg := telemetry.DefaultConfig()
	tp, err := telemetry.NewTracerProvider(ctx, cfg)
	if err != nil {
		t.Fatalf("unexpected error initializing tracer provider: %v", err)
	}
	defer func() {
		_ = tp.Shutdown(ctx)
	}()

	if !tp.IsNoop() {
		t.Error("expected IsNoop() to be true when OTLPEndpoint is empty")
	}

	tracer := tp.Tracer("test-tracer")
	if tracer == nil {
		t.Fatal("expected non-nil tracer")
	}

	// Creating span with noop tracer should not panic
	ctxSpan, span := tracer.Start(ctx, "test-span")
	if ctxSpan == nil || span == nil {
		t.Fatal("expected non-nil span and context")
	}
	span.End()
}

func TestTracerProviderUnreachableEndpointGracefulFallback(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Unreachable endpoint should not crash or hang indefinitely; it should fallback gracefully
	cfg := telemetry.DefaultConfig()
	cfg.OTLPEndpoint = "127.0.0.1:59999" // Non-existent collector port
	cfg.Insecure = true

	tp, err := telemetry.NewTracerProvider(ctx, cfg)
	// It should either return error or fallback to noop/in-memory provider with warning
	if err != nil {
		// Handled gracefully with error
		t.Logf("NewTracerProvider returned handled error: %v", err)
		return
	}
	defer func() {
		_ = tp.Shutdown(ctx)
	}()

	tracer := tp.Tracer("test-unreachable")
	_, span := tracer.Start(ctx, "test")
	span.End()
}
