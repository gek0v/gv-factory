// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import React, { useEffect, useState, useCallback, useMemo } from 'react';
import { useFactoryFlowStore } from '../../store';
import type { RunCostSummary, BudgetGuardrail } from '../../types/telemetry';
import { fetchRunCost } from '../../api/telemetry';
import { formatUSD, formatTokenCount, CostBadge } from './CostBadge';

export interface CostBudgetModalProps {
  isOpen?: boolean;
  onClose?: () => void;
  runId?: string;
  summary?: RunCostSummary | null;
  onRefresh?: () => void;
}

export type BudgetStatusLevel = 'normal' | 'warning' | 'exceeded';

export interface BudgetStatusInfo {
  status: BudgetStatusLevel;
  message: string;
  color: string;
  percentage: number;
}

export function calculateBudgetPercentage(current: number, max?: number): number {
  if (!max || max <= 0 || isNaN(current) || current <= 0) {
    return 0;
  }
  const pct = (current / max) * 100;
  return Math.min(100, Math.max(0, Math.round(pct * 10) / 10));
}

export function getBudgetStatus(summary?: RunCostSummary | null): BudgetStatusInfo {
  if (!summary) {
    return {
      status: 'normal',
      message: 'Within safe budget limits',
      color: '#10b981',
      percentage: 0,
    };
  }

  const guard = summary.guardrail || {};
  const maxUSD = guard.max_budget_usd || 0;
  const maxTokens = guard.max_tokens || 0;

  const costPct = maxUSD > 0 ? (summary.total_cost_usd / maxUSD) * 100 : 0;
  const tokenPct = maxTokens > 0 ? (summary.total_tokens / maxTokens) * 100 : 0;
  const effectivePct = Math.max(costPct, tokenPct);

  if (summary.exceeded || costPct >= 100 || tokenPct >= 100) {
    return {
      status: 'exceeded',
      message: summary.exceeded_reason || 'Budget or token limit breached',
      color: '#ef4444',
      percentage: Math.min(100, Math.round(effectivePct)),
    };
  }

  const warnThreshold = (guard.warning_threshold || 0.8) * 100;
  if (summary.warning_triggered || effectivePct >= warnThreshold) {
    return {
      status: 'warning',
      message: `Approaching quota limit (${Math.round(effectivePct)}% used)`,
      color: '#f59e0b',
      percentage: Math.min(100, Math.round(effectivePct)),
    };
  }

  return {
    status: 'normal',
    message: `Nominal consumption (${Math.round(effectivePct)}% of budget)`,
    color: '#10b981',
    percentage: Math.min(100, Math.round(effectivePct)),
  };
}

export const CostBudgetModal: React.FC<CostBudgetModalProps> = ({
  isOpen: explicitIsOpen,
  onClose: explicitOnClose,
  runId,
  summary: propSummary,
  onRefresh,
}) => {
  const store = useFactoryFlowStore();
  const storeOpen = typeof window === 'undefined' ? useFactoryFlowStore.getState().showCostBudgetModal : store.showCostBudgetModal;
  const isOpen = explicitIsOpen !== undefined ? explicitIsOpen : storeOpen;
  const handleClose = () => {
    if (explicitOnClose) {
      explicitOnClose();
    }
    useFactoryFlowStore.getState().setShowCostBudgetModal(false);
  };

  const [data, setData] = useState<RunCostSummary | null>(propSummary || null);
  const [isLoading, setIsLoading] = useState<boolean>(false);
  const [error, setError] = useState<string | null>(null);

  const loadData = useCallback(async () => {
    if (!runId) return;
    setIsLoading(true);
    setError(null);
    try {
      const result = await fetchRunCost(runId);
      setData(result);
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      setError(msg);
    } finally {
      setIsLoading(false);
    }
  }, [runId]);

  useEffect(() => {
    if (propSummary) {
      setData(propSummary);
    } else if (isOpen && runId) {
      loadData();
    }
  }, [isOpen, runId, propSummary, loadData]);

  const summary = propSummary || data;
  const statusInfo = useMemo(() => getBudgetStatus(summary), [summary]);

  if (!isOpen) {
    return null;
  }

  const guardrail: BudgetGuardrail = summary?.guardrail || {
    max_budget_usd: 10.0,
    max_tokens: 500000,
  };

  const cost = summary?.total_cost_usd || 0;
  const promptTokens = summary?.prompt_tokens || 0;
  const compTokens = summary?.completion_tokens || 0;
  const totalTokens = summary?.total_tokens || 0;

  const costPercentage = calculateBudgetPercentage(cost, guardrail.max_budget_usd);
  const tokenPercentage = calculateBudgetPercentage(totalTokens, guardrail.max_tokens);

  const modelsList = summary?.models ? Object.values(summary.models) : [];

  return (
    <div
      className="cost-modal-backdrop fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/75 backdrop-blur-sm"
      role="dialog"
      aria-modal="true"
      aria-labelledby="cost-modal-title"
    >
      <div className="cost-modal-container bg-slate-900 border border-slate-700/80 rounded-xl shadow-2xl w-full max-w-2xl overflow-hidden text-slate-100 animate-in fade-in zoom-in-95 duration-150">
        {/* Header */}
        <div className="flex items-center justify-between px-6 py-4 border-b border-slate-800 bg-slate-950/40">
          <div className="flex items-center space-x-3">
            <div
              className="p-2 rounded-lg bg-cyan-950/60 border border-cyan-800/50 text-cyan-400"
              aria-hidden="true"
            >
              <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                <line x1="12" y1="1" x2="12" y2="23" />
                <path d="M17 5H9.5a3.5 3.5 0 0 0 0 7h5a3.5 3.5 0 0 1 0 7H6" />
              </svg>
            </div>
            <div>
              <h2 id="cost-modal-title" className="text-lg font-bold tracking-tight text-white">
                Cost &amp; Token Budget Guardrails
              </h2>
              <p className="text-xs text-slate-400 font-mono">
                {summary?.run_id ? `Run: ${summary.run_id}` : runId ? `Run: ${runId}` : 'Pipeline Telemetry Overview'}
              </p>
            </div>
          </div>
          <button
            type="button"
            onClick={handleClose}
            className="text-slate-400 hover:text-white p-1 rounded-lg hover:bg-slate-800 transition-colors"
            aria-label="Close modal"
          >
            <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <line x1="18" y1="6" x2="6" y2="18" />
              <line x1="6" y1="6" x2="18" y2="18" />
            </svg>
          </button>
        </div>

        {/* Content Body */}
        <div className="p-6 space-y-6 max-h-[80vh] overflow-y-auto">
          {error && (
            <div className="p-3 bg-red-950/40 border border-red-800/60 rounded-lg text-red-300 text-sm flex items-center justify-between">
              <span>{error}</span>
              <button
                type="button"
                onClick={loadData}
                className="text-xs underline hover:text-white font-mono"
              >
                Retry
              </button>
            </div>
          )}

          {/* Live Budget Bar Section */}
          <div className="p-4 bg-slate-950/60 border border-slate-800 rounded-lg space-y-3">
            <div className="flex items-center justify-between">
              <div className="flex items-center space-x-2">
                <span
                  className="w-2.5 h-2.5 rounded-full"
                  style={{ backgroundColor: statusInfo.color }}
                />
                <span className="text-sm font-semibold tracking-wide uppercase text-slate-200">
                  {statusInfo.status === 'exceeded'
                    ? 'Guardrail Breached'
                    : statusInfo.status === 'warning'
                    ? 'Warning: Quota Approaching'
                    : 'Budget Status Normal'}
                </span>
              </div>
              <span className="text-xs font-mono font-bold" style={{ color: statusInfo.color }}>
                {statusInfo.percentage}% Consumed
              </span>
            </div>

            {/* Progress Bar */}
            <div
              className="w-full bg-slate-800/80 rounded-full h-3 overflow-hidden p-0.5 border border-slate-700/50"
              role="progressbar"
              aria-valuenow={statusInfo.percentage}
              aria-valuemin={0}
              aria-valuemax={100}
            >
              <div
                className="h-full rounded-full transition-all duration-300"
                style={{
                  width: `${statusInfo.percentage}%`,
                  backgroundColor: statusInfo.color,
                }}
              />
            </div>

            {/* Warning / Exceeded Banner */}
            {statusInfo.status !== 'normal' && (
              <div
                className={`p-2.5 rounded-md text-xs font-medium border flex items-center space-x-2 ${
                  statusInfo.status === 'exceeded'
                    ? 'bg-red-950/40 border-red-800/80 text-red-200'
                    : 'bg-amber-950/40 border-amber-800/80 text-amber-200'
                }`}
              >
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className="flex-shrink-0">
                  <path d="m21.73 18-8-14a2 2 0 0 0-3.48 0l-8 14A2 2 0 0 0 4 21h16a2 2 0 0 0 1.73-3Z" />
                  <line x1="12" y1="9" x2="12" y2="13" />
                  <line x1="12" y1="17" x2="12.01" y2="17" />
                </svg>
                <span>{statusInfo.message}</span>
              </div>
            )}
          </div>

          {/* Token & Cost Metrics Cards Grid */}
          <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
            {/* Input Tokens */}
            <div className="p-3 bg-slate-950/40 border border-slate-800/80 rounded-lg">
              <span className="text-xs text-slate-400 font-medium block">Input (Prompt)</span>
              <span className="text-lg font-bold font-mono text-cyan-400 mt-1 block">
                {formatTokenCount(promptTokens)}
              </span>
              <span className="text-[11px] text-slate-500 font-mono">
                {promptTokens.toLocaleString()} tok
              </span>
            </div>

            {/* Output Tokens */}
            <div className="p-3 bg-slate-950/40 border border-slate-800/80 rounded-lg">
              <span className="text-xs text-slate-400 font-medium block">Output (Completion)</span>
              <span className="text-lg font-bold font-mono text-emerald-400 mt-1 block">
                {formatTokenCount(compTokens)}
              </span>
              <span className="text-[11px] text-slate-500 font-mono">
                {compTokens.toLocaleString()} tok
              </span>
            </div>

            {/* Total Tokens vs Quota */}
            <div className="p-3 bg-slate-950/40 border border-slate-800/80 rounded-lg">
              <span className="text-xs text-slate-400 font-medium block">Total Tokens</span>
              <span className="text-lg font-bold font-mono text-white mt-1 block">
                {formatTokenCount(totalTokens)}
              </span>
              <span className="text-[11px] text-slate-500 font-mono">
                Limit: {guardrail.max_tokens ? formatTokenCount(guardrail.max_tokens) : '∞'} ({tokenPercentage}%)
              </span>
            </div>

            {/* Estimated Cost vs Budget */}
            <div className="p-3 bg-slate-950/40 border border-slate-800/80 rounded-lg">
              <span className="text-xs text-slate-400 font-medium block">Estimated Cost</span>
              <span className="text-lg font-bold font-mono text-white mt-1 block">
                {formatUSD(cost)}
              </span>
              <span className="text-[11px] text-slate-500 font-mono">
                Max: {guardrail.max_budget_usd ? formatUSD(guardrail.max_budget_usd) : '∞'} ({costPercentage}%)
              </span>
            </div>
          </div>

          {/* Model Breakdown Table */}
          <div className="space-y-2">
            <h3 className="text-xs font-semibold uppercase tracking-wider text-slate-400">
              Consumption by Model
            </h3>
            {modelsList.length === 0 ? (
              <div className="p-4 text-center text-xs text-slate-500 bg-slate-950/30 border border-dashed border-slate-800 rounded-lg">
                No model telemetry recorded yet for this pipeline run.
              </div>
            ) : (
              <div className="border border-slate-800 rounded-lg overflow-hidden bg-slate-950/40">
                <table className="w-full text-xs text-left">
                  <thead className="bg-slate-900/80 text-slate-400 border-b border-slate-800 font-mono">
                    <tr>
                      <th className="py-2 px-3">Model</th>
                      <th className="py-2 px-3 text-right">Input</th>
                      <th className="py-2 px-3 text-right">Output</th>
                      <th className="py-2 px-3 text-right">Total</th>
                      <th className="py-2 px-3 text-right">Est. Cost</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-slate-800/60 font-mono">
                    {modelsList.map((m) => (
                      <tr key={m.model} className="hover:bg-slate-800/30 transition-colors">
                        <td className="py-2 px-3 font-sans font-medium text-slate-200">
                          {m.display_name || m.model}
                        </td>
                        <td className="py-2 px-3 text-right text-slate-400">
                          {formatTokenCount(m.prompt_tokens)}
                        </td>
                        <td className="py-2 px-3 text-right text-slate-400">
                          {formatTokenCount(m.completion_tokens)}
                        </td>
                        <td className="py-2 px-3 text-right text-slate-300 font-semibold">
                          {formatTokenCount(m.total_tokens)}
                        </td>
                        <td className="py-2 px-3 text-right text-emerald-400 font-bold">
                          {formatUSD(m.cost_usd)}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>
        </div>

        {/* Footer Actions */}
        <div className="px-6 py-3.5 border-t border-slate-800 bg-slate-950/60 flex items-center justify-between">
          <div className="text-[11px] text-slate-500 font-mono">
            {summary?.updated_at && `Updated: ${new Date(summary.updated_at).toLocaleTimeString()}`}
          </div>
          <div className="flex items-center space-x-2">
            {(runId || onRefresh) && (
              <button
                type="button"
                onClick={() => {
                  if (onRefresh) onRefresh();
                  if (runId) loadData();
                }}
                disabled={isLoading}
                className="px-3 py-1.5 rounded-lg border border-slate-700 bg-slate-800 hover:bg-slate-700 text-xs font-medium text-slate-200 transition-colors disabled:opacity-50"
              >
                {isLoading ? 'Refreshing...' : 'Refresh'}
              </button>
            )}
            <button
              type="button"
              onClick={handleClose}
              className="px-4 py-1.5 rounded-lg bg-cyan-600 hover:bg-cyan-500 text-xs font-semibold text-white transition-colors"
            >
              Done
            </button>
          </div>
        </div>
      </div>
    </div>
  );
};

export const CostBudgetButton: React.FC<{
  summary?: RunCostSummary | null;
  className?: string;
}> = ({ summary, className = '' }) => {
  const store = useFactoryFlowStore();
  const show = typeof window === 'undefined' ? useFactoryFlowStore.getState().showCostBudgetModal : store.showCostBudgetModal;

  return (
    <CostBadge
      summary={summary}
      onClick={() => useFactoryFlowStore.getState().setShowCostBudgetModal(!show)}
      className={className}
    />
  );
};
