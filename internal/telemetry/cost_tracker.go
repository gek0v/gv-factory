// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package telemetry

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// ErrBudgetExceeded is returned when token consumption or estimated cost breaches configured guardrails.
var ErrBudgetExceeded = errors.New("telemetry: budget guardrail exceeded")

// BudgetExceededError encapsulates granular details of which quota or budget limit was breached.
type BudgetExceededError struct {
	RunID   string  `json:"run_id"`
	TaskID  string  `json:"task_id,omitempty"`
	Metric  string  `json:"metric"` // "cost", "tokens", "prompt_tokens", or "completion_tokens"
	Current float64 `json:"current"`
	Limit   float64 `json:"limit"`
	Reason  string  `json:"reason"`
}

func (e *BudgetExceededError) Error() string {
	return fmt.Sprintf("telemetry: budget guardrail exceeded for run %q: %s", e.RunID, e.Reason)
}

// Unwrap enables standard errors.Is(err, ErrBudgetExceeded) checks.
func (e *BudgetExceededError) Unwrap() error {
	return ErrBudgetExceeded
}

// ModelPricing defines input (prompt) and output (completion) prices per 1 million tokens in USD.
type ModelPricing struct {
	Model                  string  `json:"model"`
	DisplayName            string  `json:"display_name,omitempty"`
	PromptPricePer1MTokens float64 `json:"prompt_price_per_1m_tokens"`
	OutputPricePer1MTokens float64 `json:"output_price_per_1m_tokens"`
}

// CalculateCost computes the total estimated USD cost for the provided token usage.
func (mp ModelPricing) CalculateCost(promptTokens, completionTokens int64) float64 {
	promptCost := (float64(promptTokens) / 1_000_000.0) * mp.PromptPricePer1MTokens
	completionCost := (float64(completionTokens) / 1_000_000.0) * mp.OutputPricePer1MTokens
	return promptCost + completionCost
}

// PricingRegistry maintains a registry of model pricing schemas with fuzzy/normalized lookup support.
type PricingRegistry struct {
	mu     sync.RWMutex
	prices map[string]ModelPricing
}

// NewPricingRegistry instantiates a registry pre-loaded with industry standard models.
func NewPricingRegistry() *PricingRegistry {
	r := &PricingRegistry{
		prices: make(map[string]ModelPricing),
	}
	r.registerDefaults()
	return r
}

func (r *PricingRegistry) registerDefaults() {
	// Claude 3.5 Sonnet: $3.00/1M input, $15.00/1M output
	claude := ModelPricing{
		Model:                  "claude-3-5-sonnet",
		DisplayName:            "Anthropic Claude 3.5 Sonnet",
		PromptPricePer1MTokens: 3.00,
		OutputPricePer1MTokens: 15.00,
	}
	r.prices["claude-3-5-sonnet"] = claude
	r.prices["claude-3.5-sonnet"] = claude
	r.prices["claude-3-5-sonnet-20241022"] = claude
	r.prices["claude-3-5-sonnet-latest"] = claude

	// GPT-4o: $2.50/1M input, $10.00/1M output
	gpt4o := ModelPricing{
		Model:                  "gpt-4o",
		DisplayName:            "OpenAI GPT-4o",
		PromptPricePer1MTokens: 2.50,
		OutputPricePer1MTokens: 10.00,
	}
	r.prices["gpt-4o"] = gpt4o
	r.prices["gpt-4o-2024-08-06"] = gpt4o
	r.prices["chatgpt-4o-latest"] = gpt4o

	// DeepSeek: $0.14/1M input (cache hit/miss baseline), $0.28/1M output
	deepseek := ModelPricing{
		Model:                  "deepseek-chat",
		DisplayName:            "DeepSeek V3 / Chat",
		PromptPricePer1MTokens: 0.14,
		OutputPricePer1MTokens: 0.28,
	}
	r.prices["deepseek-chat"] = deepseek
	r.prices["deepseek-coder"] = deepseek
	r.prices["deepseek-v3"] = deepseek
	r.prices["deepseek-r1"] = ModelPricing{
		Model:                  "deepseek-r1",
		DisplayName:            "DeepSeek R1",
		PromptPricePer1MTokens: 0.55,
		OutputPricePer1MTokens: 2.19,
	}

	// Ollama / Local: $0.00 input, $0.00 output
	local := ModelPricing{
		Model:                  "local",
		DisplayName:            "Local / Ollama",
		PromptPricePer1MTokens: 0.00,
		OutputPricePer1MTokens: 0.00,
	}
	r.prices["local"] = local
	r.prices["ollama"] = local
}

// Register registers or overrides a model pricing configuration.
func (r *PricingRegistry) Register(p ModelPricing) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := strings.ToLower(strings.TrimSpace(p.Model))
	r.prices[key] = p
}

// Resolve matches a model identifier to pricing, resolving known prefixes and aliases.
func (r *PricingRegistry) Resolve(modelName string) ModelPricing {
	r.mu.RLock()
	defer r.mu.RUnlock()

	norm := strings.ToLower(strings.TrimSpace(modelName))
	if p, ok := r.prices[norm]; ok {
		return p
	}

	// Substring / family matching
	if strings.Contains(norm, "claude-3-5-sonnet") || strings.Contains(norm, "claude-3.5-sonnet") {
		return r.prices["claude-3-5-sonnet"]
	}
	if strings.Contains(norm, "gpt-4o") {
		return r.prices["gpt-4o"]
	}
	if strings.Contains(norm, "deepseek-r1") {
		return r.prices["deepseek-r1"]
	}
	if strings.Contains(norm, "deepseek") {
		return r.prices["deepseek-chat"]
	}
	if strings.Contains(norm, "ollama") || strings.Contains(norm, "local") {
		return r.prices["local"]
	}

	// Fallback default: generic cloud model tier ($1.00 / 1M input, $3.00 / 1M output)
	return ModelPricing{
		Model:                  modelName,
		DisplayName:            modelName,
		PromptPricePer1MTokens: 1.00,
		OutputPricePer1MTokens: 3.00,
	}
}

// BudgetGuardrail specifies limits and thresholds to avoid runaway cost or token loops.
type BudgetGuardrail struct {
	MaxBudgetUSD        float64 `json:"max_budget_usd,omitempty"`
	MaxTokens           int64   `json:"max_tokens,omitempty"`
	MaxPromptTokens     int64   `json:"max_prompt_tokens,omitempty"`
	MaxCompletionTokens int64   `json:"max_completion_tokens,omitempty"`
	WarningThreshold    float64 `json:"warning_threshold,omitempty"` // Fractional threshold for warnings (e.g. 0.8 for 80%)
}

// DefaultBudgetGuardrail provides sensible defaults ($10.00 USD, 500,000 tokens, 80% warning threshold).
func DefaultBudgetGuardrail() BudgetGuardrail {
	return BudgetGuardrail{
		MaxBudgetUSD:     10.00,
		MaxTokens:        500_000,
		WarningThreshold: 0.80,
	}
}

// ModelUsage aggregates consumption for a specific model within a run.
type ModelUsage struct {
	Model            string  `json:"model"`
	DisplayName      string  `json:"display_name,omitempty"`
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	TotalTokens      int64   `json:"total_tokens"`
	CostUSD          float64 `json:"cost_usd"`
}

// RunCostSummary contains consolidated token usage and cost metrics for a run.
type RunCostSummary struct {
	RunID            string                `json:"run_id"`
	TaskID           string                `json:"task_id,omitempty"`
	PromptTokens     int64                 `json:"prompt_tokens"`
	CompletionTokens int64                 `json:"completion_tokens"`
	TotalTokens      int64                 `json:"total_tokens"`
	TotalCostUSD     float64               `json:"total_cost_usd"`
	Guardrail        BudgetGuardrail       `json:"guardrail"`
	Exceeded         bool                  `json:"exceeded"`
	ExceededReason   string                `json:"exceeded_reason,omitempty"`
	WarningTriggered bool                  `json:"warning_triggered"`
	Models           map[string]ModelUsage `json:"models"`
	UpdatedAt        time.Time             `json:"updated_at"`
}

// CostTracker maintains thread-safe accounting of token usage, costs, and budget guardrails.
type CostTracker struct {
	mu               sync.RWMutex
	registry         *PricingRegistry
	defaultGuardrail BudgetGuardrail
	runs             map[string]*RunCostSummary
}

// NewCostTracker creates a new CostTracker instance.
func NewCostTracker(defaultGuardrail ...BudgetGuardrail) *CostTracker {
	dg := DefaultBudgetGuardrail()
	if len(defaultGuardrail) > 0 {
		dg = defaultGuardrail[0]
	}
	return &CostTracker{
		registry:         NewPricingRegistry(),
		defaultGuardrail: dg,
		runs:             make(map[string]*RunCostSummary),
	}
}

// PricingRegistry exposes the underlying pricing registry.
func (ct *CostTracker) PricingRegistry() *PricingRegistry {
	return ct.registry
}

// SetGuardrail sets or updates the budget guardrail for a specific run.
func (ct *CostTracker) SetGuardrail(runID string, guardrail BudgetGuardrail) {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	summary := ct.getOrCreateSummaryLocked(runID, "")
	summary.Guardrail = guardrail
	summary.UpdatedAt = time.Now()
	ct.evaluateGuardrailLocked(summary)
}

// RecordUsage tracks token consumption for a run, calculates costs, updates statistics, and evaluates guardrails.
func (ct *CostTracker) RecordUsage(runID, taskID, modelName string, promptTokens, completionTokens int64) (*RunCostSummary, error) {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	summary := ct.getOrCreateSummaryLocked(runID, taskID)
	pricing := ct.registry.Resolve(modelName)
	addedCost := pricing.CalculateCost(promptTokens, completionTokens)

	summary.PromptTokens += promptTokens
	summary.CompletionTokens += completionTokens
	summary.TotalTokens += (promptTokens + completionTokens)
	summary.TotalCostUSD += addedCost
	summary.UpdatedAt = time.Now()

	// Update per-model stats
	modelKey := pricing.Model
	mu := summary.Models[modelKey]
	mu.Model = pricing.Model
	mu.DisplayName = pricing.DisplayName
	mu.PromptTokens += promptTokens
	mu.CompletionTokens += completionTokens
	mu.TotalTokens += (promptTokens + completionTokens)
	mu.CostUSD += addedCost
	summary.Models[modelKey] = mu

	err := ct.evaluateGuardrailLocked(summary)
	return ct.cloneSummary(summary), err
}

// CheckBudget evaluates whether the specified run has exceeded its budget.
func (ct *CostTracker) CheckBudget(runID string) error {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	summary, ok := ct.runs[runID]
	if !ok {
		return nil
	}

	if summary.Exceeded {
		return &BudgetExceededError{
			RunID:   summary.RunID,
			TaskID:  summary.TaskID,
			Metric:  "exceeded",
			Current: summary.TotalCostUSD,
			Limit:   summary.Guardrail.MaxBudgetUSD,
			Reason:  summary.ExceededReason,
		}
	}

	return nil
}

// GetRunCost returns the cost summary for a given run ID.
func (ct *CostTracker) GetRunCost(runID string) (*RunCostSummary, bool) {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	summary, ok := ct.runs[runID]
	if !ok {
		return nil, false
	}
	return ct.cloneSummary(summary), true
}

// GetAllCosts returns summaries for all tracked runs.
func (ct *CostTracker) GetAllCosts() []*RunCostSummary {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	res := make([]*RunCostSummary, 0, len(ct.runs))
	for _, s := range ct.runs {
		res = append(res, ct.cloneSummary(s))
	}
	return res
}

// Reset clears recorded usage for a specific run ID.
func (ct *CostTracker) Reset(runID string) {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	delete(ct.runs, runID)
}

func (ct *CostTracker) getOrCreateSummaryLocked(runID, taskID string) *RunCostSummary {
	if s, ok := ct.runs[runID]; ok {
		if taskID != "" && s.TaskID == "" {
			s.TaskID = taskID
		}
		return s
	}

	s := &RunCostSummary{
		RunID:     runID,
		TaskID:    taskID,
		Guardrail: ct.defaultGuardrail,
		Models:    make(map[string]ModelUsage),
		UpdatedAt: time.Now(),
	}
	ct.runs[runID] = s
	return s
}

func (ct *CostTracker) evaluateGuardrailLocked(summary *RunCostSummary) error {
	g := summary.Guardrail
	summary.Exceeded = false
	summary.ExceededReason = ""
	summary.WarningTriggered = false

	warnRatio := g.WarningThreshold
	if warnRatio <= 0 {
		warnRatio = 0.80
	}

	// 1. MaxBudgetUSD check
	if g.MaxBudgetUSD > 0 {
		if summary.TotalCostUSD >= g.MaxBudgetUSD*warnRatio {
			summary.WarningTriggered = true
		}
		if summary.TotalCostUSD > g.MaxBudgetUSD {
			summary.Exceeded = true
			summary.ExceededReason = fmt.Sprintf("cost limit breached: $%.4f > $%.2f", summary.TotalCostUSD, g.MaxBudgetUSD)
			return &BudgetExceededError{
				RunID:   summary.RunID,
				TaskID:  summary.TaskID,
				Metric:  "cost",
				Current: summary.TotalCostUSD,
				Limit:   g.MaxBudgetUSD,
				Reason:  summary.ExceededReason,
			}
		}
	}

	// 2. MaxTokens check
	if g.MaxTokens > 0 {
		if float64(summary.TotalTokens) >= float64(g.MaxTokens)*warnRatio {
			summary.WarningTriggered = true
		}
		if summary.TotalTokens > g.MaxTokens {
			summary.Exceeded = true
			summary.ExceededReason = fmt.Sprintf("total token limit breached: %d > %d tokens", summary.TotalTokens, g.MaxTokens)
			return &BudgetExceededError{
				RunID:   summary.RunID,
				TaskID:  summary.TaskID,
				Metric:  "tokens",
				Current: float64(summary.TotalTokens),
				Limit:   float64(g.MaxTokens),
				Reason:  summary.ExceededReason,
			}
		}
	}

	// 3. MaxPromptTokens check
	if g.MaxPromptTokens > 0 {
		if float64(summary.PromptTokens) >= float64(g.MaxPromptTokens)*warnRatio {
			summary.WarningTriggered = true
		}
		if summary.PromptTokens > g.MaxPromptTokens {
			summary.Exceeded = true
			summary.ExceededReason = fmt.Sprintf("prompt token limit breached: %d > %d tokens", summary.PromptTokens, g.MaxPromptTokens)
			return &BudgetExceededError{
				RunID:   summary.RunID,
				TaskID:  summary.TaskID,
				Metric:  "prompt_tokens",
				Current: float64(summary.PromptTokens),
				Limit:   float64(g.MaxPromptTokens),
				Reason:  summary.ExceededReason,
			}
		}
	}

	// 4. MaxCompletionTokens check
	if g.MaxCompletionTokens > 0 {
		if float64(summary.CompletionTokens) >= float64(g.MaxCompletionTokens)*warnRatio {
			summary.WarningTriggered = true
		}
		if summary.CompletionTokens > g.MaxCompletionTokens {
			summary.Exceeded = true
			summary.ExceededReason = fmt.Sprintf("completion token limit breached: %d > %d tokens", summary.CompletionTokens, g.MaxCompletionTokens)
			return &BudgetExceededError{
				RunID:   summary.RunID,
				TaskID:  summary.TaskID,
				Metric:  "completion_tokens",
				Current: float64(summary.CompletionTokens),
				Limit:   float64(g.MaxCompletionTokens),
				Reason:  summary.ExceededReason,
			}
		}
	}

	return nil
}

func (ct *CostTracker) cloneSummary(src *RunCostSummary) *RunCostSummary {
	if src == nil {
		return nil
	}
	clone := *src
	clone.Models = make(map[string]ModelUsage, len(src.Models))
	for k, v := range src.Models {
		clone.Models[k] = v
	}
	return &clone
}
