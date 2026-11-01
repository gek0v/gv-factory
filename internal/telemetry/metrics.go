// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package telemetry

import (
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics maintains isolated Prometheus collectors for all platform subsystems.
type Metrics struct {
	registry *prometheus.Registry

	// HTTP RED collectors
	HTTPRequestsTotal   *prometheus.CounterVec
	HTTPRequestDuration *prometheus.HistogramVec

	// DAG Execution collectors
	FactoryRunsTotal *prometheus.CounterVec
	DAGStepDuration  *prometheus.HistogramVec

	// Worker & Queue capacity collectors
	WorkersActiveCount    prometheus.Gauge
	QueueDepth            *prometheus.GaugeVec
	WorkerSandboxesActive prometheus.Gauge

	// LLM Token & Cost accounting collectors
	LLMTokensConsumedTotal *prometheus.CounterVec
	LLMEstimatedCostUSD    *prometheus.CounterVec
}

// NewMetrics initializes an isolated Prometheus registry and registers all required platform collectors.
func NewMetrics() *Metrics {
	reg := prometheus.NewRegistry()

	m := &Metrics{
		registry: reg,

		HTTPRequestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "ov_http_requests_total",
				Help: "Total count of processed HTTP requests partitioned by method, path and status.",
			},
			[]string{"method", "path", "status"},
		),

		HTTPRequestDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "ov_http_request_duration_seconds",
				Help:    "Latency distribution of HTTP request handling.",
				Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30},
			},
			[]string{"method", "path", "status"},
		),

		FactoryRunsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "ov_factory_runs_total",
				Help: "Total count of autonomous factory pipeline runs by termination status.",
			},
			[]string{"status"},
		),

		DAGStepDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "ov_dag_step_duration_seconds",
				Help:    "Execution duration distribution of individual DAG task steps.",
				Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300},
			},
			[]string{"node_type", "status"},
		),

		WorkersActiveCount: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "ov_workers_active_count",
				Help: "Current count of active daemon workers connected to the cluster.",
			},
		),

		QueueDepth: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "ov_queue_depth",
				Help: "Current depth of pending task events partitioned by stream and topic.",
			},
			[]string{"stream", "topic"},
		),

		WorkerSandboxesActive: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "ov_worker_sandboxes_active",
				Help: "Current count of active isolated worktree sandboxes running tasks.",
			},
		),

		LLMTokensConsumedTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "ov_llm_tokens_consumed_total",
				Help: "Total tokens consumed by agent LLM invocations by provider, model and token type.",
			},
			[]string{"provider", "model", "token_type"},
		),

		LLMEstimatedCostUSD: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "ov_llm_estimated_cost_usd",
				Help: "Total estimated USD expenditure incurred by agent LLM queries.",
			},
			[]string{"provider", "model"},
		),
	}

	reg.MustRegister(
		m.HTTPRequestsTotal,
		m.HTTPRequestDuration,
		m.FactoryRunsTotal,
		m.DAGStepDuration,
		m.WorkersActiveCount,
		m.QueueDepth,
		m.WorkerSandboxesActive,
		m.LLMTokensConsumedTotal,
		m.LLMEstimatedCostUSD,
	)

	return m
}

// Registry returns the isolated Prometheus registry.
func (m *Metrics) Registry() *prometheus.Registry {
	if m == nil {
		return nil
	}
	return m.registry
}

// Handler returns an HTTP handler serving Prometheus text format for /metrics.
func (m *Metrics) Handler() http.Handler {
	if m == nil || m.registry == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		})
	}
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	})
}

// RecordHTTPRequest records latency and counts for an HTTP interaction.
func (m *Metrics) RecordHTTPRequest(method, path string, status int, duration time.Duration) {
	if m == nil {
		return
	}
	statusStr := fmt.Sprintf("%d", status)
	m.HTTPRequestsTotal.WithLabelValues(method, path, statusStr).Inc()
	m.HTTPRequestDuration.WithLabelValues(method, path, statusStr).Observe(duration.Seconds())
}

// RecordFactoryRun increments the factory run counter for the given terminal status.
func (m *Metrics) RecordFactoryRun(status string) {
	if m == nil {
		return
	}
	m.FactoryRunsTotal.WithLabelValues(status).Inc()
}

// RecordDAGStep records execution duration and increments status for a DAG task step.
func (m *Metrics) RecordDAGStep(nodeType, status string, duration time.Duration) {
	if m == nil {
		return
	}
	m.DAGStepDuration.WithLabelValues(nodeType, status).Observe(duration.Seconds())
}

// SetWorkersActive sets the gauge value for active workers.
func (m *Metrics) SetWorkersActive(count float64) {
	if m == nil {
		return
	}
	m.WorkersActiveCount.Set(count)
}

// SetQueueDepth sets the gauge value for pending queue depth.
func (m *Metrics) SetQueueDepth(stream, topic string, depth float64) {
	if m == nil {
		return
	}
	m.QueueDepth.WithLabelValues(stream, topic).Set(depth)
}

// SetWorkerSandboxesActive sets the gauge value for active sandbox containers / worktrees.
func (m *Metrics) SetWorkerSandboxesActive(count float64) {
	if m == nil {
		return
	}
	m.WorkerSandboxesActive.Set(count)
}

// RecordLLMTokens adds to the token counter for an LLM provider, model, and token type.
func (m *Metrics) RecordLLMTokens(provider, model, tokenType string, tokens float64) {
	if m == nil {
		return
	}
	m.LLMTokensConsumedTotal.WithLabelValues(provider, model, tokenType).Add(tokens)
}

// RecordLLMCost adds to the estimated cost counter for an LLM provider and model.
func (m *Metrics) RecordLLMCost(provider, model string, costUSD float64) {
	if m == nil {
		return
	}
	m.LLMEstimatedCostUSD.WithLabelValues(provider, model).Add(costUSD)
}
