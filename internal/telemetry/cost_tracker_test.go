// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package telemetry

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestPricingRegistry_DefaultsAndResolve(t *testing.T) {
	reg := NewPricingRegistry()

	tests := []struct {
		name         string
		model        string
		expectedMod  string
		expectedIn   float64
		expectedOut  float64
	}{
		{
			name:        "Claude 3.5 Sonnet exact",
			model:       "claude-3-5-sonnet",
			expectedMod: "claude-3-5-sonnet",
			expectedIn:  3.00,
			expectedOut: 15.00,
		},
		{
			name:        "Claude 3.5 Sonnet dot notation",
			model:       "claude-3.5-sonnet-20241022",
			expectedMod: "claude-3-5-sonnet",
			expectedIn:  3.00,
			expectedOut: 15.00,
		},
		{
			name:        "GPT-4o exact",
			model:       "gpt-4o",
			expectedMod: "gpt-4o",
			expectedIn:  2.50,
			expectedOut: 10.00,
		},
		{
			name:        "GPT-4o versioned alias uppercase",
			model:       "GPT-4o-2024-08-06",
			expectedMod: "gpt-4o",
			expectedIn:  2.50,
			expectedOut: 10.00,
		},
		{
			name:        "DeepSeek Chat",
			model:       "deepseek-chat",
			expectedMod: "deepseek-chat",
			expectedIn:  0.14,
			expectedOut: 0.28,
		},
		{
			name:        "DeepSeek R1",
			model:       "deepseek-r1",
			expectedMod: "deepseek-r1",
			expectedIn:  0.55,
			expectedOut: 2.19,
		},
		{
			name:        "DeepSeek prefix/family",
			model:       "deepseek/deepseek-coder:33b",
			expectedMod: "deepseek-chat",
			expectedIn:  0.14,
			expectedOut: 0.28,
		},
		{
			name:        "Local / Ollama local",
			model:       "local",
			expectedMod: "local",
			expectedIn:  0.00,
			expectedOut: 0.00,
		},
		{
			name:        "Local / Ollama llama3:8b",
			model:       "ollama/llama3:8b",
			expectedMod: "local",
			expectedIn:  0.00,
			expectedOut: 0.00,
		},
		{
			name:        "Unknown fallback model",
			model:       "custom-corp-llm",
			expectedMod: "custom-corp-llm",
			expectedIn:  1.00,
			expectedOut: 3.00,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := reg.Resolve(tc.model)
			if p.PromptPricePer1MTokens != tc.expectedIn {
				t.Errorf("expected prompt price %f, got %f", tc.expectedIn, p.PromptPricePer1MTokens)
			}
			if p.OutputPricePer1MTokens != tc.expectedOut {
				t.Errorf("expected output price %f, got %f", tc.expectedOut, p.OutputPricePer1MTokens)
			}
		})
	}
}

func TestPricingRegistry_CustomRegister(t *testing.T) {
	reg := NewPricingRegistry()
	custom := ModelPricing{
		Model:                  "custom-v1",
		DisplayName:            "Custom LLM V1",
		PromptPricePer1MTokens: 5.0,
		OutputPricePer1MTokens: 20.0,
	}
	reg.Register(custom)

	resolved := reg.Resolve("custom-v1")
	if resolved.PromptPricePer1MTokens != 5.0 || resolved.OutputPricePer1MTokens != 20.0 {
		t.Fatalf("unexpected custom pricing: %+v", resolved)
	}

	cost := resolved.CalculateCost(1_000_000, 1_000_000)
	if cost != 25.0 {
		t.Fatalf("expected cost 25.0, got %f", cost)
	}
}

func TestCostTracker_RecordUsageAndAccumulation(t *testing.T) {
	ct := NewCostTracker(BudgetGuardrail{
		MaxBudgetUSD:     50.0,
		MaxTokens:        1_000_000,
		WarningThreshold: 0.8,
	})

	runID := "run-100"
	taskID := "task-abc"

	// 100k prompt, 50k completion on Claude 3.5 Sonnet
	// Prompt cost: (100,000 / 1M) * $3.00 = $0.30
	// Output cost: (50,000 / 1M) * $15.00 = $0.75
	// Total cost = $1.05
	s, err := ct.RecordUsage(runID, taskID, "claude-3-5-sonnet", 100_000, 50_000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.PromptTokens != 100_000 || s.CompletionTokens != 50_000 || s.TotalTokens != 150_000 {
		t.Errorf("unexpected tokens: prompt=%d, comp=%d, total=%d", s.PromptTokens, s.CompletionTokens, s.TotalTokens)
	}
	expectedCost := 1.05
	if fmt.Sprintf("%.2f", s.TotalCostUSD) != fmt.Sprintf("%.2f", expectedCost) {
		t.Errorf("expected total cost %.2f, got %.4f", expectedCost, s.TotalCostUSD)
	}
	if s.WarningTriggered || s.Exceeded {
		t.Errorf("expected no warning or exceeded flag")
	}

	// Record Ollama usage (free)
	s2, err := ct.RecordUsage(runID, taskID, "ollama", 500_000, 200_000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s2.TotalTokens != 850_000 {
		t.Errorf("expected 850,000 total tokens, got %d", s2.TotalTokens)
	}
	if fmt.Sprintf("%.2f", s2.TotalCostUSD) != fmt.Sprintf("%.2f", expectedCost) {
		t.Errorf("expected cost to remain %.2f, got %.4f", expectedCost, s2.TotalCostUSD)
	}

	// 850,000 / 1,000,000 = 85% -> Warning threshold triggered!
	if !s2.WarningTriggered {
		t.Errorf("expected warning to be triggered for token ratio >= 80%%")
	}
	if s2.Exceeded {
		t.Errorf("expected Exceeded to remain false")
	}

	// Verify model breakdown
	if len(s2.Models) != 2 {
		t.Errorf("expected 2 models tracked, got %d", len(s2.Models))
	}
	claudeStats := s2.Models["claude-3-5-sonnet"]
	if claudeStats.TotalTokens != 150_000 {
		t.Errorf("expected claude total tokens 150,000, got %d", claudeStats.TotalTokens)
	}
}

func TestCostTracker_BudgetExceededGuardrails(t *testing.T) {
	t.Run("MaxBudgetUSD exceeded", func(t *testing.T) {
		ct := NewCostTracker(BudgetGuardrail{
			MaxBudgetUSD: 1.00,
			MaxTokens:    1_000_000,
		})
		runID := "run-budget"

		// 100k prompt, 50k completion on Claude ($1.05 > $1.00)
		s, err := ct.RecordUsage(runID, "task-1", "claude-3-5-sonnet", 100_000, 50_000)
		if err == nil {
			t.Fatal("expected ErrBudgetExceeded error, got nil")
		}
		if !errors.Is(err, ErrBudgetExceeded) {
			t.Errorf("expected errors.Is(err, ErrBudgetExceeded) == true, got: %v", err)
		}
		if !s.Exceeded {
			t.Errorf("expected s.Exceeded == true")
		}

		// Subsequent CheckBudget
		checkErr := ct.CheckBudget(runID)
		if checkErr == nil || !errors.Is(checkErr, ErrBudgetExceeded) {
			t.Errorf("expected CheckBudget to return ErrBudgetExceeded, got: %v", checkErr)
		}
	})

	t.Run("MaxTokens exceeded", func(t *testing.T) {
		ct := NewCostTracker(BudgetGuardrail{
			MaxTokens: 5_000,
		})
		runID := "run-tokens"

		s, err := ct.RecordUsage(runID, "task-2", "local", 3_000, 3_000)
		if err == nil || !errors.Is(err, ErrBudgetExceeded) {
			t.Fatalf("expected ErrBudgetExceeded for token limit, got: %v", err)
		}
		if !s.Exceeded {
			t.Errorf("expected summary.Exceeded = true")
		}
	})

	t.Run("MaxPromptTokens and MaxCompletionTokens limits", func(t *testing.T) {
		ct := NewCostTracker(BudgetGuardrail{
			MaxPromptTokens:     1_000,
			MaxCompletionTokens: 500,
		})

		_, errPrompt := ct.RecordUsage("run-p", "task-p", "gpt-4o", 1_500, 200)
		if errPrompt == nil || !errors.Is(errPrompt, ErrBudgetExceeded) {
			t.Errorf("expected ErrBudgetExceeded for prompt tokens, got %v", errPrompt)
		}

		_, errComp := ct.RecordUsage("run-c", "task-c", "gpt-4o", 200, 600)
		if errComp == nil || !errors.Is(errComp, ErrBudgetExceeded) {
			t.Errorf("expected ErrBudgetExceeded for completion tokens, got %v", errComp)
		}
	})
}

func TestCostTracker_SetGuardrailAndReset(t *testing.T) {
	ct := NewCostTracker(BudgetGuardrail{})
	runID := "run-override"

	// Initially no limit
	_, err := ct.RecordUsage(runID, "task-1", "gpt-4o", 500_000, 500_000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Update guardrail dynamically to low limit
	ct.SetGuardrail(runID, BudgetGuardrail{MaxBudgetUSD: 0.50})
	summary, ok := ct.GetRunCost(runID)
	if !ok {
		t.Fatal("expected summary to exist")
	}
	if !summary.Exceeded {
		t.Errorf("expected Exceeded = true after guardrail lowered")
	}

	// GetAllCosts
	all := ct.GetAllCosts()
	if len(all) != 1 {
		t.Errorf("expected 1 run in GetAllCosts, got %d", len(all))
	}

	// Reset
	ct.Reset(runID)
	_, found := ct.GetRunCost(runID)
	if found {
		t.Errorf("expected run to be deleted after Reset")
	}

	// CheckBudget on non-existent run returns nil
	if err := ct.CheckBudget("non-existent"); err != nil {
		t.Errorf("expected nil error for unknown run, got %v", err)
	}
}

func TestCostTracker_ConcurrentAccess(t *testing.T) {
	ct := NewCostTracker(BudgetGuardrail{MaxBudgetUSD: 1000.0, MaxTokens: 10_000_000})

	const numGoroutines = 20
	const iterations = 50
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			runID := fmt.Sprintf("run-concurrent-%d", id%4)
			for j := 0; j < iterations; j++ {
				_, _ = ct.RecordUsage(runID, "task", "gpt-4o", 100, 50)
				_ = ct.CheckBudget(runID)
				_, _ = ct.GetRunCost(runID)
				_ = ct.GetAllCosts()
			}
		}(i)
	}

	wg.Wait()

	costs := ct.GetAllCosts()
	if len(costs) != 4 {
		t.Errorf("expected 4 tracked runs, got %d", len(costs))
	}
}

func TestCostTracker_EdgeCasesAndCoverage(t *testing.T) {
	ct := NewCostTracker()
	if ct.PricingRegistry() == nil {
		t.Error("expected non-nil PricingRegistry")
	}

	// cloneSummary with nil
	if ct.cloneSummary(nil) != nil {
		t.Error("expected nil clone for nil input")
	}

	// BudgetExceededError formatting
	bErr := &BudgetExceededError{
		RunID:   "run-err",
		TaskID:  "task-err",
		Metric:  "cost",
		Current: 12.5,
		Limit:   10.0,
		Reason:  "cost limit breached",
	}
	if !strings.Contains(bErr.Error(), "run-err") || !strings.Contains(bErr.Error(), "cost limit breached") {
		t.Errorf("unexpected error format: %s", bErr.Error())
	}
	if !errors.Is(bErr, ErrBudgetExceeded) {
		t.Error("expected bErr to unwrap to ErrBudgetExceeded")
	}

	// Check existing run with TaskID update
	ct.RecordUsage("run-existing", "", "local", 10, 10)
	summary, _ := ct.RecordUsage("run-existing", "task-now-provided", "local", 10, 10)
	if summary.TaskID != "task-now-provided" {
		t.Errorf("expected task id to be updated, got %s", summary.TaskID)
	}

	// Custom warning threshold
	ct.SetGuardrail("run-warn", BudgetGuardrail{
		MaxBudgetUSD:     10.0,
		WarningThreshold: -1, // fallback to 0.8
	})
	sWarn, _ := ct.RecordUsage("run-warn", "t", "claude-3-5-sonnet", 600_000, 500_000)
	if !sWarn.WarningTriggered {
		t.Error("expected warning triggered with default ratio fallback")
	}
}
