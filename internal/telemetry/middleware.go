// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package telemetry

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

type statusRecorder struct {
	http.ResponseWriter
	statusCode  int
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(code int) {
	if !r.wroteHeader {
		r.statusCode = code
		r.wroteHeader = true
		r.ResponseWriter.WriteHeader(code)
	}
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if !r.wroteHeader {
		r.WriteHeader(http.StatusOK)
	}
	return r.ResponseWriter.Write(b)
}

func (r *statusRecorder) Flush() {
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// NewHTTPMiddleware wraps an http.Handler with OpenTelemetry tracing and Prometheus RED metrics.
func NewHTTPMiddleware(metrics *Metrics, tracer trace.Tracer) func(http.Handler) http.Handler {
	if tracer == nil {
		tracer = noop.NewTracerProvider().Tracer("ov-server.http")
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			startTime := time.Now()

			normPath := normalizePath(r.URL.Path)
			if normPath == "" {
				normPath = normalizePath(r.RequestURI)
			}

			// Extract incoming W3C traceparent/tracestate
			ctx := ExtractHTTP(r.Context(), r.Header)

			quiet := isQuietEndpoint(normPath)

			var span trace.Span
			if !quiet {
				spanName := fmt.Sprintf("HTTP %s %s", r.Method, normPath)
				ctx, span = tracer.Start(
					ctx,
					spanName,
					trace.WithSpanKind(trace.SpanKindServer),
					trace.WithAttributes(
						semconv.HTTPRequestMethodKey.String(r.Method),
						semconv.URLPath(normPath),
						attribute.String("http.route", normPath),
						attribute.String("user_agent.original", r.UserAgent()),
					),
				)
				defer span.End()

				// Inject outgoing trace context in response headers for client diagnostics
				InjectHTTP(ctx, w.Header())
			}

			recorder := &statusRecorder{
				ResponseWriter: w,
				statusCode:     http.StatusOK,
			}

			// Forward request with tracing context
			next.ServeHTTP(recorder, r.WithContext(ctx))

			duration := time.Since(startTime)

			if span != nil {
				span.SetAttributes(semconv.HTTPResponseStatusCode(recorder.statusCode))
				if recorder.statusCode >= http.StatusInternalServerError {
					span.SetStatus(codes.Error, fmt.Sprintf("HTTP %d", recorder.statusCode))
				} else {
					span.SetStatus(codes.Ok, "OK")
				}
			}

			if metrics != nil {
				metrics.RecordHTTPRequest(r.Method, normPath, recorder.statusCode, duration)
			}
		})
	}
}

func normalizePath(rawPath string) string {
	if rawPath == "" {
		return "/"
	}
	u, err := url.Parse(rawPath)
	p := rawPath
	if err == nil {
		p = u.Path
	}
	if p == "" {
		p = "/"
	}
	if len(p) > 1 && strings.HasSuffix(p, "/") {
		p = strings.TrimSuffix(p, "/")
	}
	return p
}

func isQuietEndpoint(path string) bool {
	switch path {
	case "/health", "/healthz", "/readyz", "/metrics", "/api/v1/health":
		return true
	default:
		return false
	}
}
