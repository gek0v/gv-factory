// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package telemetry

import (
	"context"

	"go.opentelemetry.io/otel/trace"
)

// Telemetry provides a unified lifecycle facade over tracing and metrics providers.
type Telemetry struct {
	TracerProvider *TracerProvider
	Tracer         trace.Tracer
	Metrics        *Metrics
	Config         Config
}

// New initializes the entire telemetry subsystem based on the given configuration.
func New(ctx context.Context, cfg Config) (*Telemetry, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	tp, err := NewTracerProvider(ctx, cfg)
	if err != nil {
		return nil, err
	}

	tracer := tp.Tracer(cfg.ServiceName)

	var metrics *Metrics
	if cfg.MetricsEnabled {
		metrics = NewMetrics()
	}

	return &Telemetry{
		TracerProvider: tp,
		Tracer:         tracer,
		Metrics:        metrics,
		Config:         cfg,
	}, nil
}

// Status returns a human-readable health/operational status string ("healthy", "noop", or "disabled").
func (t *Telemetry) Status() string {
	if t == nil {
		return "disabled"
	}
	if t.TracerProvider == nil || t.TracerProvider.IsNoop() {
		return "noop"
	}
	return "healthy"
}

// Shutdown gracefully flushes and terminates telemetry providers.
func (t *Telemetry) Shutdown(ctx context.Context) error {
	if t == nil || t.TracerProvider == nil {
		return nil
	}
	return t.TracerProvider.Shutdown(ctx)
}
