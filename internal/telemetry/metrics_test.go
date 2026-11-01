// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package telemetry_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gv-factory/gv-factory/internal/telemetry"
)

func TestMetricsRegistryAndCollectors(t *testing.T) {
	m := telemetry.NewMetrics()
	if m == nil {
		t.Fatal("expected non-nil Metrics instance")
	}

	// 1. HTTP RED metrics
	m.RecordHTTPRequest("GET", "/api/v1/health", 200, 15*time.Millisecond)
	m.RecordHTTPRequest("POST", "/api/v1/webhooks/github", 500, 120*time.Millisecond)

	// 2. DAG Execution metrics
	m.RecordFactoryRun("COMPLETED")
	m.RecordFactoryRun("FAILED")
	m.RecordDAGStep("tdd", "COMPLETED", 500*time.Millisecond)
	m.RecordDAGStep("reviewer", "FAILED", 2*time.Second)

	// 3. Worker and Queue metrics
	m.SetWorkersActive(4)
	m.SetQueueDepth("ov-stream", "tasks.pending", 12)
	m.SetWorkerSandboxesActive(3)

	// 4. LLM Token and Cost accounting
	m.RecordLLMTokens("anthropic", "claude-3-5-sonnet", "input", 1500)
	m.RecordLLMTokens("anthropic", "claude-3-5-sonnet", "output", 350)
	m.RecordLLMCost("anthropic", "claude-3-5-sonnet", 0.00975)

	// Render via Handler
	handler := m.Handler()
	if handler == nil {
		t.Fatal("expected non-nil Handler")
	}

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	res := rec.Result()
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200 OK, got %d", res.StatusCode)
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}

	bodyStr := string(body)

	expectedMetrics := []string{
		"ov_http_requests_total",
		"ov_http_request_duration_seconds",
		"ov_factory_runs_total",
		"ov_dag_step_duration_seconds",
		"ov_workers_active_count",
		"ov_queue_depth",
		"ov_worker_sandboxes_active",
		"ov_llm_tokens_consumed_total",
		"ov_llm_estimated_cost_usd",
	}

	for _, metric := range expectedMetrics {
		if !strings.Contains(bodyStr, metric) {
			t.Errorf("missing expected metric %q in /metrics output", metric)
		}
	}

	// Verify specific labels rendered
	expectedSubstrings := []string{
		`ov_http_requests_total{method="GET",path="/api/v1/health",status="200"} 1`,
		`ov_http_requests_total{method="POST",path="/api/v1/webhooks/github",status="500"} 1`,
		`ov_factory_runs_total{status="COMPLETED"} 1`,
		`ov_factory_runs_total{status="FAILED"} 1`,
		`ov_workers_active_count 4`,
		`ov_queue_depth{stream="ov-stream",topic="tasks.pending"} 12`,
		`ov_worker_sandboxes_active 3`,
		`ov_llm_tokens_consumed_total{model="claude-3-5-sonnet",provider="anthropic",token_type="input"} 1500`,
		`ov_llm_tokens_consumed_total{model="claude-3-5-sonnet",provider="anthropic",token_type="output"} 350`,
		`ov_llm_estimated_cost_usd{model="claude-3-5-sonnet",provider="anthropic"} 0.00975`,
	}

	for _, sub := range expectedSubstrings {
		if !strings.Contains(bodyStr, sub) {
			t.Errorf("expected substring %q not found in /metrics output", sub)
		}
	}
}

func TestMetricsIsolation(t *testing.T) {
	// Creating multiple metrics instances should not panic with duplicate metric registration
	m1 := telemetry.NewMetrics()
	m2 := telemetry.NewMetrics()

	m1.RecordFactoryRun("COMPLETED")
	m2.RecordFactoryRun("FAILED")

	if m1.Registry() == m2.Registry() {
		t.Error("expected different registries for isolated Metrics instances")
	}
}
