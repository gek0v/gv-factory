// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gv-factory/gv-factory/internal/telemetry"
)

func TestCostEndpoints_Lifecycle(t *testing.T) {
	mux := http.NewServeMux()
	tracker := telemetry.NewCostTracker(telemetry.BudgetGuardrail{
		MaxBudgetUSD: 10.0,
		MaxTokens:    100_000,
	})
	registerCostEndpoints(mux, tracker)

	// 1. Initial GET /api/v1/telemetry/costs should be empty
	req := httptest.NewRequest(http.MethodGet, "/api/v1/telemetry/costs", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}

	var overview CostsOverviewResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &overview); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if overview.TotalRuns != 0 || len(overview.Runs) != 0 {
		t.Errorf("expected 0 runs initially, got %d", overview.TotalRuns)
	}

	// 2. Record token usage via POST /api/v1/telemetry/costs
	recordPayload := RecordUsageRequest{
		RunID:            "run-abc",
		TaskID:           "task-123",
		Model:            "claude-3-5-sonnet",
		PromptTokens:     10_000,
		CompletionTokens: 2_000,
	}
	body, _ := json.Marshal(recordPayload)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/telemetry/costs", bytes.NewReader(body))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on record, got %d: %s", rec.Code, rec.Body.String())
	}

	// 3. GET /api/v1/telemetry/costs/:run_id
	req = httptest.NewRequest(http.MethodGet, "/api/v1/telemetry/costs/run-abc", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for run-abc, got %d", rec.Code)
	}

	var runSummary telemetry.RunCostSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &runSummary); err != nil {
		t.Fatalf("failed to unmarshal run summary: %v", err)
	}
	if runSummary.RunID != "run-abc" || runSummary.TotalTokens != 12_000 {
		t.Errorf("unexpected run summary: %+v", runSummary)
	}

	// 4. Update guardrail via POST /api/v1/telemetry/costs/run-abc/guardrail
	guardrailPayload := SetGuardrailRequest{
		MaxBudgetUSD: 0.01, // lower than current cost
	}
	gBody, _ := json.Marshal(guardrailPayload)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/telemetry/costs/run-abc/guardrail", bytes.NewReader(gBody))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on guardrail update, got %d", rec.Code)
	}

	var updatedSummary telemetry.RunCostSummary
	_ = json.Unmarshal(rec.Body.Bytes(), &updatedSummary)
	if !updatedSummary.Exceeded {
		t.Errorf("expected run to be marked Exceeded after lowering MaxBudgetUSD")
	}

	// 5. Subsequent record breaching budget returns 402 Payment Required
	recordPayload2 := RecordUsageRequest{
		RunID:            "run-abc",
		Model:            "gpt-4o",
		PromptTokens:     5_000,
		CompletionTokens: 5_000,
	}
	body2, _ := json.Marshal(recordPayload2)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/telemetry/costs", bytes.NewReader(body2))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusPaymentRequired {
		t.Errorf("expected 402 Payment Required when budget exceeded, got %d", rec.Code)
	}

	// 6. GET overview now has 1 run and accumulated metrics
	req = httptest.NewRequest(http.MethodGet, "/api/v1/telemetry/costs", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	_ = json.Unmarshal(rec.Body.Bytes(), &overview)
	if overview.TotalRuns != 1 {
		t.Errorf("expected 1 total run in overview, got %d", overview.TotalRuns)
	}
	if overview.TotalTokens != 22_000 {
		t.Errorf("expected 22,000 total tokens, got %d", overview.TotalTokens)
	}
}

func TestCostEndpoints_ErrorAndEdgeCases(t *testing.T) {
	mux := http.NewServeMux()
	tracker := telemetry.NewCostTracker()
	registerCostEndpoints(mux, tracker)

	// Nil safety
	registerCostEndpoints(nil, nil)
	registerCostEndpoints(http.NewServeMux(), nil)

	// 404 for non-existent run
	req := httptest.NewRequest(http.MethodGet, "/api/v1/telemetry/costs/run-non-existent", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown run, got %d", rec.Code)
	}

	// 405 Method Not Allowed on collection
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/telemetry/costs", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for DELETE on collection, got %d", rec.Code)
	}

	// 405 Method Not Allowed on item
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/telemetry/costs/run-123", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for DELETE on item, got %d", rec.Code)
	}

	// 400 Bad Request on invalid JSON to record
	req = httptest.NewRequest(http.MethodPost, "/api/v1/telemetry/costs", strings.NewReader("{invalid-json"))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for malformed json, got %d", rec.Code)
	}

	// 400 Bad Request on empty run_id
	emptyRunPayload := RecordUsageRequest{RunID: "   "}
	eb, _ := json.Marshal(emptyRunPayload)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/telemetry/costs", bytes.NewReader(eb))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty run_id, got %d", rec.Code)
	}

	// 400 Bad Request on invalid JSON to guardrail
	req = httptest.NewRequest(http.MethodPost, "/api/v1/telemetry/costs/run-123/guardrail", strings.NewReader("bad"))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for malformed guardrail json, got %d", rec.Code)
	}

	// 404 for unknown subpath
	req = httptest.NewRequest(http.MethodGet, "/api/v1/telemetry/costs/run-123/unknown/subpath", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown subpath, got %d", rec.Code)
	}

	// Slash trailing fallback
	req = httptest.NewRequest(http.MethodGet, "/api/v1/telemetry/costs/", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for trailing slash overview, got %d", rec.Code)
	}
}
