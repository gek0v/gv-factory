// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package telemetry

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config configures tracing, metrics, and transport observability options.
type Config struct {
	ServiceName    string  `json:"service_name"`
	ServiceVersion string  `json:"service_version"`
	Environment    string  `json:"environment"`
	OTLPEndpoint   string  `json:"otlp_endpoint"`
	OTLPProtocol   string  `json:"otlp_protocol"`
	Insecure       bool    `json:"insecure"`
	MetricsEnabled bool    `json:"metrics_enabled"`
	SamplingRatio  float64 `json:"sampling_ratio"`
}

// DefaultConfig provides sensible defaults for local development.
func DefaultConfig() Config {
	return Config{
		ServiceName:    "ov-server",
		ServiceVersion: "1.0.0",
		Environment:    "development",
		OTLPEndpoint:   "",
		OTLPProtocol:   "grpc",
		Insecure:       true,
		MetricsEnabled: true,
		SamplingRatio:  1.0,
	}
}

// ConfigFromEnv populates configuration from standard OpenTelemetry environment variables.
func ConfigFromEnv() Config {
	cfg := DefaultConfig()

	if v := os.Getenv("OTEL_SERVICE_NAME"); v != "" {
		cfg.ServiceName = v
	}
	if v := os.Getenv("OTEL_SERVICE_VERSION"); v != "" {
		cfg.ServiceVersion = v
	}
	if v := os.Getenv("OTEL_ENVIRONMENT"); v != "" {
		cfg.Environment = v
	}
	if v := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"); v != "" {
		cfg.OTLPEndpoint = v
	}
	if v := os.Getenv("OTEL_EXPORTER_OTLP_PROTOCOL"); v != "" {
		cfg.OTLPProtocol = strings.ToLower(strings.TrimSpace(v))
	}
	if v := os.Getenv("OTEL_EXPORTER_OTLP_INSECURE"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.Insecure = b
		}
	}
	if v := os.Getenv("OTEL_METRICS_ENABLED"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.MetricsEnabled = b
		}
	}
	if v := os.Getenv("OTEL_TRACES_SAMPLER_ARG"); v != "" {
		if r, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.SamplingRatio = r
		}
	}

	return cfg
}

// Validate checks the configuration parameters for logical consistency.
func (c Config) Validate() error {
	if strings.TrimSpace(c.ServiceName) == "" {
		return errors.New("service_name cannot be empty")
	}
	proto := strings.ToLower(strings.TrimSpace(c.OTLPProtocol))
	if proto != "grpc" && proto != "http" && proto != "http/protobuf" {
		return fmt.Errorf("unsupported otlp_protocol %q (must be 'grpc', 'http', or 'http/protobuf')", c.OTLPProtocol)
	}
	if c.SamplingRatio < 0.0 || c.SamplingRatio > 1.0 {
		return fmt.Errorf("sampling_ratio must be between 0.0 and 1.0, got %f", c.SamplingRatio)
	}
	return nil
}
