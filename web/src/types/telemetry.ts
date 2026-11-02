// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

export interface ModelUsage {
  model: string;
  display_name?: string;
  prompt_tokens: number;
  completion_tokens: number;
  total_tokens: number;
  cost_usd: number;
}

export interface BudgetGuardrail {
  max_budget_usd?: number;
  max_tokens?: number;
  max_prompt_tokens?: number;
  max_completion_tokens?: number;
  warning_threshold?: number;
}

export interface RunCostSummary {
  run_id: string;
  task_id?: string;
  prompt_tokens: number;
  completion_tokens: number;
  total_tokens: number;
  total_cost_usd: number;
  guardrail: BudgetGuardrail;
  exceeded: boolean;
  exceeded_reason?: string;
  warning_triggered: boolean;
  models: Record<string, ModelUsage>;
  updated_at?: string;
}

export interface CostsOverviewResponse {
  runs: RunCostSummary[];
  total_runs: number;
  total_tokens: number;
  total_cost_usd: number;
  total_prompt_tokens: number;
  total_completion_tokens: number;
}
