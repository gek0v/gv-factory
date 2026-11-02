// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import React from 'react';
import type { RunCostSummary } from '../../types/telemetry';

export interface CostBadgeProps {
  summary?: RunCostSummary | null;
  totalCostUSD?: number;
  totalTokens?: number;
  exceeded?: boolean;
  warning?: boolean;
  size?: 'sm' | 'md' | 'lg';
  showTokens?: boolean;
  onClick?: () => void;
  className?: string;
}

export function formatUSD(amount: number): string {
  if (isNaN(amount) || amount < 0) return '$0.00';
  if (amount === 0) return '$0.00';
  if (amount < 0.01) {
    return `$${amount.toFixed(4)}`;
  }
  return `$${amount.toFixed(2)}`;
}

export function formatTokenCount(tokens: number): string {
  if (isNaN(tokens) || tokens <= 0) return '0';
  if (tokens >= 1_000_000) {
    return `${(tokens / 1_000_000).toFixed(1)}M`;
  }
  if (tokens >= 1_000) {
    return `${(tokens / 1_000).toFixed(1)}k`;
  }
  return tokens.toLocaleString();
}

export const CostBadge: React.FC<CostBadgeProps> = ({
  summary,
  totalCostUSD,
  totalTokens,
  exceeded,
  warning,
  size = 'md',
  showTokens = true,
  onClick,
  className = '',
}) => {
  const cost = summary ? summary.total_cost_usd : (totalCostUSD ?? 0);
  const tokens = summary ? summary.total_tokens : (totalTokens ?? 0);
  const isExceeded = summary ? summary.exceeded : Boolean(exceeded);
  const isWarning = summary ? summary.warning_triggered : Boolean(warning);

  let variantClass = 'cost-badge-normal';
  let statusText = 'Normal';
  let badgeColor = '#10b981';

  if (isExceeded) {
    variantClass = 'cost-badge-exceeded';
    statusText = 'Budget Exceeded';
    badgeColor = '#ef4444';
  } else if (isWarning) {
    variantClass = 'cost-badge-warning';
    statusText = 'Quota Warning';
    badgeColor = '#f59e0b';
  }

  const sizeClasses = {
    sm: 'text-xs py-0.5 px-2 gap-1',
    md: 'text-sm py-1 px-2.5 gap-1.5',
    lg: 'text-base py-1.5 px-3.5 gap-2',
  }[size];

  return (
    <button
      type="button"
      onClick={onClick}
      className={`cost-badge inline-flex items-center font-mono font-medium rounded-full transition-all duration-150 border ${variantClass} ${sizeClasses} ${className}`}
      title={`Token usage: ${tokens.toLocaleString()} tokens | Est. Cost: ${formatUSD(cost)} (${statusText})`}
      aria-label={`Cost and budget summary: ${formatUSD(cost)}, ${tokens} tokens (${statusText})`}
    >
      <span
        className="w-2 h-2 rounded-full inline-block flex-shrink-0 animate-pulse"
        style={{ backgroundColor: badgeColor }}
      />
      <span className="cost-badge-value font-semibold">{formatUSD(cost)}</span>
      {showTokens && (
        <span className="cost-badge-tokens opacity-75 font-normal">
          ({formatTokenCount(tokens)} tok)
        </span>
      )}
      {isExceeded && (
        <span className="cost-badge-alert font-bold text-xs uppercase tracking-wider ml-0.5">
          LIMIT
        </span>
      )}
    </button>
  );
};
