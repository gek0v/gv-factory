// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package telemetry_test

import (
	"context"
	"testing"

	"github.com/gv-factory/gv-factory/internal/telemetry"
)

func TestTelemetryLifecycle(t *testing.T) {
	ctx := context.Background()

	cfg := telemetry.DefaultConfig()
	tel, err := telemetry.New(ctx, cfg)
	if err != nil {
		t.Fatalf("unexpected error initializing Telemetry facade: %v", err)
	}

	if tel.TracerProvider == nil {
		t.Fatal("expected non-nil TracerProvider")
	}
	if tel.Tracer == nil {
		t.Fatal("expected non-nil Tracer")
	}
	if tel.Metrics == nil {
		t.Fatal("expected non-nil Metrics")
	}

	// In default config without OTLP collector, status should be "noop"
	if tel.Status() != "noop" {
		t.Errorf("expected status 'noop', got %q", tel.Status())
	}

	if err := tel.Shutdown(ctx); err != nil {
		t.Fatalf("unexpected error shutting down Telemetry: %v", err)
	}
}

func TestTelemetryStatusNilSafety(t *testing.T) {
	var tel *telemetry.Telemetry
	if tel.Status() != "disabled" {
		t.Errorf("expected 'disabled' for nil telemetry, got %q", tel.Status())
	}
	if err := tel.Shutdown(context.Background()); err != nil {
		t.Errorf("expected nil error on nil Shutdown, got %v", err)
	}
}
