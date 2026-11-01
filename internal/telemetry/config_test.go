// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package telemetry_test

import (
	"testing"

	"github.com/gv-factory/gv-factory/internal/telemetry"
)

func TestDefaultConfig(t *testing.T) {
	cfg := telemetry.DefaultConfig()

	if cfg.ServiceName != "ov-server" {
		t.Errorf("expected ServiceName 'ov-server', got %q", cfg.ServiceName)
	}
	if cfg.ServiceVersion != "1.0.0" {
		t.Errorf("expected ServiceVersion '1.0.0', got %q", cfg.ServiceVersion)
	}
	if cfg.Environment != "development" {
		t.Errorf("expected Environment 'development', got %q", cfg.Environment)
	}
	if cfg.OTLPProtocol != "grpc" {
		t.Errorf("expected OTLPProtocol 'grpc', got %q", cfg.OTLPProtocol)
	}
	if !cfg.Insecure {
		t.Errorf("expected Insecure true by default in dev, got %v", cfg.Insecure)
	}
	if !cfg.MetricsEnabled {
		t.Errorf("expected MetricsEnabled true by default, got %v", cfg.MetricsEnabled)
	}
	if cfg.SamplingRatio != 1.0 {
		t.Errorf("expected SamplingRatio 1.0, got %f", cfg.SamplingRatio)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("DefaultConfig failed validation: %v", err)
	}
}

func TestConfigFromEnv(t *testing.T) {
	t.Setenv("OTEL_SERVICE_NAME", "custom-factory")
	t.Setenv("OTEL_SERVICE_VERSION", "2.1.0")
	t.Setenv("OTEL_ENVIRONMENT", "production")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "collector.monitoring:4317")
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "http/protobuf")
	t.Setenv("OTEL_EXPORTER_OTLP_INSECURE", "false")
	t.Setenv("OTEL_TRACES_SAMPLER_ARG", "0.25")
	t.Setenv("OTEL_METRICS_ENABLED", "false")

	cfg := telemetry.ConfigFromEnv()

	if cfg.ServiceName != "custom-factory" {
		t.Errorf("expected 'custom-factory', got %q", cfg.ServiceName)
	}
	if cfg.ServiceVersion != "2.1.0" {
		t.Errorf("expected '2.1.0', got %q", cfg.ServiceVersion)
	}
	if cfg.Environment != "production" {
		t.Errorf("expected 'production', got %q", cfg.Environment)
	}
	if cfg.OTLPEndpoint != "collector.monitoring:4317" {
		t.Errorf("expected 'collector.monitoring:4317', got %q", cfg.OTLPEndpoint)
	}
	if cfg.OTLPProtocol != "http/protobuf" {
		t.Errorf("expected 'http/protobuf', got %q", cfg.OTLPProtocol)
	}
	if cfg.Insecure {
		t.Errorf("expected Insecure false, got true")
	}
	if cfg.SamplingRatio != 0.25 {
		t.Errorf("expected SamplingRatio 0.25, got %f", cfg.SamplingRatio)
	}
	if cfg.MetricsEnabled {
		t.Errorf("expected MetricsEnabled false, got true")
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("ConfigFromEnv failed validation: %v", err)
	}
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*telemetry.Config)
		wantErr bool
	}{
		{
			name: "empty service name",
			mutate: func(c *telemetry.Config) {
				c.ServiceName = ""
			},
			wantErr: true,
		},
		{
			name: "invalid protocol",
			mutate: func(c *telemetry.Config) {
				c.OTLPProtocol = "ftp"
			},
			wantErr: true,
		},
		{
			name: "sampling ratio out of range high",
			mutate: func(c *telemetry.Config) {
				c.SamplingRatio = 1.5
			},
			wantErr: true,
		},
		{
			name: "sampling ratio out of range low",
			mutate: func(c *telemetry.Config) {
				c.SamplingRatio = -0.1
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := telemetry.DefaultConfig()
			tt.mutate(&cfg)
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}
