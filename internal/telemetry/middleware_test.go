// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package telemetry_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gv-factory/gv-factory/internal/telemetry"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestHTTPMiddlewareRecordsTracesAndMetrics(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	tracer := tp.Tracer("http-test")
	metrics := telemetry.NewMetrics()

	mw := telemetry.NewHTTPMiddleware(metrics, tracer)

	// Sample application handler returning 201 Created
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"status":"created"}`))
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks?query=param", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	res := rec.Result()
	defer res.Body.Close()

	if res.StatusCode != http.StatusCreated {
		t.Fatalf("expected status 201, got %d", res.StatusCode)
	}

	// Response header must contain traceparent
	traceparent := res.Header.Get("traceparent")
	if traceparent == "" {
		t.Error("expected 'traceparent' header in HTTP response")
	}

	// Span must be created
	spans := sr.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 ended span, got %d", len(spans))
	}

	s := spans[0]
	if s.Name() != "HTTP POST /api/v1/tasks" {
		t.Errorf("expected span name 'HTTP POST /api/v1/tasks', got %q", s.Name())
	}

	// Verify metrics registered
	metricsHandler := metrics.Handler()
	metricsRec := httptest.NewRecorder()
	metricsReq := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	metricsHandler.ServeHTTP(metricsRec, metricsReq)

	metricsBody := metricsRec.Body.String()
	expectedMetric := `ov_http_requests_total{method="POST",path="/api/v1/tasks",status="201"} 1`
	if !containsString(metricsBody, expectedMetric) {
		t.Errorf("expected metric %q in metrics output:\n%s", expectedMetric, metricsBody)
	}
}

func TestHTTPMiddlewareSkipsHealthAndMetricsSpans(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	tracer := tp.Tracer("http-test")
	metrics := telemetry.NewMetrics()

	mw := telemetry.NewHTTPMiddleware(metrics, tracer)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	healthReqs := []string{"/healthz", "/health", "/readyz", "/metrics"}
	for _, path := range healthReqs {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}

	// Health and metrics checks must not create noise spans
	spans := sr.Ended()
	if len(spans) != 0 {
		t.Errorf("expected 0 spans for healthz/metrics endpoints, got %d", len(spans))
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || (len(s) > 0 && stringContains(s, substr)))
}

func stringContains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
