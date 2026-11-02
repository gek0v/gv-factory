// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gv-factory/gv-factory/internal/telemetry"
)

// CostsOverviewResponse provides an aggregated view of all runs monitored for token usage and costs.
type CostsOverviewResponse struct {
	Runs                  []*telemetry.RunCostSummary `json:"runs"`
	TotalRuns             int                         `json:"total_runs"`
	TotalTokens           int64                       `json:"total_tokens"`
	TotalCostUSD          float64                     `json:"total_cost_usd"`
	TotalPromptTokens     int64                       `json:"total_prompt_tokens"`
	TotalCompletionTokens int64                       `json:"total_completion_tokens"`
}

// RecordUsageRequest models the JSON payload to record LLM token usage for a run.
type RecordUsageRequest struct {
	RunID            string `json:"run_id"`
	TaskID           string `json:"task_id,omitempty"`
	Model            string `json:"model"`
	PromptTokens     int64  `json:"prompt_tokens"`
	CompletionTokens int64  `json:"completion_tokens"`
}

// SetGuardrailRequest models updating a run's budget guardrail.
type SetGuardrailRequest struct {
	MaxBudgetUSD        float64 `json:"max_budget_usd,omitempty"`
	MaxTokens           int64   `json:"max_tokens,omitempty"`
	MaxPromptTokens     int64   `json:"max_prompt_tokens,omitempty"`
	MaxCompletionTokens int64   `json:"max_completion_tokens,omitempty"`
	WarningThreshold    float64 `json:"warning_threshold,omitempty"`
}

// registerCostEndpoints registers REST API routes for cost and token tracking onto the provided mux.
func registerCostEndpoints(mux *http.ServeMux, tracker *telemetry.CostTracker) {
	if mux == nil {
		return
	}
	if tracker == nil {
		tracker = telemetry.NewCostTracker()
	}

	// GET /api/v1/telemetry/costs
	// POST /api/v1/telemetry/costs (alias for record)
	costsCollectionHandler := func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			runs := tracker.GetAllCosts()
			var totalTokens, totalPrompt, totalCompletion int64
			var totalCost float64

			for _, s := range runs {
				totalTokens += s.TotalTokens
				totalPrompt += s.PromptTokens
				totalCompletion += s.CompletionTokens
				totalCost += s.TotalCostUSD
			}

			resp := CostsOverviewResponse{
				Runs:                  runs,
				TotalRuns:             len(runs),
				TotalTokens:           totalTokens,
				TotalCostUSD:          totalCost,
				TotalPromptTokens:     totalPrompt,
				TotalCompletionTokens: totalCompletion,
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(resp)

		case http.MethodPost:
			var req RecordUsageRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error": "invalid json payload",
				})
				return
			}

			if strings.TrimSpace(req.RunID) == "" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error": "run_id is required",
				})
				return
			}

			summary, err := tracker.RecordUsage(req.RunID, req.TaskID, req.Model, req.PromptTokens, req.CompletionTokens)
			w.Header().Set("Content-Type", "application/json")
			if err != nil {
				w.WriteHeader(http.StatusPaymentRequired) // 402 or 400 for exceeded quota
			} else {
				w.WriteHeader(http.StatusOK)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"summary": summary,
				"error":   errString(err),
			})

		default:
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		}
	}

	mux.HandleFunc("/api/v1/telemetry/costs", costsCollectionHandler)

	// Sub-paths: /api/v1/telemetry/costs/:run_id and /api/v1/telemetry/costs/:run_id/guardrail
	mux.HandleFunc("/api/v1/telemetry/costs/", func(w http.ResponseWriter, r *http.Request) {
		subpath := strings.TrimPrefix(r.URL.Path, "/api/v1/telemetry/costs/")
		subpath = strings.Trim(subpath, "/")

		if subpath == "" {
			costsCollectionHandler(w, r)
			return
		}

		parts := strings.Split(subpath, "/")
		runID := parts[0]

		if len(parts) == 1 {
			// /api/v1/telemetry/costs/:run_id
			if r.Method != http.MethodGet {
				http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
				return
			}

			summary, ok := tracker.GetRunCost(runID)
			if !ok {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusNotFound)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error": fmt.Sprintf("cost summary for run %q not found", runID),
				})
				return
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(summary)
			return
		}

		if len(parts) == 2 && parts[1] == "guardrail" {
			// POST /api/v1/telemetry/costs/:run_id/guardrail
			if r.Method != http.MethodPost && r.Method != http.MethodPut {
				http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
				return
			}

			var req SetGuardrailRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error": "invalid json payload",
				})
				return
			}

			guardrail := telemetry.BudgetGuardrail{
				MaxBudgetUSD:        req.MaxBudgetUSD,
				MaxTokens:           req.MaxTokens,
				MaxPromptTokens:     req.MaxPromptTokens,
				MaxCompletionTokens: req.MaxCompletionTokens,
				WarningThreshold:    req.WarningThreshold,
			}
			tracker.SetGuardrail(runID, guardrail)

			summary, _ := tracker.GetRunCost(runID)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(summary)
			return
		}

		http.NotFound(w, r)
	})
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
