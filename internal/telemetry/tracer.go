// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package telemetry

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

func init() {
	// Initialize standard W3C TraceContext and Baggage propagators globally
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
}

// TracerProvider manages OpenTelemetry tracer lifecycle and trace exporters.
type TracerProvider struct {
	provider trace.TracerProvider
	isNoop   bool
	shutdown func(context.Context) error
}

// NewTracerProvider initializes an OpenTelemetry TracerProvider with OTLP exporter or noop fallback.
func NewTracerProvider(ctx context.Context, cfg Config) (*TracerProvider, error) {
	// If no OTLP collector endpoint specified, return a lightweight noop provider
	if strings.TrimSpace(cfg.OTLPEndpoint) == "" {
		noopProvider := noop.NewTracerProvider()
		otel.SetTracerProvider(noopProvider)
		return &TracerProvider{
			provider: noopProvider,
			isNoop:   true,
			shutdown: func(context.Context) error { return nil },
		}, nil
	}

	// Build resource attributes
	res, err := resource.New(ctx,
		resource.WithSchemaURL(semconv.SchemaURL),
		resource.WithAttributes(
			semconv.ServiceNameKey.String(cfg.ServiceName),
			semconv.ServiceVersionKey.String(cfg.ServiceVersion),
			attribute.String("deployment.environment", cfg.Environment),
		),
	)
	if err != nil {
		res = resource.Default()
	}

	// Create OTLP exporter
	expCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	var client otlptrace.Client
	proto := strings.ToLower(strings.TrimSpace(cfg.OTLPProtocol))

	if proto == "http" || proto == "http/protobuf" {
		opts := []otlptracehttp.Option{
			otlptracehttp.WithEndpoint(cfg.OTLPEndpoint),
		}
		if cfg.Insecure {
			opts = append(opts, otlptracehttp.WithInsecure())
		}
		client = otlptracehttp.NewClient(opts...)
	} else {
		opts := []otlptracegrpc.Option{
			otlptracegrpc.WithEndpoint(cfg.OTLPEndpoint),
		}
		if cfg.Insecure {
			opts = append(opts, otlptracegrpc.WithInsecure())
		}
		client = otlptracegrpc.NewClient(opts...)
	}

	exporter, err := otlptrace.New(expCtx, client)
	if err != nil {
		log.Printf("Warning: failed to initialize OTLP trace exporter (%v); falling back to no-op tracer", err)
		noopProvider := noop.NewTracerProvider()
		otel.SetTracerProvider(noopProvider)
		return &TracerProvider{
			provider: noopProvider,
			isNoop:   true,
			shutdown: func(context.Context) error { return nil },
		}, nil
	}

	sampler := sdktrace.AlwaysSample()
	if cfg.SamplingRatio < 1.0 {
		sampler = sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SamplingRatio))
	}

	sdkTP := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	)

	otel.SetTracerProvider(sdkTP)

	return &TracerProvider{
		provider: sdkTP,
		isNoop:   false,
		shutdown: sdkTP.Shutdown,
	}, nil
}

// Tracer returns a named tracer instance from the underlying provider.
func (tp *TracerProvider) Tracer(name string) trace.Tracer {
	if tp == nil || tp.provider == nil {
		return noop.NewTracerProvider().Tracer(name)
	}
	return tp.provider.Tracer(name)
}

// IsNoop indicates whether the provider is operating in no-op fallback mode.
func (tp *TracerProvider) IsNoop() bool {
	if tp == nil {
		return true
	}
	return tp.isNoop
}

// Shutdown flushes pending spans and releases provider resources.
func (tp *TracerProvider) Shutdown(ctx context.Context) error {
	if tp == nil || tp.shutdown == nil {
		return nil
	}
	if err := tp.shutdown(ctx); err != nil {
		return fmt.Errorf("tracer shutdown error: %w", err)
	}
	return nil
}
